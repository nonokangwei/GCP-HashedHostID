package syncer

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"hosthashedid/internal/gce"
	"hosthashedid/internal/k8s"
	"hosthashedid/internal/pubsub"
	"hosthashedid/internal/store"
)

// GCEInterface defines the subset of GCE client methods used by Syncer.
type GCEInterface interface {
	GetInstance(ctx context.Context, project, zone, instanceName string) (*gce.Instance, error)
	ListInstances(ctx context.Context, project, zone string) ([]*gce.Instance, error)
	AggregatedListInstances(ctx context.Context, project string) ([]*gce.Instance, error)
}

// Syncer coordinates GCE inspection, storage persistence, and Kubernetes node labeling.
type Syncer struct {
	gceClient GCEInterface
	store     store.Store
	labeler   k8s.NodeLabeler
	projectID string
	zones     []string

	eventPollTimeout  time.Duration
	eventPollInterval time.Duration

	mu                   sync.RWMutex
	lastScanTime         time.Time
	totalScanned         int64
	totalWithHostID      int64
	totalGKESynced       int64
	totalEventsProcessed int64
}

// SyncerOption defines a functional option for Syncer.
type SyncerOption func(*Syncer)

// WithEventPollConfig sets custom timeout and interval for polling host migration events.
func WithEventPollConfig(timeout, interval time.Duration) SyncerOption {
	return func(s *Syncer) {
		s.eventPollTimeout = timeout
		s.eventPollInterval = interval
	}
}

// NewSyncer creates a new Syncer instance.
func NewSyncer(gceClient GCEInterface, st store.Store, labeler k8s.NodeLabeler, projectID string, zones []string, opts ...SyncerOption) *Syncer {
	s := &Syncer{
		gceClient:         gceClient,
		store:             st,
		labeler:           labeler,
		projectID:         projectID,
		zones:             zones,
		eventPollTimeout:  3 * time.Minute,
		eventPollInterval: 5 * time.Second,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// ScanResult contains details about a scan execution.
type ScanResult struct {
	ScannedInstances int                   `json:"scanned_instances"`
	MatchedInstances int                   `json:"matched_instances"`
	GKESynced        int                   `json:"gke_synced"`
	Duration         string                `json:"duration"`
	Records          []store.InstanceRecord `json:"records"`
}

// FullScan scans all configured zones (or entire project if zones is empty) and persists HashedHostID.
func (s *Syncer) FullScan(ctx context.Context, customZones ...string) (*ScanResult, error) {
	startTime := time.Now()
	targetZones := s.zones
	if len(customZones) > 0 {
		targetZones = customZones
	}

	var instances []*gce.Instance
	var err error

	if len(targetZones) > 0 {
		for _, zone := range targetZones {
			zoneInstances, err := s.gceClient.ListInstances(ctx, s.projectID, zone)
			if err != nil {
				return nil, fmt.Errorf("failed to list instances in zone %s: %w", zone, err)
			}
			instances = append(instances, zoneInstances...)
		}
	} else {
		instances, err = s.gceClient.AggregatedListInstances(ctx, s.projectID)
		if err != nil {
			return nil, fmt.Errorf("failed to aggregated list instances for project %s: %w", s.projectID, err)
		}
	}

	result := &ScanResult{
		ScannedInstances: len(instances),
	}

	for _, inst := range instances {
		record, synced, saved, err := s.processInstance(ctx, inst)
		if err != nil {
			log.Printf("[Syncer WARN] Failed to process instance %s: %v", inst.Name, err)
			continue
		}
		if saved {
			result.MatchedInstances++
			result.Records = append(result.Records, *record)
		}
		if synced {
			result.GKESynced++
		}
	}

	result.Duration = time.Since(startTime).String()

	s.mu.Lock()
	s.lastScanTime = time.Now().UTC()
	s.totalScanned += int64(result.ScannedInstances)
	s.totalWithHostID += int64(result.MatchedInstances)
	s.totalGKESynced += int64(result.GKESynced)
	s.mu.Unlock()

	log.Printf("[Syncer] Full scan completed: %d scanned, %d matched HashedHostID, %d GKE nodes labeled (duration: %s)",
		result.ScannedInstances, result.MatchedInstances, result.GKESynced, result.Duration)

	return result, nil
}

// RefreshInstance fetches the latest state of a single instance and updates store and K8s label.
func (s *Syncer) RefreshInstance(ctx context.Context, project, zone, instanceName string) (*store.InstanceRecord, error) {
	if project == "" {
		project = s.projectID
	}

	inst, err := s.gceClient.GetInstance(ctx, project, zone, instanceName)
	if err != nil {
		return nil, fmt.Errorf("failed to get instance %s/%s/%s: %w", project, zone, instanceName, err)
	}

	record, _, _, err := s.processInstance(ctx, inst)
	if err != nil {
		return nil, err
	}

	return record, nil
}

// HandleHostEvent implements pubsub.EventHandler.
func (s *Syncer) HandleHostEvent(ctx context.Context, event *pubsub.HostEvent) error {
	project := event.ProjectID
	if project == "" {
		project = s.projectID
	}

	log.Printf("[Syncer] Handling host event %s for instance=%s (zone=%s, project=%s)",
		event.MethodName, event.InstanceName, event.Zone, project)

	// Fetch existing record if any to detect host ID change
	var oldHostID string
	if event.InstanceID != "" {
		if existing, err := s.store.Get(ctx, event.InstanceID); err == nil && existing != nil {
			oldHostID = existing.HashedHostID
		}
	}
	if oldHostID == "" && event.InstanceName != "" {
		if all, err := s.store.List(ctx); err == nil {
			for _, rec := range all {
				if rec.InstanceName == event.InstanceName {
					oldHostID = rec.HashedHostID
					break
				}
			}
		}
	}

	pollTimeout := s.eventPollTimeout
	if pollTimeout <= 0 {
		pollTimeout = 3 * time.Minute
	}
	pollInterval := s.eventPollInterval
	if pollInterval <= 0 {
		pollInterval = 5 * time.Second
	}

	startTime := time.Now()
	var lastRecord *store.InstanceRecord
	var lastErr error

	for {
		inst, err := s.gceClient.GetInstance(ctx, project, event.Zone, event.InstanceName)
		if err != nil {
			lastErr = err
			log.Printf("[Syncer WARN] Failed to get instance %s on event: %v", event.InstanceName, err)
		} else if inst != nil {
			hostID, enabled := gce.ExtractHashedHostID(inst)
			if !enabled {
				log.Printf("[Syncer] Instance %s does not have exposeHostTopology enabled, skipping event", event.InstanceName)
				break
			}

			if hostID == "" {
				log.Printf("[Syncer] Instance %s host ID is not yet available (topology pending), will re-check in %v...",
					event.InstanceName, pollInterval)
			} else if oldHostID != "" && hostID == oldHostID {
				log.Printf("[Syncer] Instance %s host ID is still '%s' (migration cutover in progress), will re-check in %v...",
					event.InstanceName, oldHostID, pollInterval)
			} else {
				// Host ID is non-empty and has changed (or new instance), proceed to process, save to store and label node
				record, _, _, err := s.processInstance(ctx, inst)
				if err != nil {
					lastErr = err
				} else if record != nil {
					lastRecord = record
					if oldHostID != "" {
						log.Printf("[Syncer] Instance %s host ID changed from '%s' to '%s' (elapsed: %v)",
							record.InstanceName, oldHostID, record.HashedHostID, time.Since(startTime).Round(time.Millisecond))
					}
				}
				break
			}
		}

		if time.Since(startTime) >= pollTimeout {
			log.Printf("[Syncer WARN] Timed out waiting for host ID change for instance %s after %v", event.InstanceName, pollTimeout)
			// On timeout, still do a final process to make sure latest state is reflected
			if inst, err := s.gceClient.GetInstance(ctx, project, event.Zone, event.InstanceName); err == nil && inst != nil {
				lastRecord, _, _, _ = s.processInstance(ctx, inst)
			}
			break
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}

	if lastRecord == nil && lastErr != nil {
		return fmt.Errorf("failed to refresh instance on event: %w", lastErr)
	}

	s.mu.Lock()
	s.totalEventsProcessed++
	s.mu.Unlock()

	if lastRecord != nil {
		log.Printf("[Syncer] Successfully updated HashedHostID for instance %s to %s", lastRecord.InstanceName, lastRecord.HashedHostID)
	}
	return nil
}

// processInstance evaluates an instance, extracts HashedHostID, saves to store, and syncs K8s label.
func (s *Syncer) processInstance(ctx context.Context, inst *gce.Instance) (*store.InstanceRecord, bool, bool, error) {
	hostID, enabled := gce.ExtractHashedHostID(inst)
	if !enabled || hostID == "" {
		return nil, false, false, nil
	}

	isGKE := s.labeler.IsGKENode(inst.Name)
	record := store.InstanceRecord{
		InstanceID:   inst.ID,
		InstanceName: inst.Name,
		ProjectID:    s.projectID,
		Zone:         cleanZone(inst.Zone),
		HashedHostID: hostID,
		IsGKE:        isGKE,
		UpdatedAt:    time.Now().UTC(),
	}

	if err := s.store.Save(ctx, record); err != nil {
		return nil, false, false, fmt.Errorf("failed to save instance to store: %w", err)
	}

	gkeSynced := false
	if isGKE {
		if err := s.labeler.LabelNode(ctx, inst.Name, hostID); err != nil {
			log.Printf("[Syncer WARN] Failed to label GKE node %s: %v", inst.Name, err)
		} else {
			gkeSynced = true
		}
	}

	return &record, gkeSynced, true, nil
}

// Stats represents runtime telemetry for Syncer.
type Stats struct {
	ProjectID            string    `json:"project_id"`
	ConfiguredZones      []string  `json:"configured_zones"`
	LastScanTime         time.Time `json:"last_scan_time"`
	TotalScanned         int64     `json:"total_scanned"`
	TotalWithHostID      int64     `json:"total_with_host_id"`
	TotalGKESynced       int64     `json:"total_gke_synced"`
	TotalEventsProcessed int64     `json:"total_events_processed"`
}

// GetStats returns current statistics.
func (s *Syncer) GetStats() Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return Stats{
		ProjectID:            s.projectID,
		ConfiguredZones:      s.zones,
		LastScanTime:         s.lastScanTime,
		TotalScanned:         s.totalScanned,
		TotalWithHostID:      s.totalWithHostID,
		TotalGKESynced:       s.totalGKESynced,
		TotalEventsProcessed: s.totalEventsProcessed,
	}
}

func cleanZone(z string) string {
	parts := splitSlashes(z)
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return z
}

func splitSlashes(s string) []string {
	var res []string
	cur := ""
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			if cur != "" {
				res = append(res, cur)
				cur = ""
			}
		} else {
			cur += string(s[i])
		}
	}
	if cur != "" {
		res = append(res, cur)
	}
	return res
}
