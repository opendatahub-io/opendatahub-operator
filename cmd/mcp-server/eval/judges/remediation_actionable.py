"""Judge: Is the remediation specific and actionable?

Contract: def judge(outputs, **kwargs) -> (bool, str)
"""

import json
import re

from judges.common import get_diagnosis_text, word_match, STOP_WORDS


def judge(outputs, **kwargs):
    annotations = outputs.get("annotations", {})
    if not annotations:
        return (False, "No ground truth annotations available")

    expected_remediation = annotations.get("expected_remediation", "")
    if not expected_remediation:
        return (False, "Ground truth has no expected_remediation — "
                "cannot evaluate without a reference answer")

    expected_error_code = annotations.get(
        "expected_classification", {},
    ).get("error_code", 0)
    expected_category = (annotations.get(
        "expected_classification", {},
    ).get("category", "") or "").strip()

    if not isinstance(expected_error_code, int) or isinstance(expected_error_code, bool):
        return (False, "Ground truth error_code must be an integer")

    diagnosis_text = get_diagnosis_text(outputs)
    if not diagnosis_text:
        return (False, "No diagnostic output found")

    normalized_category = expected_category.lower()
    is_healthy = expected_error_code == 0 and normalized_category in ("none", "")
    if is_healthy:
        return _check_healthy(diagnosis_text)

    # Config C JSON output
    try:
        data = json.loads(diagnosis_text.strip())
        if isinstance(data, dict) and "error_code" in data:
            rem = data.get("remediation", "")
            if not rem:
                return (False, "Classifier JSON has no remediation field")
            return _assess(rem, expected_remediation)
    except (json.JSONDecodeError, ValueError):
        pass

    remediation_text = _extract_remediation(diagnosis_text)
    if not remediation_text:
        return (False, "No remediation found: no markdown headers "
                "(## Remediation/Fix/Resolution/Actions) and no lines "
                "containing commands (kubectl, oc, scale, delete, apply)")

    return _assess(remediation_text, expected_remediation)


def _check_healthy(diagnosis_text):
    """Healthy scenario — verify agent isn't suggesting unnecessary fixes."""
    text_lower = diagnosis_text.lower()
    no_action = word_match("no action needed", text_lower) or \
                word_match("no action required", text_lower) or \
                word_match("no remediation", text_lower)
    suggests_fix = any(word_match(cmd, text_lower) for cmd in _COMMANDS)

    if no_action and not suggests_fix:
        return (True, "Correctly stated no action needed")
    if suggests_fix:
        return (False, "Suggested fix commands on a healthy cluster")
    return (False, "Did not explicitly state no action needed "
            "for healthy cluster")


def _extract_remediation(text):
    for pattern in (
        r"(?i)##?\s*remediation[:\s]*(.*?)(?=\n##|\Z)",
        r"(?i)##?\s*fix[:\s]*(.*?)(?=\n##|\Z)",
        r"(?i)##?\s*resolution[:\s]*(.*?)(?=\n##|\Z)",
        r"(?i)##?\s*(?:recommended?\s*)?action[s]?[:\s]*(.*?)(?=\n##|\Z)",
    ):
        match = re.search(pattern, text, re.DOTALL)
        if match and len(match.group(1).strip()) > 10:
            return match.group(1).strip()

    lines = [line.strip() for line in text.splitlines()
             if any(word_match(c, line.strip().lower()) for c in _COMMANDS)]
    if lines:
        return "\n".join(lines)
    return ""

# Multi-word commands only — avoids single-word substring collisions
_COMMANDS = (
    "kubectl apply", "kubectl delete", "kubectl scale", "kubectl patch",
    "kubectl get", "oc apply", "oc delete", "oc scale", "oc patch",
    "scale deployment", "delete pod", "delete networkpolicy",
    "apply -f", "patch deployment",
)

_GENERIC = (
    "check the logs", "investigate further", "review the configuration",
    "contact support", "see documentation", "try again later",
)

_SPECIFIC = (
    "kubectl rollout", "kubectl set image", "kubectl describe",
    "kubectl delete", "kubectl scale", "kubectl apply", "kubectl patch",
    "oc apply", "oc delete", "oc scale", "oc patch", "oc rollout",
    "replicas", "deployment/", "networkpolicy", "resourcequota",
)


def _assess(remediation_text, expected_remediation):
    text_lower = remediation_text.lower()

    generic = [p for p in _GENERIC if word_match(p, text_lower)]
    specific = [s for s in _SPECIFIC if word_match(s, text_lower)]

    # Stop-word-filtered overlap so "the" and "to" don't inflate score
    exp = {w for w in expected_remediation.lower().split()
           if w not in STOP_WORDS and len(w) > 2}
    act = {w for w in text_lower.split()
           if w not in STOP_WORDS and len(w) > 2}
    overlap = len(exp & act) / len(exp) if exp else 0.0

    if specific:
        return (True, "Actionable: found [%s] (overlap: %.0f%%)"
                % (", ".join(specific[:3]), overlap * 100))

    if overlap >= 0.4:
        return (True, "Matches expected fix (%.0f%% overlap)" % (overlap * 100))

    if generic:
        return (False, "Too generic: [%s]" % ", ".join(generic))

    if len(remediation_text.strip()) < 20:
        return (False, "Too brief (%d chars)" % len(remediation_text.strip()))

    return (False, "No actionable commands found: '%s'" % remediation_text[:100])
