# WOOP Pod resource guardrail

Admission webhook preventing CAST AI Workload Autoscaler Recommendations from exceeding aggregate Pod CPU or memory budgets.

## Prerequisites

- CAST AI Workload Autoscaler installed
- Helm 3
- cert-manager installed (default TLS option)

## Contract

Opt in by annotating a WOOP-managed Deployment, StatefulSet, ReplicaSet, DaemonSet, or Argo Rollout:

```yaml
metadata:
  annotations:
    guardrail.woop.cast.ai/max-pod-cpu: "15"
    guardrail.woop.cast.ai/max-pod-memory: "60Gi"
```

Recommendations within both budgets pass. If either total exceeds its budget, Kubernetes rejects the Recommendation CREATE/UPDATE. Workloads without either annotation pass unchanged.

## Flow

```text
WOOP Recommendation CREATE/UPDATE
              ↓
Webhook reads target workload budget
              ↓
Sum all recommended container requests
              ↓
CPU and memory within budget?
        yes /       \ no
           ↓         ↓
         allow      deny
```

## Install

```bash
helm upgrade --install guardrail \
  oci://ghcr.io/ronakforcast/charts/woop-pod-resource-guardrail \
  --version 0.1.0 \
  --namespace woop-guardrail-system \
  --create-namespace
```

Annotate each protected workload:

```yaml
metadata:
  annotations:
    guardrail.woop.cast.ai/max-pod-cpu: "15"
    guardrail.woop.cast.ai/max-pod-memory: "60Gi"
```

Verify:

```bash
kubectl get pods -n woop-guardrail-system
kubectl get validatingwebhookconfiguration | grep guardrail
```

Without cert-manager, provide an existing TLS Secret and base64 CA bundle:

```bash
helm upgrade --install guardrail \
  oci://ghcr.io/ronakforcast/charts/woop-pod-resource-guardrail \
  --version 0.1.0 \
  --namespace woop-guardrail-system \
  --create-namespace \
  --set webhook.certManager.enabled=false \
  --set webhook.existingTLSSecret=guardrail-tls \
  --set webhook.caBundle=BASE64_CA
```

Uninstall:

```bash
helm uninstall guardrail --namespace woop-guardrail-system
```

## Build and test

```bash
make test
make vet
make build
docker build -t woop-pod-resource-guardrail:dev .
```

## Safety

- `failurePolicy: Fail` by default: unavailable webhook blocks Recommendation writes.
- Invalid budgets or Kubernetes lookup failures deny the Recommendation.
- Webhook never mutates CAST Recommendations or workloads.
- Disabling WOOP after rejection remains an operator action.
- Test CAST retry and cleanup behavior before production enforcement.

## Integration fixtures

With chart installed, create zero-replica target workload and exercise live webhook without persisting Recommendations:

```bash
kubectl apply -f examples/integration-workload.yaml
kubectl apply --dry-run=server -f examples/recommendation-safe.yaml
kubectl apply --dry-run=server -f examples/recommendation-unsafe.yaml
kubectl apply --dry-run=server -f examples/recommendation-unsafe-memory.yaml
```

Expected: safe fixture accepted; CPU and memory overflow fixtures denied with calculated totals.
