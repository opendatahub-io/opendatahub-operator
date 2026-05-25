# Failure Scenarios

Test scenarios for validating the deterministic classifier (`classify_failure`) and the diagnostic agent against known failure patterns. Each scenario injects a specific failure into a live ODH cluster, runs diagnostic tools, and compares results to ground truth.

## Prerequisites

| Requirement | Details |
|---|---|
| Environment safety | **Use a dedicated non-production cluster only**. These scenarios intentionally induce failures and can cause service disruption. |
| Cluster | OpenShift/Kubernetes with `KUBECONFIG` set |
| ODH operator | Installed and running |
| CRs | DSCI (`default-dsci`) + DSC (`default-dsc`) created, at least one component `Managed` |
| Tools | `oc`, `jq` installed |
| Binary | Built via `make mcp-server` |

## Scenario Index

| Scenario | What it simulates | Expected Category | Expected Code | Confidence |
|----------|-------------------|-------------------|---------------|------------|
| cascading-failure | Deny-all NetworkPolicy blocking all traffic in apps namespace | infrastructure/pod-startup | 1002 | medium |
| component-misconfiguration | Dashboard deployment references nonexistent secret | infrastructure/pod-startup | 1002 | medium |
| dependency-chain | JobSetOperator CR set to Available=False, causing Trainer to degrade | infrastructure/dsci-unhealthy | 1009 | medium |
| image-pull-failure | Dashboard deployment patched to use nonexistent image | infrastructure/image-pull | 1001 | medium |
| independent-failures | Two unrelated component operators scaled to 0 | infrastructure/cluster-distress | 1099 | low |
| missing-dependency | cert-manager operator scaled to 0 | unknown/unclassifiable | 3000 | low |
| node-pressure | Worker node patched with MemoryPressure=True | infrastructure/node-pressure | 1005 | high |
| operator-crash | ODH operator deployment scaled to 0 | infrastructure/operator | 1008 | high |
| partial-failure | One component operator scaled to 0, others healthy | infrastructure/dsc-unhealthy | 1010 | medium |
| resource-exhaustion | Impossibly low ResourceQuota applied to apps namespace | infrastructure/quota-oom | 1004 | high |

## File Structure

Each scenario consists of three files:

```text
<scenario-name>-setup.sh          # Injects the failure
<scenario-name>-teardown.sh       # Restores the cluster
<scenario-name>-ground-truth.json # Expected classifier output
```

Some scenarios include supplementary manifests:

- `cascading-failure-networkpolicy.yaml` — deny-all NetworkPolicy
- `resource-exhaustion-quota.yaml` — restrictive ResourceQuota

Shared helpers are in `common.sh` (namespace discovery, backup/restore, MCP tool invocation, wait helpers).

## Running a Scenario

```bash
# From the repo root:

# 1. Build the binary
make mcp-server

# 2. Source shared helpers
source cmd/mcp-server/scenarios/common.sh

# 3. Run setup (injects the failure)
bash cmd/mcp-server/scenarios/operator-crash-setup.sh

# 4. Call classify_failure and compare to ground truth
call_mcp_tool classify_failure '{}' | jq .
cat cmd/mcp-server/scenarios/operator-crash-ground-truth.json | jq .expected_classification

# 5. Teardown (restores the cluster)
bash cmd/mcp-server/scenarios/operator-crash-teardown.sh
```

## Ground Truth Schema

Each `*-ground-truth.json` file follows this structure:

```json
{
  "scenario_id": "operator-crash",
  "scenario_name": "Operator Crash",
  "description": "What the scenario does",
  "root_cause": "The actual root cause",
  "expected_classification": {
    "category": "infrastructure",
    "subcategory": "operator",
    "error_code": 1008,
    "confidence": "high"
  },
  "expected_tool_outputs": { },
  "expected_symptoms": ["symptom 1", "symptom 2"],
  "tools_that_detect": ["classify_failure", "platform_health"],
  "expected_remediation": "How to fix it"
}
```

## Adding a New Scenario

1. Create `<name>-setup.sh` — source `common.sh`, use `backup_resource()` to save state, inject the failure, use `wait_for_pod_status()` to confirm the failure is active
2. Create `<name>-teardown.sh` — source `common.sh`, restore backups, wait for recovery
3. Create `<name>-ground-truth.json` — fill in the expected classification using the [error code reference](../prompts/diagnostic.md#error-code-reference)
4. Test: run setup, call `classify_failure`, compare output to ground truth, run teardown
