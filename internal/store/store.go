package store

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrNotFound is returned when an instance record is not found in the store.
	ErrNotFound = errors.New("instance record not found")
)

// InstanceRecord represents the persisted information about a GCE instance and its HashedHostID.
type InstanceRecord struct {
	InstanceID   string    `json:"instance_id"`
	InstanceName string    `json:"instance_name"`
	ProjectID    string    `json:"project_id"`
	Zone         string    `json:"zone"`
	HashedHostID string    `json:"hashed_host_id"`
	IsGKE        bool      `json:"is_gke"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Store is the storage interface for saving and retrieving GCE instance HashedHostID mappings.
type Store interface {
	// Save creates or updates an instance record.
	Save(ctx context.Context, record InstanceRecord) error

	// Get retrieves a record by instance ID.
	Get(ctx context.Context, instanceID string) (*InstanceRecord, error)

	// List retrieves all stored instance records.
	List(ctx context.Context) ([]InstanceRecord, error)

	// Delete removes an instance record by ID.
	Delete(ctx context.Context, instanceID string) error

	// Close closes any open connections (e.g., Redis).
	Close() error
}
