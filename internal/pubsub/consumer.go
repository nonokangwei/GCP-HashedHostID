package pubsub

import (
	"context"
	"fmt"
	"log"
	"strings"

	gcppubsub "cloud.google.com/go/pubsub"
)

// EventHandler is called when a relevant GCE host event is received.
type EventHandler interface {
	HandleHostEvent(ctx context.Context, event *HostEvent) error
}

// EventHandlerFunc is an adapter allowing the use of ordinary functions as EventHandlers.
type EventHandlerFunc func(ctx context.Context, event *HostEvent) error

func (f EventHandlerFunc) HandleHostEvent(ctx context.Context, event *HostEvent) error {
	return f(ctx, event)
}

// Consumer consumes Cloud Logging events from a GCP Pub/Sub subscription.
type Consumer struct {
	projectID      string
	subscriptionID string
	client         *gcppubsub.Client
	handler        EventHandler
}

// NewConsumer creates a new Pub/Sub Consumer.
func NewConsumer(ctx context.Context, projectID, subscriptionID string, handler EventHandler) (*Consumer, error) {
	if projectID == "" || subscriptionID == "" {
		return nil, fmt.Errorf("pubsub projectID and subscriptionID must not be empty")
	}

	client, err := gcppubsub.NewClient(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to create Pub/Sub client: %w", err)
	}

	return &Consumer{
		projectID:      projectID,
		subscriptionID: subscriptionID,
		client:         client,
		handler:        handler,
	}, nil
}

// Start starts receiving messages from the Pub/Sub subscription. It blocks until ctx is canceled.
func (c *Consumer) Start(ctx context.Context) error {
	subID := c.subscriptionID
	if strings.Contains(subID, "/subscriptions/") {
		parts := strings.Split(subID, "/subscriptions/")
		subID = parts[len(parts)-1]
	}

	log.Printf("[PubSub] Starting listener on subscription %s (project: %s)", subID, c.projectID)
	sub := c.client.Subscription(subID)

	err := sub.Receive(ctx, func(msgCtx context.Context, msg *gcppubsub.Message) {
		event, isTarget, err := ParseAuditLog(msg.Data)
		if err != nil {
			log.Printf("[PubSub WARN] Failed to parse message (id=%s): %v", msg.ID, err)
			msg.Ack() // Ack unparseable non-audit messages to avoid poison pill
			return
		}

		if !isTarget {
			// Not a host migration/restart event, acknowledge and ignore
			msg.Ack()
			return
		}

		log.Printf("[PubSub] Received event %s for instance=%s (id=%s, zone=%s)",
			event.MethodName, event.InstanceName, event.InstanceID, event.Zone)

		if c.handler != nil {
			if err := c.handler.HandleHostEvent(msgCtx, event); err != nil {
				log.Printf("[PubSub ERROR] Error handling event for instance %s: %v", event.InstanceName, err)
				msg.Nack()
				return
			}
		}

		msg.Ack()
	})

	if err != nil {
		return fmt.Errorf("Pub/Sub subscription receive error: %w", err)
	}

	return nil
}

// Close closes the Pub/Sub client.
func (c *Consumer) Close() error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}
