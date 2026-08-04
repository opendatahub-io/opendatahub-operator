#!/usr/bin/env python3
"""Score eval results using judges and generate a comparison report.

Reads diagnosis.txt from each config/scenario directory, loads ground
truth from dataset/annotations.yaml, runs all 5 judges, and outputs
scored results + a markdown comparison report.

Usage:
  python3 score-results.py --results-dir ../results --dataset-dir ../dataset
"""

import argparse
import csv
import json
import re
import sys
from pathlib import Path
from statistics import median as _median

import yaml

from analysis_helpers import (
    append_statistical, append_categories, append_agent_vs_classifier,
    append_consistency, append_verdict,
)

# Add eval root to sys.path so judges.* is importable
EVAL_DIR = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(EVAL_DIR))

from judges.root_cause_match import judge as judge_root_cause
from judges.remediation_actionable import judge as judge_remediation
from judges.false_positive import judge as judge_false_positive
from judges.quality_scoring import judge_evidence_citation, judge_completeness
from judges.hallucination import judge as judge_hallucination


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--results-dir", required=True)
    parser.add_argument("--dataset-dir", required=True)
    args = parser.parse_args()

    results_dir = Path(args.results_dir)
    dataset_dir = Path(args.dataset_dir)

    if not results_dir.is_dir():
        print("ERROR: Results directory not found: %s" % results_dir,
              file=sys.stderr)
        sys.exit(1)
    if not dataset_dir.is_dir():
        print("ERROR: Dataset directory not found: %s" % dataset_dir,
              file=sys.stderr)
        sys.exit(1)

    config_names = sorted(
        d.name for d in results_dir.iterdir()
        if d.is_dir() and d.name.startswith("config-")
    )
    if not config_names:
        print("ERROR: No config-* directories in %s" % results_dir,
              file=sys.stderr)
        sys.exit(1)

    all_scores = {}
    all_scenarios = set()

    for config_name in config_names:
        config_dir = results_dir / config_name
        config_scores = {}

        for scenario_dir in sorted(config_dir.iterdir()):
            if not scenario_dir.is_dir():
                continue
            scenario_id = scenario_dir.name
            all_scenarios.add(scenario_id)

            diagnosis_file = scenario_dir / "diagnosis.txt"
            if not diagnosis_file.exists():
                print("  SKIP: %s/%s (no diagnosis.txt)" %
                      (config_name, scenario_id), file=sys.stderr)
                continue

            try:
                diagnosis_text = diagnosis_file.read_text()
            except (UnicodeDecodeError, OSError) as e:
                print("  ERROR: Cannot read %s: %s" % (diagnosis_file, e),
                      file=sys.stderr)
                continue

            if len(diagnosis_text.strip()) < 10:
                print("  SKIP: %s/%s (diagnosis.txt too short)" %
                      (config_name, scenario_id), file=sys.stderr)
                continue

            # Load ground truth annotations
            annotations = _load_annotations(dataset_dir, scenario_id)
            if annotations is None:
                print("  SKIP: %s/%s (no annotations.yaml)" %
                      (config_name, scenario_id), file=sys.stderr)
                continue

            # Load tool calls from parse-stream.py output
            tool_calls = _load_tool_calls(scenario_dir)

            # Build outputs dict matching the judge contract
            outputs = {
                "files": {"diagnosis.txt": diagnosis_text},
                "annotations": annotations,
                "stdout": diagnosis_text,
                "conversation": diagnosis_text,
                "events": [],
                "tool_calls": tool_calls,
            }

            # Load run_result.json for duration
            run_result = _load_run_result(scenario_dir)

            # Run judges
            scores = {}
            for name, judge_fn in [
                ("root_cause_correct", judge_root_cause),
                ("remediation_actionable", judge_remediation),
                ("false_positive", judge_false_positive),
                ("evidence_citation", judge_evidence_citation),
                ("completeness", judge_completeness),
                ("hallucination", judge_hallucination),
            ]:
                try:
                    value, rationale = judge_fn(outputs)
                    scores[name] = {
                        "value": value,
                        "rationale": rationale,
                    }
                except Exception as e:
                    scores[name] = {
                        "value": None,
                        "rationale": "Judge error: %s" % e,
                    }
                    print("  ERROR: Judge '%s' failed on %s/%s: %s" %
                          (name, config_name, scenario_id, e),
                          file=sys.stderr)

            scores["tool_call_count"] = {
                "value": len(tool_calls),
                "rationale": "%d tool calls" % len(tool_calls),
            }
            scores["duration_s"] = {"value": run_result.get("duration_s"), "rationale": ""}
            config_scores[scenario_id] = scores

            print("  Scored: %s/%s" % (config_name, scenario_id))

        all_scores[config_name] = config_scores

    # Combine config-b-runN into a single config-b using majority vote / median
    repeat_names = sorted(c for c in config_names if re.match(r"config-b-run\d+$", c))
    if repeat_names and not all_scores.get("config-b"):
        merged = {}
        for sid in sorted(all_scenarios):
            runs = [all_scores[rc][sid] for rc in repeat_names if sid in all_scores.get(rc, {})]
            if not runs:
                continue
            combined = {}
            for key in runs[0]:
                vals = [r[key]["value"] for r in runs if key in r and r[key].get("value") is not None]
                rats = [r[key].get("rationale", "") for r in runs if key in r and r[key].get("value") is not None]
                if not vals:
                    combined[key] = {"value": None, "rationale": ""}
                elif isinstance(vals[0], bool):
                    passes = sum(1 for v in vals if v)
                    winner = passes > len(vals) / 2
                    rat = next((r for v, r in zip(vals, rats) if v == winner), "")
                    combined[key] = {"value": winner, "rationale": rat}
                elif isinstance(vals[0], (int, float)):
                    med = _median(vals)
                    closest = min(range(len(vals)), key=lambda i: abs(vals[i] - med))
                    combined[key] = {"value": round(med, 2) if isinstance(med, float) else med, "rationale": rats[closest]}
                else:
                    combined[key] = runs[0][key]
            merged[sid] = combined
        all_scores["config-b"] = merged

    # Only use primary configs (config-a, config-b, config-c) for the report
    report_configs = sorted(c for c in all_scores if not re.match(r"config-b-run\d+$", c))

    # Build scored results
    scored_results = _build_scored_results(
        {k: v for k, v in all_scores.items() if k in report_configs},
        sorted(all_scenarios),
    )

    # Add per-run consistency data for config-b-run* directories
    if repeat_names:
        consistency = {}
        for sid in sorted(all_scenarios):
            runs = []
            for rc in repeat_names:
                scores = all_scores.get(rc, {}).get(sid, {})
                rc_val = scores.get("root_cause_correct", {}).get("value")
                if rc_val is not None:
                    runs.append(rc_val)
            if runs:
                consistency[sid] = runs
        scored_results["consistency"] = consistency

    # Write scored JSON
    scored_file = results_dir / "scored-results.json"
    with open(scored_file, "w") as f:
        json.dump(scored_results, f, indent=2, default=str)
    print("\nScored JSON: %s" % scored_file)

    # Write scored CSV
    csv_file = results_dir / "scored-results.csv"
    _write_csv(csv_file, scored_results)
    print("Scored CSV: %s" % csv_file)

    # Generate comparison report
    report_file = results_dir / "eval-report.md"
    _write_report(report_file, scored_results, report_configs, results_dir, dataset_dir)
    print("Report: %s" % report_file)


def _load_tool_calls(scenario_dir):
    """Load tool-calls.json produced by parse-stream.py."""
    tc_file = scenario_dir / "tool-calls.json"
    if not tc_file.exists():
        return []
    try:
        with open(tc_file) as f:
            data = json.load(f)
        if isinstance(data, list):
            return data
        return []
    except (json.JSONDecodeError, OSError) as e:
        print("  WARNING: Failed to read %s: %s" % (tc_file, e),
              file=sys.stderr)
        return []


def _load_annotations(dataset_dir, scenario_id):
    """Load annotations.yaml for a scenario."""
    ann_file = dataset_dir / scenario_id / "annotations.yaml"
    if not ann_file.exists():
        return None
    try:
        with open(ann_file) as f:
            return yaml.safe_load(f) or {}
    except Exception as e:
        print("  WARNING: Failed to parse %s: %s" % (ann_file, e),
              file=sys.stderr)
        return None


def _load_run_result(scenario_dir):
    run_result_file = scenario_dir / "run_result.json"
    if not run_result_file.exists():
        return {}
    try:
        with open(run_result_file) as f:
            return json.load(f)
    except (json.JSONDecodeError, OSError):
        return {}


def _build_scored_results(all_scores, scenarios):
    result = {"scenarios": []}
    summary = {}

    for scenario_id in scenarios:
        entry = {"scenario_id": scenario_id, "configs": {}}
        for config_name, config_scores in sorted(all_scores.items()):
            if scenario_id in config_scores:
                scores = config_scores[scenario_id]
                entry["configs"][config_name] = {
                    "root_cause_correct": scores.get("root_cause_correct", {}).get("value"),
                    "root_cause_rationale": scores.get("root_cause_correct", {}).get("rationale", ""),
                    "remediation_actionable": scores.get("remediation_actionable", {}).get("value"),
                    "remediation_rationale": scores.get("remediation_actionable", {}).get("rationale", ""),
                    "false_positive": scores.get("false_positive", {}).get("value"),
                    "false_positive_rationale": scores.get("false_positive", {}).get("rationale", ""),
                    "evidence_citation": scores.get("evidence_citation", {}).get("value"),
                    "evidence_rationale": scores.get("evidence_citation", {}).get("rationale", ""),
                    "completeness": scores.get("completeness", {}).get("value"),
                    "completeness_rationale": scores.get("completeness", {}).get("rationale", ""),
                    "hallucination": scores.get("hallucination", {}).get("value"),
                    "hallucination_rationale": scores.get("hallucination", {}).get("rationale", ""),
                    "tool_call_count": scores.get("tool_call_count", {}).get("value", 0),
                    "duration_s": scores.get("duration_s", {}).get("value", 0),
                }

                # Aggregate for summary
                if config_name not in summary:
                    summary[config_name] = {
                        "root_cause_pass": 0, "root_cause_total": 0,
                        "remediation_pass": 0, "remediation_total": 0,
                        "false_positive_pass": 0, "false_positive_total": 0,
                        "evidence_scores": [], "completeness_scores": [],
                        "hallucination_scores": [],
                        "tool_calls": [], "durations": [],
                    }
                s = summary[config_name]
                rc = scores.get("root_cause_correct", {}).get("value")
                if rc is not None:
                    s["root_cause_total"] += 1
                    if rc:
                        s["root_cause_pass"] += 1
                rem = scores.get("remediation_actionable", {}).get("value")
                if rem is not None:
                    s["remediation_total"] += 1
                    if rem:
                        s["remediation_pass"] += 1
                fp = scores.get("false_positive", {}).get("value")
                if fp is not None:
                    s["false_positive_total"] += 1
                    if fp:
                        s["false_positive_pass"] += 1
                ev = scores.get("evidence_citation", {}).get("value")
                if ev is not None:
                    s["evidence_scores"].append(ev)
                comp = scores.get("completeness", {}).get("value")
                if comp is not None:
                    s["completeness_scores"].append(comp)
                hal = scores.get("hallucination", {}).get("value")
                if hal is not None:
                    s["hallucination_scores"].append(hal)
                tc = scores.get("tool_call_count", {}).get("value", 0)
                s["tool_calls"].append(tc)
                dur = scores.get("duration_s", {}).get("value")
                if dur is not None:
                    s["durations"].append(dur)

        result["scenarios"].append(entry)

    # Compute summary rates
    result["summary"] = {}
    for config_name, s in sorted(summary.items()):
        result["summary"][config_name] = {
            "root_cause_pass_rate": (s["root_cause_pass"] / s["root_cause_total"]
                                     if s["root_cause_total"] > 0 else None),
            "remediation_pass_rate": (s["remediation_pass"] / s["remediation_total"]
                                      if s["remediation_total"] > 0 else None),
            "false_positive_pass_rate": (s["false_positive_pass"] / s["false_positive_total"]
                                         if s["false_positive_total"] > 0 else None),
            "avg_evidence_citation": (sum(s["evidence_scores"]) / len(s["evidence_scores"])
                                      if s["evidence_scores"] else None),
            "avg_completeness": (sum(s["completeness_scores"]) / len(s["completeness_scores"])
                                 if s["completeness_scores"] else None),
            "avg_hallucination": (sum(s["hallucination_scores"]) / len(s["hallucination_scores"])
                                  if s["hallucination_scores"] else None),
            "avg_tool_calls": (sum(s["tool_calls"]) / len(s["tool_calls"])
                               if s["tool_calls"] else None),
            "avg_duration_s": (sum(s["durations"]) / len(s["durations"])
                               if s["durations"] else None),
            "scenarios_scored": s["root_cause_total"],
        }

    return result


def _write_csv(csv_file, scored_results):
    with open(csv_file, "w", newline="") as f:
        writer = csv.writer(f)
        writer.writerow([
            "scenario", "config",
            "root_cause_correct", "root_cause_rationale",
            "remediation_actionable", "remediation_rationale",
            "false_positive", "false_positive_rationale",
            "evidence_citation", "evidence_rationale",
            "completeness", "completeness_rationale",
            "hallucination", "hallucination_rationale",
            "tool_call_count", "duration_s",
        ])
        for s in scored_results["scenarios"]:
            for config_name, data in sorted(s["configs"].items()):
                writer.writerow([
                    s["scenario_id"], config_name,
                    data.get("root_cause_correct", ""),
                    data.get("root_cause_rationale", ""),
                    data.get("remediation_actionable", ""),
                    data.get("remediation_rationale", ""),
                    data.get("false_positive", ""),
                    data.get("false_positive_rationale", ""),
                    data.get("evidence_citation", ""),
                    data.get("evidence_rationale", ""),
                    data.get("completeness", ""),
                    data.get("completeness_rationale", ""),
                    data.get("hallucination", ""),
                    data.get("hallucination_rationale", ""),
                    data.get("tool_call_count", ""),
                    data.get("duration_s", ""),
                ])


def _write_report(report_file, scored_results, config_names, results_dir, dataset_dir):
    lines = []
    lines.append("# Eval Report: 3-Config Diagnostic Comparison\n")

    # Summary table
    lines.append("## Summary\n")
    lines.append("| Metric | %s |" % " | ".join(config_names))
    lines.append("|--------|%s|" % "|".join(["--------"] * len(config_names)))

    summary = scored_results.get("summary", {})

    def _fmt_rate(config, key):
        val = summary.get(config, {}).get(key)
        if val is None:
            return "N/A"
        return "%.0f%%" % (val * 100)

    def _fmt_num(config, key):
        val = summary.get(config, {}).get(key)
        if val is None:
            return "N/A"
        return "%.1f" % val

    lines.append("| Root cause correct | %s |" %
                 " | ".join(_fmt_rate(c, "root_cause_pass_rate") for c in config_names))
    lines.append("| Remediation actionable | %s |" %
                 " | ".join(_fmt_rate(c, "remediation_pass_rate") for c in config_names))
    lines.append("| No false positives | %s |" %
                 " | ".join(_fmt_rate(c, "false_positive_pass_rate") for c in config_names))
    lines.append("| Avg evidence citation | %s |" %
                 " | ".join(_fmt_num(c, "avg_evidence_citation") for c in config_names))
    lines.append("| Avg completeness | %s |" %
                 " | ".join(_fmt_num(c, "avg_completeness") for c in config_names))
    lines.append("| Avg hallucination score | %s |" %
                 " | ".join(_fmt_num(c, "avg_hallucination") for c in config_names))
    lines.append("| Avg tool calls | %s |" %
                 " | ".join(_fmt_num(c, "avg_tool_calls") for c in config_names))
    lines.append("| Avg duration (s) | %s |" %
                 " | ".join(_fmt_num(c, "avg_duration_s") for c in config_names))
    lines.append("| Scenarios scored | %s |" %
                 " | ".join(str(summary.get(c, {}).get("scenarios_scored", 0))
                            for c in config_names))
    lines.append("")

    # Per-scenario details
    lines.append("## Per-Scenario Results\n")
    for scenario in scored_results["scenarios"]:
        lines.append("### %s\n" % scenario["scenario_id"])
        lines.append("| Metric | %s |" % " | ".join(config_names))
        lines.append("|--------|%s|" % "|".join(["--------"] * len(config_names)))

        def _get(config, field, scenario=scenario):
            data = scenario["configs"].get(config, {})
            val = data.get(field)
            if val is None:
                return "N/A"
            if isinstance(val, bool):
                return "PASS" if val else "FAIL"
            if isinstance(val, float):
                return "%.1f" % val
            return str(val)

        lines.append("| Root cause | %s |" %
                     " | ".join(_get(c, "root_cause_correct") for c in config_names))
        lines.append("| Remediation | %s |" %
                     " | ".join(_get(c, "remediation_actionable") for c in config_names))
        lines.append("| No false positives | %s |" %
                     " | ".join(_get(c, "false_positive") for c in config_names))
        lines.append("| Evidence citation | %s |" %
                     " | ".join(_get(c, "evidence_citation") for c in config_names))
        lines.append("| Completeness | %s |" %
                     " | ".join(_get(c, "completeness") for c in config_names))
        lines.append("| Hallucination | %s |" %
                     " | ".join(_get(c, "hallucination") for c in config_names))
        lines.append("| Tool calls | %s |" %
                     " | ".join(_get(c, "tool_call_count") for c in config_names))
        lines.append("| Duration (s) | %s |" %
                     " | ".join(_get(c, "duration_s") for c in config_names))
        lines.append("")

        # Rationales
        for config_name in config_names:
            data = scenario["configs"].get(config_name, {})
            rationales = []
            for key in ("root_cause_rationale", "remediation_rationale",
                        "false_positive_rationale", "hallucination_rationale"):
                r = data.get(key, "")
                if r:
                    label = key.replace("_rationale", "").replace("_", " ").title()
                    rationales.append("- **%s**: %s" % (label, r))
            if rationales:
                lines.append("**%s rationale:**\n" % config_name)
                lines.append("\n".join(rationales))
                lines.append("")

    # --- Analysis sections ---
    append_statistical(lines, scored_results, config_names)
    append_categories(lines, scored_results, dataset_dir)
    append_agent_vs_classifier(lines, scored_results, dataset_dir)
    append_consistency(lines, scored_results, results_dir)
    append_verdict(lines, scored_results)

    with open(report_file, "w") as f:
        f.write("\n".join(lines))


if __name__ == "__main__":
    main()
