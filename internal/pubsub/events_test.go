package pubsub

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseAuditLog_MigrateEvent(t *testing.T) {
	// Read examples/migrate_event.json
	data, err := os.ReadFile(filepath.Join("..", "..", "examples", "migrate_event.json"))
	if err != nil {
		t.Fatalf("failed to read migrate_event.json: %v", err)
	}

	event, isTarget, err := ParseAuditLog(data)
	if err != nil {
		t.Fatalf("ParseAuditLog failed: %v", err)
	}

	if !isTarget {
		t.Fatalf("expected isTarget to be true")
	}

	if event.MethodName != MethodMigrateOnHostMaintenance {
		t.Errorf("expected methodName %s, got %s", MethodMigrateOnHostMaintenance, event.MethodName)
	}
	if event.ProjectID != "xhs-us-default" {
		t.Errorf("expected projectID xhs-us-default, got %s", event.ProjectID)
	}
	if event.Zone != "us-east1-b" {
		t.Errorf("expected zone us-east1-b, got %s", event.Zone)
	}
	if event.InstanceID != "2449360396712485978" {
		t.Errorf("expected instanceID 2449360396712485978, got %s", event.InstanceID)
	}
	if event.InstanceName != "guseast1b-db-gcpeast1-yunqi-1-ae-cuybj-1" {
		t.Errorf("expected instanceName guseast1b-db-gcpeast1-yunqi-1-ae-cuybj-1, got %s", event.InstanceName)
	}
}

func TestParseAuditLog_RestartEvent(t *testing.T) {
	// Read examples/restart_event.json
	data, err := os.ReadFile(filepath.Join("..", "..", "examples", "restart_event.json"))
	if err != nil {
		t.Fatalf("failed to read restart_event.json: %v", err)
	}

	event, isTarget, err := ParseAuditLog(data)
	if err != nil {
		t.Fatalf("ParseAuditLog failed: %v", err)
	}

	if !isTarget {
		t.Fatalf("expected isTarget to be true")
	}

	if event.MethodName != MethodAutomaticRestart {
		t.Errorf("expected methodName %s, got %s", MethodAutomaticRestart, event.MethodName)
	}
	if event.ProjectID != "xhs-us-default" {
		t.Errorf("expected projectID xhs-us-default, got %s", event.ProjectID)
	}
	if event.Zone != "us-east1-b" {
		t.Errorf("expected zone us-east1-b, got %s", event.Zone)
	}
	if event.InstanceID != "320540705183992811" {
		t.Errorf("expected instanceID 320540705183992811, got %s", event.InstanceID)
	}
	if event.InstanceName != "gke-gcpeast1-kafka-1-gcpeast1-kafka-b-3a0e4bdc-9ssx" {
		t.Errorf("expected instanceName gke-gcpeast1-kafka-1-gcpeast1-kafka-b-3a0e4bdc-9ssx, got %s", event.InstanceName)
	}
}

func TestParseAuditLog_IrrelevantEvent(t *testing.T) {
	irrelevantJSON := []byte(`{
		"protoPayload": {
			"@type": "type.googleapis.com/google.cloud.audit.AuditLog",
			"methodName": "compute.instances.insert"
		}
	}`)

	event, isTarget, err := ParseAuditLog(irrelevantJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if isTarget || event != nil {
		t.Errorf("expected non-target event, got %v", event)
	}
}
