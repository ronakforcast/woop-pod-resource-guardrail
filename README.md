# WOOP Pod resource guardrail

Admission webhook preventing CAST AI Workload Autoscaler (WOOP) Recommendations from exceeding aggregate Pod CPU or memory budgets.

## What it does

When WOOP creates or updates a `Recommendation` for a protected workload, this guardrail:

1. Reads the CPU and memory budgets from the target workload annotations.
2. Validates that the Recommendation covers exactly the containers in the workload.
3. Sums the recommended container requests.
4. Allows the Recommendation if both totals are within budget.
5. Denies the Recommendation and disables WOOP for the workload if either total exceeds its budget.

## Prerequisites

- CAST AI Workload Autoscaler installed
- Helm 3
- cert-manager installed (default TLS option)

## Opt-in contract

Protect a workload by adding annotations to its `Deployment`, `StatefulSet`, `ReplicaSet`, `DaemonSet`, or Argo `Rollout`:

```yaml
metadata:
  annotations:
    guardrail.woop.cast.ai/max-pod-cpu: "15"
    guardrail.woop.cast.ai/max-pod-memory: "60Gi"
```

- Budgets are **per-pod aggregate** limits, not per-container limits.
- CPU and memory quantities follow Kubernetes resource quantity syntax (`100m`, `1.5`, `60Gi`, `128Mi`).
- A workload with **neither** annotation passes through unchanged.
- Annotations can be added or removed at any time.

## Flow diagrams

### High-level decision flow

```text
                    WOOP Recommendation CREATE/UPDATE
                                │
                                ▼
         ┌─────────────────────────────────────────────────────┐
         │  Webhook reads target workload budget annotations   │
         └─────────────────────────────────────────────────────┘
                                │
                                ▼
         ┌─────────────────────────────────────────────────────┐
         │  Validate container list matches target workload    │
         └─────────────────────────────────────────────────────┘
                                │
                mismatch / empty ──────► deny
                                │
                                ▼
         ┌─────────────────────────────────────────────────────┐
         │  Sum all recommended container requests             │
         └─────────────────────────────────────────────────────┘
                                │
                                ▼
                    within both budgets?
                    yes /           \
                   /                  \
                  ▼                    ▼
              allow              deny + enqueue
              Recommendation   disable job
                                │
                                ▼
                    controller patches workload:
                    vertical.optimization: off
```

### Safe recommendation path

```text
WOUP creates/updates Recommendation
              │
              ▼
Workload has guardrail budget annotations
              │
              ▼
Container list matches workload spec
              │
              ▼
Aggregate CPU  ≤ max-pod-cpu
Aggregate memory ≤ max-pod-memory
              │
              ▼
        ✅ Recommendation allowed
        WOOP continues optimizing the workload
```

### Unsafe recommendation path

```text
WOUP creates/updates Recommendation
              │
              ▼
Workload has guardrail budget annotations
              │
              ▼
Container list matches workload spec
              │
              ▼
Aggregate CPU  > max-pod-cpu
        OR
Aggregate memory > max-pod-memory
              │
              ▼
        ❌ Recommendation denied
        Error: "aggregate CPU request X exceeds pod budget Y; WOOP disable queued"
              │
              ▼
        Disable job written to guardrail namespace (ConfigMap)
              │
              ▼
        Remediation controller picks up job (retries on conflict)
              │
              ▼
        Workload annotated:
        workloads.cast.ai/configuration.vertical.optimization = "off"
              │
              ▼
        Job deleted after successful patch
```

### Server-side dry-run path

```text
kubectl apply --dry-run=server -f recommendation-unsafe.yaml
              │
              ▼
        Webhook evaluates budget
              │
              ▼
        ❌ Request denied with budget message
              │
              ▼
        ⚠️  No disable job enqueued
        ⚠️  Workload is NOT patched
        (safe to use in CI / validation pipelines)
```

## Expected behavior

| Scenario | Webhook verdict | Workload patched | Disable job queued | Recommendation persisted |
|---|---|---|---|---|
| Workload has no budget annotations | Allowed | No | No | Yes |
| Safe recommendation within budgets | Allowed | No | No | Yes |
| Unsafe recommendation (real apply) | Denied | Yes (`optimization: off`) | Yes | No |
| Unsafe recommendation (server dry-run) | Denied | No | No | No |
| Missing/duplicate/unknown container in Recommendation | Denied | No | No | No |
| Invalid budget annotation value | Denied | No | No | No |
| Kubernetes API lookup failure | Denied | No | No | No |

## Error messages you will see

| Situation | Message |
|---|---|
| CPU overflow | `aggregate CPU request <total> exceeds pod budget <budget>; WOOP disable queued` |
| Memory overflow | `aggregate memory request <total> exceeds pod budget <budget>; WOOP disable queued` |
| Dry-run CPU overflow | `aggregate CPU request <total> exceeds pod budget <budget>; dry-run: WOOP disable not queued` |
| Missing container | `Recommendation is missing container "<name>"` |
| Unknown/duplicate container | `Recommendation has unknown or duplicate container "<name>"` |
| Missing budgeted CPU request | `Recommendation container "<name>" is missing CPU request` |
| Missing budgeted memory request | `Recommendation container "<name>" is missing memory request` |
| Invalid budget quantity | `CPU budget: ...` / `memory budget: ...` |
| Negative quantity | `container "<name>" CPU request must not be negative` |

## Install

```bash
helm upgrade --install guardrail \
  oci://ghcr.io/ronakforcast/charts/woop-pod-resource-guardrail \
  --version 0.2.0 \
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
  --version 0.2.0 \
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
- Webhook never mutates or deletes CAST Recommendations.
- An unsafe Recommendation is denied and the target workload is patched to set `vertical.optimization: off` inside `workloads.cast.ai/configuration`.
- Existing WOOP configuration fields are preserved during that patch.
- Disable jobs are ConfigMaps in the guardrail namespace. They remain queued and retry until the workload patch succeeds.
- Server-side dry-run rejects unsafe input but does not enqueue a disable job or mutate the workload.
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

## Performance and timing

Observed in a local k3d cluster with 10-replica workloads:

- Webhook evaluation latency: **sub-second** for a single Recommendation.
- Remediation latency: **< 2 seconds** from denial to workload patch in normal conditions.
- The controller polls for disable jobs every **2 seconds**.

Production latency depends on API server load, etcd latency, and webhook replica count.

## Troubleshooting

### Workload is not being protected

- Confirm the workload kind is supported: `Deployment`, `StatefulSet`, `ReplicaSet`, `DaemonSet`, or Argo `Rollout`.
- Confirm annotations are on the **workload** metadata, not the pod template metadata.
- Confirm at least one of `guardrail.woop.cast.ai/max-pod-cpu` or `guardrail.woop.cast.ai/max-pod-memory` is set.
- Check the webhook is registered: `kubectl get validatingwebhookconfiguration`.
- Check guardrail pods are running: `kubectl get pods -n woop-guardrail-system`.

### Unsafe Recommendation is not being denied

- Confirm the Recommendation `apiVersion` is `autoscaling.cast.ai/v1` and the webhook rule matches it.
- Check webhook logs for decode or lookup errors.
- Verify the `failurePolicy` is `Fail` in the `ValidatingWebhookConfiguration`.

### Workload was disabled but I want to re-enable WOOP

- Remove `vertical.optimization: off` from the `workloads.cast.ai/configuration` annotation, or set it back to `on`.
- Ensure the next Recommendation is within budget; otherwise the guardrail will disable it again.

### Dry-run passes but real apply fails

This is expected behavior. Dry-run does not enqueue the disable job or patch the workload, but it still evaluates the budget and returns a denial if the Recommendation is unsafe.

## License

See [LICENSE](LICENSE).
