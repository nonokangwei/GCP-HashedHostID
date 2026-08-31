package config

import (
	"os"
	"reflect"
	"testing"
)

func TestLoadConfig_Defaults(t *testing.T) {
	cfg, err := LoadConfig([]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.StoreType != "memory" {
		t.Errorf("expected default StoreType to be 'memory', got %s", cfg.StoreType)
	}
	if cfg.HTTPPort != 8080 {
		t.Errorf("expected default HTTPPort to be 8080, got %d", cfg.HTTPPort)
	}
	if cfg.GCEEndpoint != "https://compute.googleapis.com" {
		t.Errorf("expected default GCEEndpoint to be https://compute.googleapis.com, got %s", cfg.GCEEndpoint)
	}
}

func TestLoadConfig_Flags(t *testing.T) {
	args := []string{
		"-project=test-project",
		"-zones=us-east1-b,us-east1-c",
		"-skip-init=true",
		"-store-type=redis",
		"-redis-host=10.0.0.5",
		"-redis-port=6380",
		"-redis-password=secret",
		"-redis-db=2",
		"-pubsub-topic=system-events",
		"-pubsub-subscription=sub-events",
		"-port=9090",
		"-k8s-dry-run=true",
	}

	cfg, err := LoadConfig(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.GCPProject != "test-project" {
		t.Errorf("expected GCPProject 'test-project', got %s", cfg.GCPProject)
	}
	if !reflect.DeepEqual(cfg.GCEZones, []string{"us-east1-b", "us-east1-c"}) {
		t.Errorf("expected GCEZones [us-east1-b us-east1-c], got %v", cfg.GCEZones)
	}
	if !cfg.SkipInit {
		t.Errorf("expected SkipInit to be true")
	}
	if cfg.StoreType != "redis" {
		t.Errorf("expected StoreType 'redis', got %s", cfg.StoreType)
	}
	if cfg.RedisHost != "10.0.0.5" || cfg.RedisPort != 6380 || cfg.RedisPassword != "secret" || cfg.RedisDB != 2 {
		t.Errorf("redis config mismatch: %+v", cfg)
	}
	if cfg.PubSubProject != "test-project" {
		t.Errorf("expected PubSubProject to fallback to GCPProject 'test-project', got %s", cfg.PubSubProject)
	}
	if cfg.PubSubTopic != "system-events" || cfg.PubSubSubscription != "sub-events" {
		t.Errorf("pubsub config mismatch: %+v", cfg)
	}
	if cfg.HTTPPort != 9090 {
		t.Errorf("expected HTTPPort 9090, got %d", cfg.HTTPPort)
	}
	if !cfg.K8sDryRun {
		t.Errorf("expected K8sDryRun to be true")
	}
}

func TestLoadConfig_EnvVars(t *testing.T) {
	os.Setenv("GCP_PROJECT", "env-project")
	os.Setenv("STORE_TYPE", "redis")
	os.Setenv("REDIS_HOST", "192.168.1.100")
	defer func() {
		os.Unsetenv("GCP_PROJECT")
		os.Unsetenv("STORE_TYPE")
		os.Unsetenv("REDIS_HOST")
	}()

	cfg, err := LoadConfig([]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.GCPProject != "env-project" {
		t.Errorf("expected GCPProject 'env-project', got %s", cfg.GCPProject)
	}
	if cfg.StoreType != "redis" {
		t.Errorf("expected StoreType 'redis', got %s", cfg.StoreType)
	}
	if cfg.RedisHost != "192.168.1.100" {
		t.Errorf("expected RedisHost '192.168.1.100', got %s", cfg.RedisHost)
	}
}

func TestLoadConfig_InvalidStoreType(t *testing.T) {
	_, err := LoadConfig([]string{"-store-type=mongo"})
	if err == nil {
		t.Errorf("expected error for invalid store type, got nil")
	}
}
