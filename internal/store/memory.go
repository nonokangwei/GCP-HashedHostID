package store

import (
	"context"
	"sync"
)

// MemoryStore is an in-memory, concurrency-safe implementation of Store.
type MemoryStore struct {
	mu      sync.RWMutex
	records map[string]InstanceRecord
}

// NewMemoryStore creates a new in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		records: make(map[string]InstanceRecord),
	}
}

// Save stores or updates an instance record in memory.
func (m *MemoryStore) Save(ctx context.Context, record InstanceRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records[record.InstanceID] = record
	return nil
}

// Get retrieves an instance record by ID.
func (m *MemoryStore) Get(ctx context.Context, instanceID string) (*InstanceRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rec, ok := m.records[instanceID]
	if !ok {
		return nil, ErrNotFound
	}
	return &rec, nil
}

// List returns a slice of all stored records.
func (m *MemoryStore) List(ctx context.Context) ([]InstanceRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list := make([]InstanceRecord, 0, len(m.records))
	for _, rec := range m.records {
		list = append(list, rec)
	}
	return list, nil
}

// Delete removes an instance record by ID.
func (m *MemoryStore) Delete(ctx context.Context, instanceID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.records[instanceID]; !ok {
		return ErrNotFound
	}
	delete(m.records, instanceID)
	return nil
}

// Close is a no-op for MemoryStore.
func (m *MemoryStore) Close() error {
	return nil
}
