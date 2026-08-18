package guardrail

import "testing"

func TestEvaluateAllowsRecommendationWithinBothBudgets(t *testing.T) {
	result, err := Evaluate(
		Budget{CPU: "15", Memory: "60Gi"},
		[]ContainerRecommendation{
			{Name: "main", CPU: "8", Memory: "40Gi"},
			{Name: "sidecar", CPU: "1500m", Memory: "2Gi"},
		},
	)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if !result.Allowed {
		t.Fatalf("expected recommendation to be allowed, got: %s", result.Message)
	}
	if result.TotalCPU != "9500m" || result.TotalMemory != "42Gi" {
		t.Fatalf("unexpected totals: CPU=%s memory=%s", result.TotalCPU, result.TotalMemory)
	}
}

func TestEvaluateRejectsWhenAggregateCPUExceedsBudget(t *testing.T) {
	result, err := Evaluate(
		Budget{CPU: "15", Memory: "60Gi"},
		[]ContainerRecommendation{
			{Name: "main", CPU: "10", Memory: "20Gi"},
			{Name: "worker", CPU: "4", Memory: "20Gi"},
			{Name: "sidecar", CPU: "2", Memory: "2Gi"},
		},
	)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if result.Allowed {
		t.Fatal("expected recommendation to be rejected")
	}
	if result.TotalCPU != "16" || result.Message != "aggregate CPU request 16 exceeds pod budget 15" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestEvaluateRejectsWhenAggregateMemoryExceedsBudget(t *testing.T) {
	result, err := Evaluate(
		Budget{CPU: "15", Memory: "60Gi"},
		[]ContainerRecommendation{
			{Name: "main", CPU: "8", Memory: "59Gi"},
			{Name: "sidecar", CPU: "1", Memory: "2048Mi"},
		},
	)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if result.Allowed {
		t.Fatal("expected recommendation to be rejected")
	}
	if result.TotalMemory != "61Gi" || result.Message != "aggregate memory request 61Gi exceeds pod budget 60Gi" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestEvaluateRejectsInvalidQuantities(t *testing.T) {
	_, err := Evaluate(Budget{CPU: "15"}, []ContainerRecommendation{{Name: "main", CPU: "many"}})
	if err == nil {
		t.Fatal("expected invalid quantity error")
	}
}

func TestEvaluateAllowsWhenNoBudgetConfigured(t *testing.T) {
	result, err := Evaluate(Budget{}, []ContainerRecommendation{{Name: "main", CPU: "200"}})
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if !result.Allowed {
		t.Fatalf("expected no-budget recommendation to be allowed: %s", result.Message)
	}
}

func TestEvaluateRejectsNegativeQuantities(t *testing.T) {
	tests := []struct {
		name       string
		budget     Budget
		containers []ContainerRecommendation
	}{
		{"CPU request", Budget{CPU: "15"}, []ContainerRecommendation{{Name: "bad", CPU: "-1"}}},
		{"memory request", Budget{Memory: "1Gi"}, []ContainerRecommendation{{Name: "bad", Memory: "-1Mi"}}},
		{"CPU budget", Budget{CPU: "-1"}, nil},
		{"memory budget", Budget{Memory: "-1Gi"}, nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Evaluate(test.budget, test.containers); err == nil {
				t.Fatal("expected negative quantity error")
			}
		})
	}
}
