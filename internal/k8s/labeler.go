package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	// LabelHashedHostID is the Kubernetes node label key for the GCE HashedHostID.
	LabelHashedHostID = "hashedhostid"
)

// NodeLabeler defines the interface for labeling Kubernetes nodes.
type NodeLabeler interface {
	LabelNode(ctx context.Context, nodeName string, hashedHostID string) error
	IsGKENode(instanceName string) bool
}

// IsGKENode checks if an instance belongs to a GKE cluster based on standard naming convention.
func IsGKENode(instanceName string) bool {
	return strings.HasPrefix(strings.ToLower(instanceName), "gke-")
}

// Client implements NodeLabeler using Kubernetes client-go.
type Client struct {
	clientset kubernetes.Interface
	dryRun    bool
}

// NewNodeLabeler creates a new Kubernetes NodeLabeler.
// If dryRun is true, changes will only be logged.
// If kubeconfigPath is provided, it will load configuration from that file;
// otherwise, it attempts in-cluster config, falling back to dryRun mode if outside a cluster.
func NewNodeLabeler(kubeconfigPath string, dryRun bool) (*Client, error) {
	if dryRun {
		return &Client{dryRun: true}, nil
	}

	var config *rest.Config
	var err error

	if kubeconfigPath != "" {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load kubeconfig from %s: %w", kubeconfigPath, err)
		}
	} else {
		config, err = rest.InClusterConfig()
		if err != nil {
			// Fallback: Check if default ~/.kube/config exists
			defaultKubeconfig := clientcmd.RecommendedHomeFile
			if defaultCfg, fileErr := clientcmd.BuildConfigFromFlags("", defaultKubeconfig); fileErr == nil {
				config = defaultCfg
				log.Printf("[K8s] Loaded default kubeconfig from %s", defaultKubeconfig)
			} else {
				log.Printf("[WARN] K8s config not available (%v); falling back to dry-run mode", err)
				return &Client{dryRun: true}, nil
			}
		}
	}

	cs, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes clientset: %w", err)
	}

	return &Client{
		clientset: cs,
		dryRun:    false,
	}, nil
}

// NewNodeLabelerWithClientset creates a NodeLabeler with an existing kubernetes.Interface (useful for testing).
func NewNodeLabelerWithClientset(clientset kubernetes.Interface, dryRun bool) *Client {
	return &Client{
		clientset: clientset,
		dryRun:    dryRun,
	}
}

// IsGKENode implements NodeLabeler.
func (c *Client) IsGKENode(instanceName string) bool {
	return IsGKENode(instanceName)
}

// LabelNode applies the label hashedhostid=<hashedHostID> to the specified Node.
func (c *Client) LabelNode(ctx context.Context, nodeName string, hashedHostID string) error {
	if c.dryRun || c.clientset == nil {
		log.Printf("[K8s DryRun] Label node %q with %s=%s", nodeName, LabelHashedHostID, hashedHostID)
		return nil
	}

	patchData := map[string]interface{}{
		"metadata": map[string]interface{}{
			"labels": map[string]string{
				LabelHashedHostID: hashedHostID,
			},
		},
	}

	patchBytes, err := json.Marshal(patchData)
	if err != nil {
		return fmt.Errorf("failed to marshal node patch: %w", err)
	}

	_, err = c.clientset.CoreV1().Nodes().Patch(ctx, nodeName, types.MergePatchType, patchBytes, metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("failed to patch node %q with label %s=%s: %w", nodeName, LabelHashedHostID, hashedHostID, err)
	}

	log.Printf("[K8s] Successfully labeled node %q with %s=%s", nodeName, LabelHashedHostID, hashedHostID)
	return nil
}
