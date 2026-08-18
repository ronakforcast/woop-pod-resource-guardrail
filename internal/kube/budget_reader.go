package kube

import (
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

	"github.com/castai-labs/woop-pod-resource-guardrail/internal/guardrail"
	"github.com/castai-labs/woop-pod-resource-guardrail/internal/webhook"
)

const (
	CPUBudgetAnnotation    = "guardrail.woop.cast.ai/max-pod-cpu"
	MemoryBudgetAnnotation = "guardrail.woop.cast.ai/max-pod-memory"
	serviceAccountToken    = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	serviceAccountCA       = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
)

type BudgetReader struct {
	baseURL string
	token   string
	client  *http.Client
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

func (reader *BudgetReader) BudgetFor(ctx context.Context, target webhook.TargetRef, namespace string) (guardrail.Budget, error) {
	resourcePath, err := targetPath(target, namespace)
	if err != nil {
		return guardrail.Budget{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, reader.baseURL+resourcePath, nil)
	if err != nil {
		return guardrail.Budget{}, fmt.Errorf("create Kubernetes API request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+reader.token)
	response, err := reader.client.Do(request)
	if err != nil {
		return guardrail.Budget{}, fmt.Errorf("get target workload: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return guardrail.Budget{}, fmt.Errorf("get target workload: HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	var workload struct {
		Metadata struct {
			Annotations map[string]string `json:"annotations"`
		} `json:"metadata"`
	}
	if err := json.NewDecoder(response.Body).Decode(&workload); err != nil {
		return guardrail.Budget{}, fmt.Errorf("decode target workload: %w", err)
	}
	return guardrail.Budget{
		CPU:    workload.Metadata.Annotations[CPUBudgetAnnotation],
		Memory: workload.Metadata.Annotations[MemoryBudgetAnnotation],
	}, nil
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
