package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/ronakforcast/woop-pod-resource-guardrail/internal/guardrail"
)

type TargetRef struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
}

type BudgetReader interface {
	PolicyFor(ctx context.Context, target TargetRef, namespace string) (Policy, error)
}

type Policy struct {
	Budget     guardrail.Budget
	Containers []string
}

type Handler struct {
	budgets BudgetReader
}

func NewHandler(budgets BudgetReader) http.Handler {
	return &Handler{budgets: budgets}
}

type admissionReview struct {
	APIVersion string             `json:"apiVersion,omitempty"`
	Kind       string             `json:"kind,omitempty"`
	Request    *admissionRequest  `json:"request,omitempty"`
	Response   *admissionResponse `json:"response,omitempty"`
}

type admissionRequest struct {
	UID       string          `json:"uid"`
	Namespace string          `json:"namespace"`
	Object    json.RawMessage `json:"object"`
}

type admissionResponse struct {
	UID     string          `json:"uid"`
	Allowed bool            `json:"allowed"`
	Status  admissionStatus `json:"status,omitempty"`
}

type admissionStatus struct {
	Message string `json:"message,omitempty"`
}

type recommendation struct {
	Spec struct {
		TargetRef      TargetRef                 `json:"targetRef"`
		Recommendation []containerRecommendation `json:"recommendation"`
	} `json:"spec"`
}

type containerRecommendation struct {
	ContainerName string `json:"containerName"`
	Requests      struct {
		CPU    string `json:"cpu"`
		Memory string `json:"memory"`
	} `json:"requests"`
}

func (h *Handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "POST required", http.StatusMethodNotAllowed)
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, 1<<20)
	var review admissionReview
	if err := json.NewDecoder(request.Body).Decode(&review); err != nil {
		http.Error(writer, fmt.Sprintf("decode AdmissionReview: %v", err), http.StatusBadRequest)
		return
	}
	if review.Request == nil {
		http.Error(writer, "AdmissionReview request is required", http.StatusBadRequest)
		return
	}
	response := h.evaluate(request.Context(), review.Request)
	writeReview(writer, review.Request.UID, response)
}

func (h *Handler) evaluate(ctx context.Context, request *admissionRequest) admissionResponse {
	response := admissionResponse{UID: request.UID, Allowed: false}
	var object recommendation
	if err := json.Unmarshal(request.Object, &object); err != nil {
		response.Status.Message = fmt.Sprintf("decode Recommendation: %v", err)
		return response
	}
	if object.Spec.TargetRef.Name == "" || object.Spec.TargetRef.Kind == "" {
		response.Status.Message = "Recommendation spec.targetRef name and kind are required"
		return response
	}
	policy, err := h.budgets.PolicyFor(ctx, object.Spec.TargetRef, request.Namespace)
	if err != nil {
		response.Status.Message = fmt.Sprintf("read workload budget: %v", err)
		return response
	}
	if len(object.Spec.Recommendation) == 0 {
		response.Status.Message = "Recommendation spec.recommendation must not be empty"
		return response
	}
	expected := make(map[string]bool, len(policy.Containers))
	for _, name := range policy.Containers {
		expected[name] = false
	}
	containers := make([]guardrail.ContainerRecommendation, 0, len(object.Spec.Recommendation))
	for _, item := range object.Spec.Recommendation {
		if item.ContainerName == "" {
			response.Status.Message = "Recommendation containerName must not be empty"
			return response
		}
		seen, exists := expected[item.ContainerName]
		if !exists || seen {
			response.Status.Message = fmt.Sprintf("Recommendation has unknown or duplicate container %q", item.ContainerName)
			return response
		}
		expected[item.ContainerName] = true
		if policy.Budget.CPU != "" && item.Requests.CPU == "" {
			response.Status.Message = fmt.Sprintf("Recommendation container %q is missing CPU request", item.ContainerName)
			return response
		}
		if policy.Budget.Memory != "" && item.Requests.Memory == "" {
			response.Status.Message = fmt.Sprintf("Recommendation container %q is missing memory request", item.ContainerName)
			return response
		}
		containers = append(containers, guardrail.ContainerRecommendation{
			Name:   item.ContainerName,
			CPU:    item.Requests.CPU,
			Memory: item.Requests.Memory,
		})
	}
	for name, seen := range expected {
		if !seen {
			response.Status.Message = fmt.Sprintf("Recommendation is missing container %q", name)
			return response
		}
	}
	result, err := guardrail.Evaluate(policy.Budget, containers)
	if err != nil {
		response.Status.Message = fmt.Sprintf("evaluate pod resource budget: %v", err)
		return response
	}
	response.Allowed = result.Allowed
	response.Status.Message = result.Message
	return response
}

func writeReview(writer http.ResponseWriter, uid string, response admissionResponse) {
	response.UID = uid
	review := admissionReview{
		APIVersion: "admission.k8s.io/v1",
		Kind:       "AdmissionReview",
		Response:   &response,
	}
	writer.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(writer).Encode(review); err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
	}
}
