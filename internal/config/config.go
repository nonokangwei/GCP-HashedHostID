package config

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config represents the complete application configuration.
type Config struct {
	// GCP Project to monitor GCE instances
	GCPProject string `json:"gcp_project"`

	// GCEZones is a list of zones to scan. If empty, all zones in the project are scanned.
	GCEZones []string `json:"gce_zones"`

	// SkipInit skips the initial full scan of GCE instances.
	SkipInit bool `json:"skip_init"`

	// Storage configuration
	StoreType     string `json:"store_type"` // "memory" or "redis"
	RedisHost     string `json:"redis_host"`
	RedisPort     int    `json:"redis_port"`
	RedisPassword string `json:"redis_password"`
	RedisDB       int    `json:"redis_db"`

	// Pub/Sub configuration
	PubSubProject      string `json:"pubsub_project"`
	PubSubTopic        string `json:"pubsub_topic"`
	PubSubSubscription string `json:"pubsub_subscription"`

	// HTTP Server configuration
	HTTPHost string `json:"http_host"`
	HTTPPort int    `json:"http_port"`

	// Kubernetes / GKE configuration
	Kubeconfig string `json:"kubeconfig"`
	K8sDryRun  bool   `json:"k8s_dry_run"`

	// GCE Endpoint (default: https://compute.googleapis.com)
	GCEEndpoint string `json:"gce_endpoint"`
}

// LoadConfig loads configuration from CLI flags, environment variables, and defaults.
func LoadConfig(args []string) (*Config, error) {
	fs := flag.NewFlagSet("hosthashedid", flag.ContinueOnError)

	var (
		gcpProject         = fs.String("project", getEnv("GCP_PROJECT", ""), "GCP Project ID")
		gceZonesStr        = fs.String("zones", getEnv("GCE_ZONES", ""), "Comma-separated list of GCE zones (empty for all zones)")
		skipInit           = fs.Bool("skip-init", getEnvBool("SKIP_INIT", false), "Skip initialization scan")
		storeType          = fs.String("store-type", getEnv("STORE_TYPE", "memory"), "Storage backend: 'memory' or 'redis'")
		redisHost          = fs.String("redis-host", getEnv("REDIS_HOST", "127.0.0.1"), "Redis server host/IP")
		redisPort          = fs.Int("redis-port", getEnvInt("REDIS_PORT", 6379), "Redis server port")
		redisPassword      = fs.String("redis-password", getEnv("REDIS_PASSWORD", ""), "Redis server password")
		redisDB            = fs.Int("redis-db", getEnvInt("REDIS_DB", 0), "Redis database index")
		pubsubProject      = fs.String("pubsub-project", getEnv("PUBSUB_PROJECT", ""), "GCP Project ID for Pub/Sub (defaults to GCP Project)")
		pubsubTopic        = fs.String("pubsub-topic", getEnv("PUBSUB_TOPIC", ""), "Pub/Sub Topic name")
		pubsubSubscription = fs.String("pubsub-subscription", getEnv("PUBSUB_SUBSCRIPTION", ""), "Pub/Sub Subscription name")
		httpHost           = fs.String("host", getEnv("HTTP_HOST", "0.0.0.0"), "HTTP Server bind host")
		httpPort           = fs.Int("port", getEnvInt("HTTP_PORT", 8080), "HTTP Server listen port")
		kubeconfig         = fs.String("kubeconfig", getEnv("KUBECONFIG", ""), "Path to kubeconfig file (optional)")
		k8sDryRun          = fs.Bool("k8s-dry-run", getEnvBool("K8S_DRY_RUN", false), "Enable K8s dry-run mode (logs labels without applying)")
		gceEndpoint        = fs.String("gce-endpoint", getEnv("GCE_ENDPOINT", "https://compute.googleapis.com"), "GCE Alpha API base endpoint")
	)

	if err := fs.Parse(args); err != nil {
		return nil, fmt.Errorf("failed to parse flags: %w", err)
	}

	cfg := &Config{
		GCPProject:         *gcpProject,
		SkipInit:           *skipInit,
		StoreType:          strings.ToLower(strings.TrimSpace(*storeType)),
		RedisHost:          *redisHost,
		RedisPort:          *redisPort,
		RedisPassword:      *redisPassword,
		RedisDB:            *redisDB,
		PubSubProject:      *pubsubProject,
		PubSubTopic:        *pubsubTopic,
		PubSubSubscription: *pubsubSubscription,
		HTTPHost:           *httpHost,
		HTTPPort:           *httpPort,
		Kubeconfig:         *kubeconfig,
		K8sDryRun:          *k8sDryRun,
		GCEEndpoint:        strings.TrimRight(*gceEndpoint, "/"),
	}

	if *gceZonesStr != "" {
		for _, z := range strings.Split(*gceZonesStr, ",") {
			z = strings.TrimSpace(z)
			if z != "" {
				cfg.GCEZones = append(cfg.GCEZones, z)
			}
		}
	}

	if cfg.PubSubProject == "" {
		cfg.PubSubProject = cfg.GCPProject
	}

	if cfg.StoreType != "memory" && cfg.StoreType != "redis" {
		return nil, fmt.Errorf("invalid store-type %q, must be 'memory' or 'redis'", cfg.StoreType)
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	b, err := strconv.ParseBool(val)
	if err != nil {
		return fallback
	}
	return b
}

func getEnvInt(key string, fallback int) int {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	i, err := strconv.Atoi(val)
	if err != nil {
		return fallback
	}
	return i
}
