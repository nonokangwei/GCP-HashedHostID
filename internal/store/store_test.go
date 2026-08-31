package store

import (
	"context"
	"testing"
	"time"
)

func TestMemoryStore_CRUD(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	record := InstanceRecord{
		InstanceID:   "2449360396712485978",
		InstanceName: "gke-node-1",
		ProjectID:    "test-project",
		Zone:         "us-east1-b",
		HashedHostID: "71397112c5ff11f47eaad4988c5c2e99",
		IsGKE:        true,
		UpdatedAt:    time.Now().UTC(),
	}

	// 1. Get non-existent
	_, err := s.Get(ctx, record.InstanceID)
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	// 2. Save
	if err := s.Save(ctx, record); err != nil {
		t.Fatalf("failed to save: %v", err)
	}

	// 3. Get existing
	got, err := s.Get(ctx, record.InstanceID)
	if err != nil {
		t.Fatalf("failed to get: %v", err)
	}
	if got.HashedHostID != record.HashedHostID {
		t.Errorf("expected HashedHostID %s, got %s", record.HashedHostID, got.HashedHostID)
	}
	if got.InstanceName != record.InstanceName {
		t.Errorf("expected InstanceName %s, got %s", record.InstanceName, got.InstanceName)
	}

	// 4. List
	list, err := s.List(ctx)
	if err != nil {
		t.Fatalf("failed to list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 item, got %d", len(list))
	}

	// 5. Update
	record.HashedHostID = "updated-hash-id-9999"
	if err := s.Save(ctx, record); err != nil {
		t.Fatalf("failed to update: %v", err)
	}
	gotUpdated, _ := s.Get(ctx, record.InstanceID)
	if gotUpdated.HashedHostID != "updated-hash-id-9999" {
		t.Errorf("expected updated HashedHostID 'updated-hash-id-9999', got %s", gotUpdated.HashedHostID)
	}

	// 6. Delete
	if err := s.Delete(ctx, record.InstanceID); err != nil {
		t.Fatalf("failed to delete: %v", err)
	}
	_, err = s.Get(ctx, record.InstanceID)
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}

	listAfterDelete, _ := s.List(ctx)
	if len(listAfterDelete) != 0 {
		t.Errorf("expected 0 items after delete, got %d", len(listAfterDelete))
	}
}
