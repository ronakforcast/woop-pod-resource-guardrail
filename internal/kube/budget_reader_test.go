package kube

import (
	"testing"

	"github.com/castai-labs/woop-pod-resource-guardrail/internal/webhook"
)

func TestTargetPath(t *testing.T) {
	tests := map[string]string{
		"Deployment":  "/apis/apps/v1/namespaces/ns/deployments/name",
		"StatefulSet": "/apis/apps/v1/namespaces/ns/statefulsets/name",
		"ReplicaSet":  "/apis/apps/v1/namespaces/ns/replicasets/name",
		"DaemonSet":   "/apis/apps/v1/namespaces/ns/daemonsets/name",
		"Rollout":     "/apis/argoproj.io/v1alpha1/namespaces/ns/rollouts/name",
	}

	for kind, want := range tests {
		got, err := targetPath(webhook.TargetRef{APIVersion: apiVersionFor(kind), Kind: kind, Name: "name"}, "ns")
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", kind, err)
		}
		if got != want {
			t.Errorf("%s: got %q, want %q", kind, got, want)
		}
	}
}

func TestTargetPathRejectsUnsupportedKind(t *testing.T) {
	if _, err := targetPath(webhook.TargetRef{APIVersion: "batch/v1", Kind: "Job", Name: "name"}, "ns"); err == nil {
		t.Fatal("expected unsupported kind error")
	}
}

func apiVersionFor(kind string) string {
	if kind == "Rollout" {
		return "argoproj.io/v1alpha1"
	}
	return "apps/v1"
}
