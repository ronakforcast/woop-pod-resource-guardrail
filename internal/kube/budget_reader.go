package kube

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/ronakforcast/woop-pod-resource-guardrail/internal/guardrail"
	"github.com/ronakforcast/woop-pod-resource-guardrail/internal/webhook"
	"sigs.k8s.io/yaml"
)

const (
	CPUBudgetAnnotation         = "guardrail.woop.cast.ai/max-pod-cpu"
	MemoryBudgetAnnotation      = "guardrail.woop.cast.ai/max-pod-memory"
	WOOPConfigurationAnnotation = "workloads.cast.ai/configuration"
	serviceAccountToken         = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	serviceAccountCA            = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
)

type BudgetReader struct {
	baseURL string
	token   string
	client  *http.Client
}

func (reader *BudgetReader) DisableOptimization(ctx context.Context, target webhook.TargetRef, namespace string) error {
	resourcePath, err := targetPath(target, namespace)
	if err != nil {
		return err
	}
	getRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, reader.baseURL+resourcePath, nil)
	if err != nil {
		return fmt.Errorf("create workload GET request: %w", err)
	}
	getRequest.Header.Set("Authorization", "Bearer "+reader.token)
	getResponse, err := reader.client.Do(getRequest)
	if err != nil {
		return fmt.Errorf("get workload configuration: %w", err)
	}
	defer getResponse.Body.Close()
	if getResponse.StatusCode != http.StatusOK {
		return fmt.Errorf("get workload configuration: HTTP %d", getResponse.StatusCode)
	}
	var workload struct {
		Metadata struct {
			Annotations map[string]string `json:"annotations"`
		} `json:"metadata"`
	}
	if err := json.NewDecoder(getResponse.Body).Decode(&workload); err != nil {
		return fmt.Errorf("decode workload configuration: %w", err)
	}
	configuration := map[string]any{}
	if raw := workload.Metadata.Annotations[WOOPConfigurationAnnotation]; raw != "" {
		if err := yaml.Unmarshal([]byte(raw), &configuration); err != nil {
			return fmt.Errorf("parse WOOP configuration: %w", err)
		}
	}
	vertical, ok := configuration["vertical"].(map[string]any)
	if !ok {
		vertical = map[string]any{}
		configuration["vertical"] = vertical
	}
	vertical["optimization"] = "off"
	encodedConfiguration, err := yaml.Marshal(configuration)
	if err != nil {
		return fmt.Errorf("encode WOOP configuration: %w", err)
	}
	patchBody, err := json.Marshal(map[string]any{"metadata": map[string]any{"annotations": map[string]string{
		WOOPConfigurationAnnotation: string(encodedConfiguration),
	}}})
	if err != nil {
		return fmt.Errorf("encode workload patch: %w", err)
	}
	patchRequest, err := http.NewRequestWithContext(ctx, http.MethodPatch, reader.baseURL+resourcePath, bytes.NewReader(patchBody))
	if err != nil {
		return fmt.Errorf("create workload PATCH request: %w", err)
	}
	patchRequest.Header.Set("Authorization", "Bearer "+reader.token)
	patchRequest.Header.Set("Content-Type", "application/merge-patch+json")
	patchResponse, err := reader.client.Do(patchRequest)
	if err != nil {
		return fmt.Errorf("patch workload configuration: %w", err)
	}
	defer patchResponse.Body.Close()
	if patchResponse.StatusCode < 200 || patchResponse.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(patchResponse.Body, 4096))
		return fmt.Errorf("patch workload configuration: HTTP %d: %s", patchResponse.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func NewInClusterBudgetReader() (*BudgetReader, error) {
	host := os.Getenv("KUBERNETES_SERVICE_HOST")
	port := os.Getenv("KUBERNETES_SERVICE_PORT_HTTPS")
	if port == "" {
		port = "443"
	}
	if host == "" {
		return nil, fmt.Errorf("KUBERNETES_SERVICE_HOST is not set")
	}
	token, err := os.ReadFile(serviceAccountToken)
	if err != nil {
		return nil, fmt.Errorf("read service account token: %w", err)
	}
	ca, err := os.ReadFile(serviceAccountCA)
	if err != nil {
		return nil, fmt.Errorf("read service account CA: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, fmt.Errorf("parse service account CA")
	}
	return &BudgetReader{
		baseURL: "https://" + host + ":" + port,
		token:   strings.TrimSpace(string(token)),
		client: &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			RootCAs:    roots,
		}}},
	}, nil
}

func (reader *BudgetReader) PolicyFor(ctx context.Context, target webhook.TargetRef, namespace string) (webhook.Policy, error) {
	resourcePath, err := targetPath(target, namespace)
	if err != nil {
		return webhook.Policy{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, reader.baseURL+resourcePath, nil)
	if err != nil {
		return webhook.Policy{}, fmt.Errorf("create Kubernetes API request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+reader.token)
	response, err := reader.client.Do(request)
	if err != nil {
		return webhook.Policy{}, fmt.Errorf("get target workload: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return webhook.Policy{}, fmt.Errorf("get target workload: HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	var workload struct {
		Metadata struct {
			Annotations map[string]string `json:"annotations"`
		} `json:"metadata"`
		Spec struct {
			Template struct {
				Spec struct {
					Containers []struct {
						Name string `json:"name"`
					} `json:"containers"`
					InitContainers []struct {
						Name          string `json:"name"`
						RestartPolicy string `json:"restartPolicy"`
					} `json:"initContainers"`
				} `json:"spec"`
			} `json:"template"`
		} `json:"spec"`
	}
	if err := json.NewDecoder(response.Body).Decode(&workload); err != nil {
		return webhook.Policy{}, fmt.Errorf("decode target workload: %w", err)
	}
	containers := make([]string, 0, len(workload.Spec.Template.Spec.Containers))
	for _, container := range workload.Spec.Template.Spec.Containers {
		containers = append(containers, container.Name)
	}
	for _, container := range workload.Spec.Template.Spec.InitContainers {
		if container.RestartPolicy == "Always" {
			containers = append(containers, container.Name)
		}
	}
	return webhook.Policy{Budget: guardrail.Budget{
		CPU:    workload.Metadata.Annotations[CPUBudgetAnnotation],
		Memory: workload.Metadata.Annotations[MemoryBudgetAnnotation],
	}, Containers: containers}, nil
}

func targetPath(target webhook.TargetRef, namespace string) (string, error) {
	parts := strings.Split(target.APIVersion, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("unsupported target apiVersion %q", target.APIVersion)
	}
	resources := map[string]string{
		"Deployment":  "deployments",
		"StatefulSet": "statefulsets",
		"ReplicaSet":  "replicasets",
		"DaemonSet":   "daemonsets",
		"Rollout":     "rollouts",
	}
	resourceName, ok := resources[target.Kind]
	if !ok {
		return "", fmt.Errorf("unsupported target kind %q", target.Kind)
	}
	return path.Join(
		"/apis", url.PathEscape(parts[0]), url.PathEscape(parts[1]),
		"namespaces", url.PathEscape(namespace), resourceName, url.PathEscape(target.Name),
	), nil
}
