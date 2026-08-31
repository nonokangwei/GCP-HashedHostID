package gce

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGCEClient_GetInstance(t *testing.T) {
	mockInstance := Instance{
		ID:                 "2449360396712485978",
		Name:               "gke-z3demo-z3-22-hdb-a1bfdebb-7qdh",
		Zone:               "https://www.googleapis.com/compute/alpha/projects/test-project/zones/us-east1-d",
		Status:             "RUNNING",
		ExposeHostTopology: true,
		ResourceStatus: &ResourceStatus{
			PhysicalHostTopology: &PhysicalHostTopology{
				Host: "71397112c5ff11f47eaad4988c5c2e99",
			},
		},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/compute/alpha/projects/test-project/zones/us-east1-d/instances/gke-z3demo-z3-22-hdb-a1bfdebb-7qdh" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockInstance)
	}))
	defer ts.Close()

	ctx := context.Background()
	client, err := NewClient(ctx, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	inst, err := client.GetInstance(ctx, "test-project", "us-east1-d", "gke-z3demo-z3-22-hdb-a1bfdebb-7qdh")
	if err != nil {
		t.Fatalf("GetInstance failed: %v", err)
	}

	if inst.ID != mockInstance.ID {
		t.Errorf("expected ID %s, got %s", mockInstance.ID, inst.ID)
	}

	hostID, enabled := ExtractHashedHostID(inst)
	if !enabled {
		t.Errorf("expected HashedHostID to be enabled")
	}
	if hostID != "71397112c5ff11f47eaad4988c5c2e99" {
		t.Errorf("expected hostID '71397112c5ff11f47eaad4988c5c2e99', got %s", hostID)
	}
}

func TestGCEClient_ListInstances(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/compute/alpha/projects/test-project/zones/us-east1-b/instances" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		resp := InstanceList{
			Items: []*Instance{
				{
					ID:   "111",
					Name: "instance-1",
					ResourceStatus: &ResourceStatus{
						PhysicalHostTopology: &PhysicalHostTopology{
							Host: "hash-111",
						},
					},
				},
				{
					ID:   "222",
					Name: "instance-2",
					// HashedHostID disabled
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	ctx := context.Background()
	client, err := NewClient(ctx, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	instances, err := client.ListInstances(ctx, "test-project", "us-east1-b")
	if err != nil {
		t.Fatalf("ListInstances failed: %v", err)
	}

	if len(instances) != 2 {
		t.Fatalf("expected 2 instances, got %d", len(instances))
	}

	host1, ok1 := ExtractHashedHostID(instances[0])
	if !ok1 || host1 != "hash-111" {
		t.Errorf("expected instance 0 to have hash-111, got %s (%v)", host1, ok1)
	}

	_, ok2 := ExtractHashedHostID(instances[1])
	if ok2 {
		t.Errorf("expected instance 1 to not have HashedHostID")
	}
}

func TestGCEClient_AggregatedListInstances(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/compute/alpha/projects/test-project/aggregated/instances" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		resp := InstanceAggregatedList{
			Items: map[string]InstancesScopedList{
				"zones/us-east1-b": {
					Instances: []*Instance{
						{ID: "inst-b1", Name: "gke-b1", ResourceStatus: &ResourceStatus{PhysicalHostTopology: &PhysicalHostTopology{Host: "host-b1"}}},
					},
				},
				"zones/us-east1-c": {
					Instances: []*Instance{
						{ID: "inst-c1", Name: "vm-c1", ResourceStatus: &ResourceStatus{PhysicalHostTopology: &PhysicalHostTopology{Host: "host-c1"}}},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	ctx := context.Background()
	client, err := NewClient(ctx, WithBaseURL(ts.URL), WithHTTPClient(ts.Client()))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	instances, err := client.AggregatedListInstances(ctx, "test-project")
	if err != nil {
		t.Fatalf("AggregatedListInstances failed: %v", err)
	}

	if len(instances) != 2 {
		t.Fatalf("expected 2 instances, got %d", len(instances))
	}
}

func TestCleanZoneName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"us-east1-b", "us-east1-b"},
		{"https://www.googleapis.com/compute/alpha/projects/p/zones/us-east1-d", "us-east1-d"},
		{"projects/p/zones/europe-west1-a", "europe-west1-a"},
	}

	for _, tt := range tests {
		got := cleanZoneName(tt.input)
		if got != tt.expected {
			t.Errorf("cleanZoneName(%s) = %s, expected %s", tt.input, got, tt.expected)
		}
	}
}
