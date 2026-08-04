"""Shared helpers for statistical analysis in eval reports."""

import json
from functools import lru_cache
from pathlib import Path

import yaml

try:
    from scipy.stats import wilcoxon as scipy_wilcoxon
    from scipy.stats import chi2 as scipy_chi2
    HAS_SCIPY = True
except ImportError:
    HAS_SCIPY = False

# Metric definitions used across multiple sections
_BINARY_METRICS = [
    ("root_cause_correct", "Root cause"),
    ("remediation_actionable", "Remediation"),
    ("false_positive", "No false positives"),
]
_CONTINUOUS_METRICS = [
    ("evidence_citation", "Evidence citation"),
    ("completeness", "Completeness"),
    ("hallucination", "Hallucination"),
]
_VERDICT_KEYS = [
    ("root_cause_pass_rate", "Root cause"),
    ("remediation_pass_rate", "Remediation"),
    ("false_positive_pass_rate", "No false positives"),
    ("avg_evidence_citation", "Evidence citation"),
    ("avg_completeness", "Completeness"),
    ("avg_hallucination", "Hallucination"),
]


def mean(vals):
    return sum(vals) / len(vals) if vals else None


def paired_scores(scored_results, metric, cfg_a, cfg_b):
    """Extract paired score lists for two configs across scenarios."""
    a_vals, b_vals = [], []
    for s in scored_results["scenarios"]:
        va = s["configs"].get(cfg_a, {}).get(metric)
        vb = s["configs"].get(cfg_b, {}).get(metric)
        if va is not None and vb is not None:
            a_vals.append(float(va) if not isinstance(va, bool) else int(va))
            b_vals.append(float(vb) if not isinstance(vb, bool) else int(vb))
    return a_vals, b_vals


def wilcoxon_test(a_vals, b_vals):
    """Wilcoxon signed-rank for continuous metrics. Returns (stat, p)."""
    if not HAS_SCIPY or len(a_vals) < 5:
        return None, None
    diffs = [b - a for a, b in zip(a_vals, b_vals)]
    if all(d == 0 for d in diffs):
        return 0.0, 1.0
    try:
        stat, p = scipy_wilcoxon(diffs)
        return stat, p
    except Exception:
        return None, None


def mcnemar_test(a_vals, b_vals):
    """McNemar's test for binary metrics. Returns (stat, p)."""
    if not HAS_SCIPY or len(a_vals) < 5:
        return None, None
    b_only = sum(1 for a, b in zip(a_vals, b_vals) if not a and b)
    a_only = sum(1 for a, b in zip(a_vals, b_vals) if a and not b)
    n = b_only + a_only
    if n == 0:
        return 0.0, 1.0
    stat = (abs(b_only - a_only) - 1) ** 2 / n
    p = 1.0 - scipy_chi2.cdf(stat, df=1)
    return stat, p


@lru_cache(maxsize=128)
def load_annotations(dataset_dir, scenario_id):
    """Load annotations.yaml for a scenario (cached to avoid re-reading)."""
    ann_file = Path(dataset_dir) / scenario_id / "annotations.yaml"
    if not ann_file.exists():
        return {}
    try:
        with open(ann_file) as f:
            return yaml.safe_load(f) or {}
    except Exception:
        return {}


def load_scored(results_dir):
    """Load scored-results.json from a results directory."""
    scored_file = Path(results_dir) / "scored-results.json"
    if not scored_file.exists():
        return {}
    try:
        with open(scored_file) as f:
            return json.load(f)
    except Exception:
        return {}


def _pass_rate(scenarios, config, metric):
    """Compute pass rate for a binary metric across scenarios."""
    vals = [s["configs"].get(config, {}).get(metric) for s in scenarios]
    vals = [v for v in vals if v is not None]
    if not vals:
        return "N/A"
    return "%.0f%%" % (sum(1 for v in vals if v) / len(vals) * 100)


def _md_table_row(label, cells):
    return "| %s | %s |" % (label, " | ".join(cells))


# ---------------------------------------------------------------------------
# Report analysis sections — appended to eval-report.md by score-results.py
# ---------------------------------------------------------------------------

def append_statistical(lines, scored_results, config_names):
    if "config-a" not in config_names or "config-b" not in config_names:
        return
    lines.append("## Statistical Comparison: Config A vs B\n")
    if not HAS_SCIPY:
        lines.append("*scipy not installed — install for statistical tests.*\n")
        return

    lines.append("| Metric | Test | Statistic | p-value | Significant (p<0.05) |")
    lines.append("|--------|------|-----------|---------|----------------------|")

    tests = [(m, l, mcnemar_test, "McNemar") for m, l in _BINARY_METRICS] + \
            [(m, l, wilcoxon_test, "Wilcoxon") for m, l in _CONTINUOUS_METRICS]

    for metric, label, test_fn, test_name in tests:
        a, b = paired_scores(scored_results, metric, "config-a", "config-b")
        stat, p = test_fn(a, b)
        if p is not None:
            sig = "Yes" if p < 0.05 else "No"
            lines.append("| %s | %s | %.2f | %.4f | %s |" % (label, test_name, stat, p, sig))
        else:
            lines.append("| %s | %s | — | — | insufficient data |" % (label, test_name))
    lines.append("")


def append_categories(lines, scored_results, dataset_dir):
    categories = {}
    for s in scored_results["scenarios"]:
        ann = load_annotations(dataset_dir, s["scenario_id"])
        cat = ann.get("expected_classification", {})
        if isinstance(cat, dict):
            cat = cat.get("category", "unknown")
        if cat:
            categories.setdefault(cat, []).append(s)

    if not categories:
        return

    cfgs = sorted({c for s in scored_results["scenarios"] for c in s["configs"]})
    lines.append("## Category Breakdown\n")

    for cat, scenarios in sorted(categories.items()):
        lines.append("### %s (%d scenarios)\n" % (cat, len(scenarios)))
        lines.append("| Metric | %s |" % " | ".join(cfgs))
        lines.append("|--------|%s|" % "|".join(["--------"] * len(cfgs)))
        for metric, label in _BINARY_METRICS[:2]:
            lines.append(_md_table_row(label, [_pass_rate(scenarios, c, metric) for c in cfgs]))
        lines.append("")


def append_agent_vs_classifier(lines, scored_results, dataset_dir):
    """Compare Config B (agent) vs Config C (classifier) across all metrics."""
    _compare_metrics = [
        ("root_cause_correct", "Root cause", "bool"),
        ("remediation_actionable", "Remediation", "bool"),
        ("false_positive", "No false pos", "bool"),
        ("evidence_citation", "Evidence", "float"),
        ("completeness", "Completeness", "float"),
    ]
    rows, b_wins, c_wins, ties = [], 0, 0, 0

    for s in scored_results["scenarios"]:
        b = s["configs"].get("config-b", {})
        c = s["configs"].get("config-c", {})
        if not b or not c:
            continue

        b_score, c_score = 0, 0
        for key, _, kind in _compare_metrics:
            bv, cv = b.get(key), c.get(key)
            if bv is None or cv is None:
                continue
            if kind == "bool":
                b_score += int(bool(bv))
                c_score += int(bool(cv))
            else:
                b_score += float(bv)
                c_score += float(cv)

        if b_score > c_score:
            result, b_wins = "B wins", b_wins + 1
        elif c_score > b_score:
            result, c_wins = "C wins", c_wins + 1
        else:
            result, ties = "Tie", ties + 1

        rows.append((s["scenario_id"], round(b_score, 1), round(c_score, 1), result))

    if not rows:
        return

    lines.append("## Agent vs Deterministic Classifier (Config B vs C)\n")
    lines.append("| Scenario | Agent score | Classifier score | Winner |")
    lines.append("|----------|------------|-----------------|--------|")
    for sid, bs, cs, result in rows:
        lines.append("| %s | %.1f | %.1f | %s |" % (sid, bs, cs, result))
    lines.append("")
    lines.append("**Summary**: Agent wins %d, Classifier wins %d, Ties %d (out of %d)\n" %
                 (b_wins, c_wins, ties, b_wins + c_wins + ties))


def append_consistency(lines, scored_results, results_dir):
    consistency = scored_results.get("consistency", {})
    if not consistency:
        return

    run_count = len(sorted(Path(results_dir).glob("config-b-run*")))

    lines.append("## Consistency Analysis (Config B — %d runs)\n" % run_count)
    lines.append("| Scenario | Root cause agreement | Results |")
    lines.append("|----------|---------------------|---------|")

    total_agree = 0
    for sid, results in sorted(consistency.items()):
        agree = len(set(results)) == 1
        total_agree += agree
        labels = "/".join("PASS" if v else "FAIL" for v in results)
        lines.append("| %s | %s | %s |" % (sid, "Yes" if agree else "No", labels))

    total = len(consistency)
    lines.append("")
    lines.append("**Overall agreement**: %.0f%% (%d/%d scenarios)\n" %
                 (total_agree / total * 100 if total else 0, total_agree, total))


def append_verdict(lines, scored_results):
    summary = scored_results.get("summary", {})
    a, b = summary.get("config-a", {}), summary.get("config-b", {})
    if not a or not b:
        return

    lines.append("## Verdict\n")
    wins, losses, ties = 0, 0, 0
    comparisons = []

    for key, label in _VERDICT_KEYS:
        va, vb = a.get(key), b.get(key)
        if va is None or vb is None:
            continue
        diff = vb - va
        if abs(diff) < 0.01:
            ties += 1
            comparisons.append("- **%s**: tied (%.2f vs %.2f)" % (label, va, vb))
        elif diff > 0:
            wins += 1
            comparisons.append("- **%s**: Config B wins (+%.2f)" % (label, diff))
        else:
            losses += 1
            comparisons.append("- **%s**: Config A wins (+%.2f)" % (label, -diff))

    if wins > losses:
        verdict = "**Yes** — system prompt adds measurable value (%d wins, %d losses, %d ties)." % (wins, losses, ties)
    elif losses > wins:
        verdict = "**No** — raw LLM outperforms on more metrics (%d losses, %d wins, %d ties)." % (losses, wins, ties)
    else:
        verdict = "**Inconclusive** — results are split (%d wins, %d losses, %d ties)." % (wins, losses, ties)

    lines.append("Does the system prompt (Config B) add value over raw LLM (Config A)?\n")
    lines.append(verdict)
    lines.append("")
    lines.append("\n".join(comparisons))
    lines.append("")
