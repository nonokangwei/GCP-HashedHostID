package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"hosthashedid/internal/api"
	"hosthashedid/internal/config"
	"hosthashedid/internal/gce"
	"hosthashedid/internal/k8s"
	"hosthashedid/internal/pubsub"
	"hosthashedid/internal/store"
	"hosthashedid/internal/syncer"
)

func main() {
	log.Println("[Main] Starting GCP GCE HashedHostID Sync Service...")

	cfg, err := config.LoadConfig(os.Args[1:])
	if err != nil {
		log.Fatalf("[Main FATAL] Configuration error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. Initialize Store
	var st store.Store
	if cfg.StoreType == "redis" {
		log.Printf("[Store] Initializing Redis store at %s:%d (db: %d)", cfg.RedisHost, cfg.RedisPort, cfg.RedisDB)
		redisStore, err := store.NewRedisStore(ctx, cfg.RedisHost, cfg.RedisPort, cfg.RedisPassword, cfg.RedisDB)
		if err != nil {
			log.Fatalf("[Main FATAL] Failed to connect to Redis: %v", err)
		}
		st = redisStore
	} else {
		log.Println("[Store] Initializing in-memory store")
		st = store.NewMemoryStore()
	}
	defer st.Close()

	// 2. Initialize GCE Client
	var gceOpts []gce.ClientOption
	if cfg.GCEEndpoint != "" {
		gceOpts = append(gceOpts, gce.WithBaseURL(cfg.GCEEndpoint))
	}
	gceClient, err := gce.NewClient(ctx, gceOpts...)
	if err != nil {
		log.Fatalf("[Main FATAL] Failed to initialize GCE client: %v", err)
	}

	// 3. Initialize K8s Labeler
	labeler, err := k8s.NewNodeLabeler(cfg.Kubeconfig, cfg.K8sDryRun)
	if err != nil {
		log.Fatalf("[Main FATAL] Failed to initialize Kubernetes node labeler: %v", err)
	}

	// 4. Initialize Syncer
	syncService := syncer.NewSyncer(gceClient, st, labeler, cfg.GCPProject, cfg.GCEZones)

	// 5. Initial Scan
	if !cfg.SkipInit {
		if cfg.GCPProject == "" {
			log.Println("[Main WARN] GCP_PROJECT is not set; skipping initial scan. Use /api/v1/sync to trigger scans.")
		} else {
			log.Printf("[Main] Executing initial GCE scan for project %s...", cfg.GCPProject)
			go func() {
				scanCtx, scanCancel := context.WithTimeout(context.Background(), 5*time.Minute)
				defer scanCancel()
				if res, err := syncService.FullScan(scanCtx); err != nil {
					log.Printf("[Main ERROR] Initial scan failed: %v", err)
				} else {
					log.Printf("[Main] Initial scan finished: %d instances, %d with HashedHostID",
						res.ScannedInstances, res.MatchedInstances)
				}
			}()
		}
	} else {
		log.Println("[Main] Initial scan skipped as requested (--skip-init)")
	}

	// 6. Pub/Sub Consumer
	if cfg.PubSubSubscription != "" && cfg.PubSubProject != "" {
		consumer, err := pubsub.NewConsumer(ctx, cfg.PubSubProject, cfg.PubSubSubscription, syncService)
		if err != nil {
			log.Printf("[PubSub WARN] Could not initialize Pub/Sub consumer: %v", err)
		} else {
			defer consumer.Close()
			go func() {
				if err := consumer.Start(ctx); err != nil && err != context.Canceled {
					log.Printf("[PubSub ERROR] Consumer exited with error: %v", err)
				}
			}()
		}
	} else {
		log.Println("[PubSub] Pub/Sub subscription not configured; running without real-time event listener")
	}

	// 7. HTTP REST Server
	httpServer := api.NewServer(cfg.HTTPHost, cfg.HTTPPort, syncService, st)
	go func() {
		if err := httpServer.Start(); err != nil {
			log.Printf("[HTTP ERROR] %v", err)
		}
	}()

	fmt.Printf("[Main] Service successfully started on http://%s:%d\n", cfg.HTTPHost, cfg.HTTPPort)

	// 8. Graceful Shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigChan
	log.Printf("[Main] Received signal %v. Initiating graceful shutdown...", sig)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	cancel() // Cancel background contexts (Pub/Sub receiver)

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("[Main ERROR] HTTP server shutdown error: %v", err)
	}

	log.Println("[Main] Server gracefully stopped.")
}
