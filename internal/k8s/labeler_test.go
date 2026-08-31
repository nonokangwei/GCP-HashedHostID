package k8s

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestIsGKENode(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		{"gke-cluster-pool-1-abc", true},
		{"GKE-cluster-node-2", true},
		{"gke-z3demo-z3-22-hdb-a1bfdebb-7qdh", true},
		{"my-custom-vm-instance", false},
		{"db-gcpeast1-yunqi-1", false},
	}

	for _, tt := range tests {
		got := IsGKENode(tt.name)
		if got != tt.expected {
			t.Errorf("IsGKENode(%q) = %v, expected %v", tt.name, got, tt.expected)
		}
	}
}

func TestLabelNode_FakeClient(t *testing.T) {
	ctx := context.Background()
	nodeName := "gke-node-test-1"

	// Create fake node in fake client
	initialNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
			Labels: map[string]string{
				"kubernetes.io/os": "linux",
			},
		},
	}
	fakeClientset := fake.NewSimpleClientset(initialNode)
	labeler := NewNodeLabelerWithClientset(fakeClientset, false)

	hashedID := "abc123456789hostid"
	err := labeler.LabelNode(ctx, nodeName, hashedID)
	if err != nil {
		t.Fatalf("LabelNode failed: %v", err)
	}

	// Fetch updated node
	updatedNode, err := fakeClientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get node: %v", err)
	}

	if updatedNode.Labels[LabelHashedHostID] != hashedID {
		t.Errorf("expected label %s=%s, got %s", LabelHashedHostID, hashedID, updatedNode.Labels[LabelHashedHostID])
	}
}

func TestLabelNode_DryRun(t *testing.T) {
	ctx := context.Background()
	labeler := &Client{dryRun: true}

	err := labeler.LabelNode(ctx, "gke-node-dryrun", "hash-dryrun-123")
	if err != nil {
		t.Errorf("expected dry-run to succeed without error, got %v", err)
	}
}
