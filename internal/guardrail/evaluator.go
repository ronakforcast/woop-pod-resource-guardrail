package guardrail

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/resource"
)

type Budget struct {
	CPU    string
	Memory string
}

type ContainerRecommendation struct {
	Name   string
	CPU    string
	Memory string
}

type Result struct {
	Allowed     bool
	Message     string
	TotalCPU    string
	TotalMemory string
}

func Evaluate(budget Budget, containers []ContainerRecommendation) (Result, error) {
	totalCPU := resource.MustParse("0")
	totalMemory := resource.MustParse("0")

	for _, container := range containers {
		if container.CPU != "" {
			quantity, err := resource.ParseQuantity(container.CPU)
			if err != nil {
				return Result{}, fmt.Errorf("container %q CPU request: %w", container.Name, err)
			}
			totalCPU.Add(quantity)
		}
		if container.Memory != "" {
			quantity, err := resource.ParseQuantity(container.Memory)
			if err != nil {
				return Result{}, fmt.Errorf("container %q memory request: %w", container.Name, err)
			}
			totalMemory.Add(quantity)
		}
	}

	result := Result{
		Allowed:     true,
		TotalCPU:    totalCPU.String(),
		TotalMemory: totalMemory.String(),
	}
	if budget.CPU != "" {
		maxCPU, err := resource.ParseQuantity(budget.CPU)
		if err != nil {
			return Result{}, fmt.Errorf("CPU budget: %w", err)
		}
		if totalCPU.Cmp(maxCPU) > 0 {
			result.Allowed = false
			result.Message = fmt.Sprintf("aggregate CPU request %s exceeds pod budget %s", totalCPU.String(), maxCPU.String())
			return result, nil
		}
	}
	if budget.Memory != "" {
		maxMemory, err := resource.ParseQuantity(budget.Memory)
		if err != nil {
			return Result{}, fmt.Errorf("memory budget: %w", err)
		}
		if totalMemory.Cmp(maxMemory) > 0 {
			result.Allowed = false
			result.Message = fmt.Sprintf("aggregate memory request %s exceeds pod budget %s", totalMemory.String(), maxMemory.String())
			return result, nil
		}
	}
	return result, nil
}
