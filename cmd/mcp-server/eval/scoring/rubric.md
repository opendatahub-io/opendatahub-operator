# Scoring Rubric

## Automated Metrics (per scenario)

| Metric | Type | How scored |
|--------|------|------------|
| Root cause correct | binary | Compares diagnosis against ground-truth root_cause and error_code |
| Remediation actionable | binary | Checks for specific commands (kubectl/oc) vs generic advice |
| False positives | binary | Flags reported failures that don't exist on the cluster |
| Tool calls | count | Number of MCP diagnostic tool invocations |
| Wall-clock time | seconds | Duration from start to finish |

## Blind Scoring (manual)

Each output is anonymized (Output-Alpha, Output-Beta, Output-Gamma).
Score without knowing which config produced it.

| Metric | Score | Criteria |
|--------|-------|----------|
| Root cause correct | 0/1 | Identifies actual root cause from ground truth, not just symptoms |
| Remediation actionable | 0/1 | Fix is specific enough to execute without further research |
| False positives | count | Problems reported that don't actually exist |
| Completeness | 1-5 | 5=full trace with evidence, 1=misses root cause |
| Clarity | 1-5 | 5=structured with headers, 1=unreadable |

## Process

1. Run `anonymize-outputs.py` to create blind outputs
2. Run `generate-blind-sheets.py` to create scoring CSV
3. Score all outputs before checking `mapping.json`
