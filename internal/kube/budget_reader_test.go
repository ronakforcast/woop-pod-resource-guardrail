package kube

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ronakforcast/woop-pod-resource-guardrail/internal/webhook"
)

func TestDisableOptimizationPreservesWOOPConfiguration(t *testing.T) {
	var patched string
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.Method {
		case http.MethodGet:
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"metadata":{"annotations":{"workloads.cast.ai/configuration":"scalingPolicyName: customer-policy\nvertical:\n  cpu:\n    max: 12\n  optimization: on\n"}}}`))}, nil
		case http.MethodPatch:
			var body struct {
				Metadata struct {
					Annotations map[string]string `json:"annotations"`
				} `json:"metadata"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			patched = body.Metadata.Annotations[WOOPConfigurationAnnotation]
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}, nil
		default:
			t.Fatalf("unexpected method %s", request.Method)
		}
		return nil, nil
	})}
	reader := &BudgetReader{baseURL: "https://kubernetes.test", token: "test", client: client}
	if err := reader.DisableOptimization(context.Background(), webhook.TargetRef{APIVersion: "apps/v1", Kind: "Deployment", Name: "payments"}, "ns"); err != nil {
		t.Fatal(err)
	}
	if patched != "scalingPolicyName: customer-policy\nvertical:\n  cpu:\n    max: 12\n  optimization: \"off\"\n" {
		t.Fatalf("unexpected patched configuration:\n%s", patched)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

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
