package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ronakforcast/woop-pod-resource-guardrail/internal/guardrail"
)

type fakeBudgetReader struct {
	policy       Policy
	err          error
	disableCalls *[]TargetRef
	disableErr   error
}

func (f fakeBudgetReader) EnqueueDisable(_ context.Context, target TargetRef, _ string) error {
	if f.disableCalls != nil {
		*f.disableCalls = append(*f.disableCalls, target)
	}
	return f.disableErr
}

func (f fakeBudgetReader) PolicyFor(context.Context, TargetRef, string) (Policy, error) {
	return f.policy, f.err
}

func TestHandlerRejectsRecommendationOverCPUBudget(t *testing.T) {
	var disabled []TargetRef
	handler := NewHandler(fakeBudgetReader{policy: Policy{Budget: guardrail.Budget{CPU: "15", Memory: "60Gi"}, Containers: []string{"main", "worker", "sidecar"}}, disableCalls: &disabled})
	response := review(t, handler, `
{
  "apiVersion":"admission.k8s.io/v1",
  "kind":"AdmissionReview",
  "request":{
    "uid":"cpu-over",
    "namespace":"customer-workloads",
    "object":{
      "spec":{
        "targetRef":{"apiVersion":"apps/v1","kind":"Deployment","name":"payments"},
        "recommendation":[
          {"containerName":"main","requests":{"cpu":"10","memory":"20Gi"}},
          {"containerName":"worker","requests":{"cpu":"4","memory":"20Gi"}},
          {"containerName":"sidecar","requests":{"cpu":"2","memory":"2Gi"}}
        ]
      }
    }
  }
}`)

	if response.Response.Allowed {
		t.Fatal("expected oversized recommendation to be denied")
	}
	if response.Response.UID != "cpu-over" {
		t.Fatalf("response UID = %q", response.Response.UID)
	}
	want := "aggregate CPU request 16 exceeds pod budget 15; WOOP disable queued"
	if response.Response.Status.Message != want {
		t.Fatalf("message = %q, want %q", response.Response.Status.Message, want)
	}
	if len(disabled) != 1 || disabled[0].Name != "payments" {
		t.Fatalf("expected payments optimization to be disabled, got %#v", disabled)
	}
}

func TestHandlerReportsDisableFailureWhileDenyingRecommendation(t *testing.T) {
	handler := NewHandler(fakeBudgetReader{
		policy:     Policy{Budget: guardrail.Budget{CPU: "1"}, Containers: []string{"main"}},
		disableErr: fmt.Errorf("patch forbidden"),
	})
	response := review(t, handler, `{"request":{"uid":"disable-failed","namespace":"customer-workloads","object":{"spec":{"targetRef":{"apiVersion":"apps/v1","kind":"Deployment","name":"payments"},"recommendation":[{"containerName":"main","requests":{"cpu":"2"}}]}}}}`)
	if response.Response.Allowed {
		t.Fatal("unsafe recommendation must remain denied when disabling fails")
	}
	if !strings.Contains(response.Response.Status.Message, "failed to queue WOOP disable") {
		t.Fatalf("expected disable failure in response, got %q", response.Response.Status.Message)
	}
}

func TestHandlerDryRunDoesNotQueueDisable(t *testing.T) {
	var disabled []TargetRef
	handler := NewHandler(fakeBudgetReader{policy: Policy{Budget: guardrail.Budget{CPU: "1"}, Containers: []string{"main"}}, disableCalls: &disabled})
	response := review(t, handler, `{"request":{"uid":"dry","dryRun":true,"namespace":"customer-workloads","object":{"spec":{"targetRef":{"apiVersion":"apps/v1","kind":"Deployment","name":"payments"},"recommendation":[{"containerName":"main","requests":{"cpu":"2"}}]}}}}`)
	if response.Response.Allowed || len(disabled) != 0 || !strings.Contains(response.Response.Status.Message, "dry-run") {
		t.Fatalf("dry-run must deny without queueing: allowed=%v calls=%v message=%q", response.Response.Allowed, disabled, response.Response.Status.Message)
	}
}

func TestHandlerAllowsRecommendationWithinBudgets(t *testing.T) {
	handler := NewHandler(fakeBudgetReader{policy: Policy{Budget: guardrail.Budget{CPU: "15", Memory: "60Gi"}, Containers: []string{"main", "sidecar"}}})
	response := review(t, handler, `
{"request":{"uid":"safe","namespace":"customer-workloads","object":{"spec":{
  "targetRef":{"apiVersion":"apps/v1","kind":"Deployment","name":"payments"},
  "recommendation":[
    {"containerName":"main","requests":{"cpu":"8","memory":"40Gi"}},
    {"containerName":"sidecar","requests":{"cpu":"1500m","memory":"2Gi"}}
  ]
}}}}`)

	if !response.Response.Allowed {
		t.Fatalf("expected safe recommendation to be allowed: %s", response.Response.Status.Message)
	}
}

func TestHandlerRejectsIncompleteContainerSet(t *testing.T) {
	handler := NewHandler(fakeBudgetReader{policy: Policy{Budget: guardrail.Budget{CPU: "15"}, Containers: []string{"main", "sidecar"}}})
	response := review(t, handler, `{"request":{"uid":"missing","namespace":"customer-workloads","object":{"spec":{"targetRef":{"apiVersion":"apps/v1","kind":"Deployment","name":"payments"},"recommendation":[{"containerName":"main","requests":{"cpu":"8"}}]}}}}`)
	if response.Response.Allowed || response.Response.Status.Message != `Recommendation is missing container "sidecar"` {
		t.Fatalf("expected missing container denial, got allowed=%v message=%q", response.Response.Allowed, response.Response.Status.Message)
	}
}

func TestHandlerRejectsMissingBudgetedResource(t *testing.T) {
	handler := NewHandler(fakeBudgetReader{policy: Policy{Budget: guardrail.Budget{CPU: "15", Memory: "60Gi"}, Containers: []string{"main"}}})
	response := review(t, handler, `{"request":{"uid":"missing-memory","namespace":"customer-workloads","object":{"spec":{"targetRef":{"apiVersion":"apps/v1","kind":"Deployment","name":"payments"},"recommendation":[{"containerName":"main","requests":{"cpu":"8"}}]}}}}`)
	if response.Response.Allowed || !strings.Contains(response.Response.Status.Message, "missing memory") {
		t.Fatalf("expected missing memory denial, got allowed=%v message=%q", response.Response.Allowed, response.Response.Status.Message)
	}
}

func review(t *testing.T, handler http.Handler, body string) admissionReview {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/validate", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var response admissionReview
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return response
}
