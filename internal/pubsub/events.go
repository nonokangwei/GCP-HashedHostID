package pubsub

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var (
	// resourceNamePattern parses "projects/{project}/zones/{zone}/instances/{instance}"
	resourceNameRegex = regexp.MustCompile(`projects/([^/]+)/zones/([^/]+)/instances/([^/]+)`)
)

const (
	MethodMigrateOnHostMaintenance = "compute.instances.migrateOnHostMaintenance"
	MethodAutomaticRestart         = "compute.instances.automaticRestart"
	MethodAutorestart              = "compute.instances.autorestart"
)

// AuditLogMessage represents the GCP Cloud Logging message structure.
type AuditLogMessage struct {
	ProtoPayload *ProtoPayload   `json:"protoPayload,omitempty"`
	InsertID     string          `json:"insertId,omitempty"`
	Resource     *ResourceInfo   `json:"resource,omitempty"`
	Timestamp    time.Time       `json:"timestamp,omitempty"`
	Severity     string          `json:"severity,omitempty"`
	LogName      string          `json:"logName,omitempty"`
}

// ProtoPayload represents the AuditLog entry inside Cloud Logging.
type ProtoPayload struct {
	Type               string            `json:"@type,omitempty"`
	ServiceName        string            `json:"serviceName,omitempty"`
	MethodName         string            `json:"methodName,omitempty"`
	ResourceName       string            `json:"resourceName,omitempty"`
	Status             *AuditStatus      `json:"status,omitempty"`
	AuthenticationInfo *AuthInfo         `json:"authenticationInfo,omitempty"`
}

// AuditStatus represents the status message of an audit log.
type AuditStatus struct {
	Message string `json:"message,omitempty"`
}

// AuthInfo contains authentication details.
type AuthInfo struct {
	PrincipalEmail string `json:"principalEmail,omitempty"`
}

// ResourceInfo represents the monitored resource.
type ResourceInfo struct {
	Type   string            `json:"type,omitempty"`
	Labels map[string]string `json:"labels,omitempty"`
}

// HostEvent represents a parsed, normalized GCE host lifecycle event.
type HostEvent struct {
	ProjectID    string    `json:"project_id"`
	Zone         string    `json:"zone"`
	InstanceID   string    `json:"instance_id"`
	InstanceName string    `json:"instance_name"`
	MethodName   string    `json:"method_name"`
	Timestamp    time.Time `json:"timestamp"`
}

// ParseAuditLog parses raw JSON bytes into a HostEvent.
// Returns (event, isTargetEvent, error).
func ParseAuditLog(data []byte) (*HostEvent, bool, error) {
	var msg AuditLogMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, false, fmt.Errorf("failed to unmarshal audit log JSON: %w", err)
	}

	if msg.ProtoPayload == nil {
		return nil, false, nil
	}

	method := msg.ProtoPayload.MethodName
	if !IsTargetHostEventMethod(method) {
		return nil, false, nil
	}

	event := &HostEvent{
		MethodName: method,
		Timestamp:  msg.Timestamp,
	}

	// 1. Extract from Resource.Labels if present
	if msg.Resource != nil && msg.Resource.Labels != nil {
		event.ProjectID = msg.Resource.Labels["project_id"]
		event.Zone = msg.Resource.Labels["zone"]
		event.InstanceID = msg.Resource.Labels["instance_id"]
	}

	// 2. Extract from ProtoPayload.ResourceName
	if msg.ProtoPayload.ResourceName != "" {
		matches := resourceNameRegex.FindStringSubmatch(msg.ProtoPayload.ResourceName)
		if len(matches) == 4 {
			if event.ProjectID == "" {
				event.ProjectID = matches[1]
			}
			if event.Zone == "" {
				event.Zone = matches[2]
			}
			event.InstanceName = matches[3]
		}
	}

	// If instanceName is still empty, and we have resourceName as just the name
	if event.InstanceName == "" && msg.ProtoPayload.ResourceName != "" {
		parts := strings.Split(msg.ProtoPayload.ResourceName, "/")
		event.InstanceName = parts[len(parts)-1]
	}

	return event, true, nil
}

// IsTargetHostEventMethod checks if the methodName is an event that changes the host.
func IsTargetHostEventMethod(method string) bool {
	switch method {
	case MethodMigrateOnHostMaintenance,
		MethodAutomaticRestart,
		MethodAutorestart:
		return true
	default:
		return false
	}
}
