"""Quality scoring judges that differentiate Config A from Config B.

Two scoring dimensions:
  1. evidence_citation_score — Does it cite specific tool names as sources?
  2. completeness_score — How many expected symptoms are mentioned?

Each returns (float, str) where float is 0.0-1.0 score.
"""

import re

from judges.common import get_diagnosis_text, word_match


def judge_evidence_citation(outputs, **kwargs):
    """Score: Does the output cite specific MCP tool names as evidence sources?

    Checks for patterns like:
    - (source: platform_health)
    - (source: classify_failure)
    - tool names mentioned in evidence context

    Returns (float, str) — 0.0 to 1.0 score with rationale.
    """
    text = get_diagnosis_text(outputs)
    if not text:
        return (0.0, "No diagnostic output")

    text_lower = text.lower()

    tool_names = [
        "platform_health", "classify_failure", "recent_events",
        "pod_logs", "describe_resource", "component_status",
        "operator_dependencies",
    ]

    source_pattern = re.compile(r"\(source:\s*(\w+)\)", re.IGNORECASE)
    source_citations = source_pattern.findall(text)

    tools_mentioned = [t for t in tool_names if word_match(t, text_lower)]

    classifier_cited = bool(re.search(
        r"(?:classifier|error.code|code\s+\d{4})", text_lower
    ))

    evidence_section = bool(re.search(
        r"###?\s*evidence", text, re.IGNORECASE
    ))

    score_parts = []
    total = 0.0

    if source_citations:
        total += 0.4
        score_parts.append("%d source citations" % len(source_citations))

    if tools_mentioned:
        tool_score = min(len(tools_mentioned) / 4.0, 1.0) * 0.3
        total += tool_score
        score_parts.append("%d tools referenced" % len(tools_mentioned))

    if classifier_cited:
        total += 0.15
        score_parts.append("classifier output cited")

    if evidence_section:
        total += 0.15
        score_parts.append("evidence section present")

    total = min(total, 1.0)

    if score_parts:
        return (total, "Evidence: %s" % ", ".join(score_parts))
    return (0.0, "No evidence citations or tool references found")


def judge_completeness(outputs, **kwargs):
    """Score: How many expected symptoms from ground truth are mentioned?

    Compares expected_symptoms from annotations against the diagnosis text
    using keyword extraction and fuzzy matching.

    Returns (float, str) — 0.0 to 1.0 score with rationale.
    """
    annotations = outputs.get("annotations", {})
    if not annotations:
        return (0.0, "No ground truth annotations")

    expected_symptoms = annotations.get("expected_symptoms", [])
    if not expected_symptoms:
        return (0.0, "No expected_symptoms in ground truth")

    text = get_diagnosis_text(outputs)
    if not text:
        return (0.0, "No diagnostic output")

    text_lower = text.lower()

    matched = []
    for symptom in expected_symptoms:
        symptom_lower = symptom.lower()
        words = re.findall(r"[a-z0-9][-a-z0-9]*", symptom_lower)
        key_words = [w for w in words if len(w) > 3]

        if not key_words:
            continue

        hits = sum(1 for w in key_words if word_match(w, text_lower))
        if hits >= len(key_words) * 0.5:
            matched.append(symptom[:50])

    score = len(matched) / len(expected_symptoms) if expected_symptoms else 0.0

    if matched:
        return (score, "Completeness: %d/%d symptoms matched (%s)"
                % (len(matched), len(expected_symptoms),
                   "; ".join(matched[:3])))
    return (0.0, "No expected symptoms found in diagnosis (0/%d)"
            % len(expected_symptoms))
