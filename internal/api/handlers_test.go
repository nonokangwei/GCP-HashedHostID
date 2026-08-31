package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"hosthashedid/internal/store"
	"hosthashedid/internal/syncer"
)

type mockSyncerService struct {
	fullScanResult *syncer.ScanResult
	refreshRecord  *store.InstanceRecord
	stats          syncer.Stats
}

func (m *mockSyncerService) FullScan(ctx context.Context, customZones ...string) (*syncer.ScanResult, error) {
	return m.fullScanResult, nil
}

func (m *mockSyncerService) RefreshInstance(ctx context.Context, project, zone, instanceName string) (*store.InstanceRecord, error) {
	return m.refreshRecord, nil
}

func (m *mockSyncerService) GetStats() syncer.Stats {
	return m.stats
}

func setupTestServer() (*http.ServeMux, store.Store, *mockSyncerService) {
	st := store.NewMemoryStore()
	mockSync := &mockSyncerService{
		fullScanResult: &syncer.ScanResult{
			ScannedInstances: 5,
			MatchedInstances: 2,
			GKESynced:        1,
			Duration:         "10ms",
		},
		refreshRecord: &store.InstanceRecord{
			InstanceID:   "inst-999",
			InstanceName: "gke-node-999",
			ProjectID:    "test-proj",
			Zone:         "us-east1-d",
			HashedHostID: "hash-999",
			IsGKE:        true,
			UpdatedAt:    time.Now().UTC(),
		},
	}

	mux := http.NewServeMux()
	handler := NewAPIHandler(mockSync, st)
	handler.RegisterRoutes(mux)

	return mux, st, mockSync
}

func TestAPI_HealthCheck(t *testing.T) {
	mux, _, _ := setupTestServer()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var resp map[string]string
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["status"] != "healthy" {
		t.Errorf("expected healthy, got %s", resp["status"])
	}
}

func TestAPI_Sync_FullScan(t *testing.T) {
	mux, _, _ := setupTestServer()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sync", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["success"] != true {
		t.Errorf("expected success true")
	}
}

func TestAPI_Sync_SingleInstance(t *testing.T) {
	mux, _, _ := setupTestServer()

	payload := SyncRequest{
		Project:  "test-proj",
		Zone:     "us-east1-d",
		Instance: "gke-node-999",
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sync", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["success"] != true {
		t.Errorf("expected success true")
	}
}

func TestAPI_Instances_ListAndGet(t *testing.T) {
	ctx := context.Background()
	mux, st, _ := setupTestServer()

	record := store.InstanceRecord{
		InstanceID:   "inst-1",
		InstanceName: "gke-node-1",
		ProjectID:    "test-proj",
		Zone:         "us-east1-b",
		HashedHostID: "hash-1",
		IsGKE:        true,
		UpdatedAt:    time.Now().UTC(),
	}
	st.Save(ctx, record)

	// List
	reqList := httptest.NewRequest(http.MethodGet, "/api/v1/instances", nil)
	recList := httptest.NewRecorder()
	mux.ServeHTTP(recList, reqList)

	if recList.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recList.Code)
	}

	// Get existing
	reqGet := httptest.NewRequest(http.MethodGet, "/api/v1/instances/inst-1", nil)
	recGet := httptest.NewRecorder()
	mux.ServeHTTP(recGet, reqGet)

	if recGet.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recGet.Code)
	}
	var gotRec store.InstanceRecord
	json.NewDecoder(recGet.Body).Decode(&gotRec)
	if gotRec.HashedHostID != "hash-1" {
		t.Errorf("expected HashedHostID 'hash-1', got %s", gotRec.HashedHostID)
	}

	// Get non-existing
	reqGet404 := httptest.NewRequest(http.MethodGet, "/api/v1/instances/not-found", nil)
	recGet404 := httptest.NewRecorder()
	mux.ServeHTTP(recGet404, reqGet404)

	if recGet404.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", recGet404.Code)
	}
}
