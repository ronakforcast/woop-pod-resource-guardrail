# WOOP Pod Resource Guardrail — v1 spec

- Validate CAST AI `Recommendation` CREATE and UPDATE operations.
- Read aggregate CPU and memory budgets from target workload annotations.
- Sum recommended requests across every application container using Kubernetes quantity semantics.
- Allow recommendations within all configured budgets.
- Reject recommendations exceeding either budget.
- Fail closed for malformed recommendations, invalid budgets, or workload lookup failures.
- Support Deployments, StatefulSets, ReplicaSets, DaemonSets, and Argo Rollouts.
- Do not mutate recommendations or delete existing recommendations.
- When a recommendation exceeds a budget, enqueue a durable disable job; retry conflict-safe updates until the existing WOOP configuration is preserved and target workload `vertical.optimization` is `off`.
- Admission dry-run must reject unsafe recommendations without queuing or mutating anything.
