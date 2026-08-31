package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"hosthashedid/internal/store"
	"hosthashedid/internal/syncer"
)

// SyncerService defines the sync operations required by the API.
type SyncerService interface {
	FullScan(ctx context.Context, customZones ...string) (*syncer.ScanResult, error)
	RefreshInstance(ctx context.Context, project, zone, instanceName string) (*store.InstanceRecord, error)
	GetStats() syncer.Stats
}

// APIHandler handles HTTP API requests.
type APIHandler struct {
	syncer SyncerService
	store  store.Store
}

// NewAPIHandler creates a new APIHandler.
func NewAPIHandler(syncer SyncerService, st store.Store) *APIHandler {
	return &APIHandler{
		syncer: syncer,
		store:  st,
	}
}

// SyncRequest represents the payload for POST /api/v1/sync
type SyncRequest struct {
	Project  string `json:"project,omitempty"`
	Zone     string `json:"zone,omitempty"`
	Instance string `json:"instance,omitempty"`
}

// RegisterRoutes registers all API routes onto the given ServeMux.
func (h *APIHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", h.handleHealthCheck)
	mux.HandleFunc("/livez", h.handleHealthCheck)
	mux.HandleFunc("/api/v1/status", h.handleStatus)
	mux.HandleFunc("/api/v1/sync", h.handleSync)
	mux.HandleFunc("/api/v1/instances/load", h.handleSync) // alias as described in design.md
	mux.HandleFunc("/api/v1/instances", h.handleInstances)
	mux.HandleFunc("/api/v1/instances/", h.handleInstanceByID)
}

func (h *APIHandler) handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "healthy",
	})
}

func (h *APIHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stats := h.syncer.GetStats()
	allRecords, err := h.store.List(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":          "running",
		"total_in_store":  len(allRecords),
		"syncer_stats":    stats,
	})
}

func (h *APIHandler) handleSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req SyncRequest
	if r.Body != nil && r.ContentLength > 0 {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	// Check query params if not set in body
	if req.Zone == "" {
		req.Zone = r.URL.Query().Get("zone")
	}
	if req.Instance == "" {
		req.Instance = r.URL.Query().Get("instance")
	}
	if req.Project == "" {
		req.Project = r.URL.Query().Get("project")
	}

	// Single instance sync
	if req.Instance != "" && req.Zone != "" {
		rec, err := h.syncer.RefreshInstance(r.Context(), req.Project, req.Zone, req.Instance)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
				"success": false,
				"error":   err.Error(),
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"message": fmt.Sprintf("Successfully refreshed instance %s", req.Instance),
			"record":  rec,
		})
		return
	}

	// Full scan or zone scan
	var customZones []string
	if req.Zone != "" {
		customZones = strings.Split(req.Zone, ",")
	}

	result, err := h.syncer.FullScan(r.Context(), customZones...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"result":  result,
	})
}

func (h *APIHandler) handleInstances(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	records, err := h.store.List(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"total": len(records),
		"items": records,
	})
}

func (h *APIHandler) handleInstanceByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Path is /api/v1/instances/{id}
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/instances/")
	if id == "" {
		http.Error(w, "Instance ID required", http.StatusBadRequest)
		return
	}

	record, err := h.store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "Instance not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, record)
}

func writeJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(data)
}
