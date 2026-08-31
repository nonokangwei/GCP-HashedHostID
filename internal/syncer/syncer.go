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

	mu                   sync.RWMutex
	lastScanTime         time.Time
	totalScanned         int64
	totalWithHostID      int64
	totalGKESynced       int64
	totalEventsProcessed int64
}

// SyncerOption allows customizing Syncer.
type SyncerOption func(*Syncer)

// NewSyncer creates a new Syncer instance.
func NewSyncer(gceClient GCEInterface, st store.Store, labeler k8s.NodeLabeler, projectID string, zones []string) *Syncer {
	return &Syncer{
		gceClient: gceClient,
		store:     st,
		labeler:   labeler,
		projectID: projectID,
		zones:     zones,
	}
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

	record, err := s.RefreshInstance(ctx, project, event.Zone, event.InstanceName)
	if err != nil {
		return fmt.Errorf("failed to refresh instance on event: %w", err)
	}

	s.mu.Lock()
	s.totalEventsProcessed++
	s.mu.Unlock()

	if record != nil {
		log.Printf("[Syncer] Successfully updated HashedHostID for instance %s to %s", record.InstanceName, record.HashedHostID)
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
