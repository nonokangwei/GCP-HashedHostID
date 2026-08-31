package syncer

import (
	"context"
	"fmt"
	"testing"
	"time"

	"hosthashedid/internal/gce"
	"hosthashedid/internal/k8s"
	"hosthashedid/internal/pubsub"
	"hosthashedid/internal/store"
)

// mockGCE implements GCEInterface
type mockGCE struct {
	instances map[string]*gce.Instance
}

func (m *mockGCE) GetInstance(ctx context.Context, project, zone, instanceName string) (*gce.Instance, error) {
	key := zone + "/" + instanceName
	if inst, ok := m.instances[key]; ok {
		return inst, nil
	}
	if inst, ok := m.instances[instanceName]; ok {
		return inst, nil
	}
	return nil, fmt.Errorf("instance not found")
}

func (m *mockGCE) ListInstances(ctx context.Context, project, zone string) ([]*gce.Instance, error) {
	var res []*gce.Instance
	for _, inst := range m.instances {
		if inst.Zone == zone || cleanZone(inst.Zone) == cleanZone(zone) {
			res = append(res, inst)
		}
	}
	return res, nil
}

func (m *mockGCE) AggregatedListInstances(ctx context.Context, project string) ([]*gce.Instance, error) {
	var res []*gce.Instance
	for _, inst := range m.instances {
		res = append(res, inst)
	}
	return res, nil
}

// mockLabeler implements k8s.NodeLabeler
type mockLabeler struct {
	labeledNodes map[string]string
}

func (m *mockLabeler) IsGKENode(name string) bool {
	return k8s.IsGKENode(name)
}

func (m *mockLabeler) LabelNode(ctx context.Context, nodeName string, hashedHostID string) error {
	m.labeledNodes[nodeName] = hashedHostID
	return nil
}

func TestSyncer_FullScan(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemoryStore()
	labeler := &mockLabeler{labeledNodes: make(map[string]string)}

	mockInstances := map[string]*gce.Instance{
		"gke-node-1": {
			ID:   "101",
			Name: "gke-node-1",
			Zone: "us-east1-b",
			ResourceStatus: &gce.ResourceStatus{
				PhysicalHostTopology: &gce.PhysicalHostTopology{Host: "host-hash-1"},
			},
		},
		"vm-standard-1": {
			ID:   "102",
			Name: "vm-standard-1",
			Zone: "us-east1-b",
			ResourceStatus: &gce.ResourceStatus{
				PhysicalHostTopology: &gce.PhysicalHostTopology{Host: "host-hash-2"},
			},
		},
		"vm-disabled-1": {
			ID:   "103",
			Name: "vm-disabled-1",
			Zone: "us-east1-b",
			// No PhysicalHostTopology
		},
	}

	gceMock := &mockGCE{instances: mockInstances}
	syncService := NewSyncer(gceMock, st, labeler, "test-proj", []string{"us-east1-b"})

	res, err := syncService.FullScan(ctx)
	if err != nil {
		t.Fatalf("FullScan failed: %v", err)
	}

	if res.ScannedInstances != 3 {
		t.Errorf("expected 3 scanned, got %d", res.ScannedInstances)
	}
	if res.MatchedInstances != 2 {
		t.Errorf("expected 2 matched, got %d", res.MatchedInstances)
	}
	if res.GKESynced != 1 {
		t.Errorf("expected 1 GKE synced, got %d", res.GKESynced)
	}

	// Verify store
	rec1, err := st.Get(ctx, "101")
	if err != nil || rec1.HashedHostID != "host-hash-1" {
		t.Errorf("expected 101 in store with host-hash-1, got %v (%v)", rec1, err)
	}

	rec2, err := st.Get(ctx, "102")
	if err != nil || rec2.HashedHostID != "host-hash-2" {
		t.Errorf("expected 102 in store with host-hash-2, got %v (%v)", rec2, err)
	}

	_, err = st.Get(ctx, "103")
	if err != store.ErrNotFound {
		t.Errorf("expected 103 to not be in store")
	}

	// Verify K8s labeling
	if labeler.labeledNodes["gke-node-1"] != "host-hash-1" {
		t.Errorf("expected gke-node-1 labeled with host-hash-1, got %s", labeler.labeledNodes["gke-node-1"])
	}
	if _, ok := labeler.labeledNodes["vm-standard-1"]; ok {
		t.Errorf("expected vm-standard-1 not to be labeled in k8s")
	}
}

func TestSyncer_HandleHostEvent(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemoryStore()
	labeler := &mockLabeler{labeledNodes: make(map[string]string)}

	mockInstances := map[string]*gce.Instance{
		"us-east1-b/gke-node-migrated": {
			ID:   "201",
			Name: "gke-node-migrated",
			Zone: "us-east1-b",
			ResourceStatus: &gce.ResourceStatus{
				PhysicalHostTopology: &gce.PhysicalHostTopology{Host: "new-migrated-host-hash"},
			},
		},
	}

	gceMock := &mockGCE{instances: mockInstances}
	syncService := NewSyncer(gceMock, st, labeler, "test-proj", nil)

	event := &pubsub.HostEvent{
		ProjectID:    "test-proj",
		Zone:         "us-east1-b",
		InstanceName: "gke-node-migrated",
		MethodName:   pubsub.MethodMigrateOnHostMaintenance,
		Timestamp:    time.Now().UTC(),
	}

	err := syncService.HandleHostEvent(ctx, event)
	if err != nil {
		t.Fatalf("HandleHostEvent failed: %v", err)
	}

	rec, err := st.Get(ctx, "201")
	if err != nil || rec.HashedHostID != "new-migrated-host-hash" {
		t.Errorf("expected store updated with new-migrated-host-hash, got %v (%v)", rec, err)
	}

	if labeler.labeledNodes["gke-node-migrated"] != "new-migrated-host-hash" {
		t.Errorf("expected k8s label updated to new-migrated-host-hash, got %s", labeler.labeledNodes["gke-node-migrated"])
	}
}
