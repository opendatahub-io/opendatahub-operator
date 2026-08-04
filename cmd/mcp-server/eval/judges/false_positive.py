"""Judge: False positive detection and category alignment.

Follows the agent-eval-harness external code judge contract:
  def judge(outputs, **kwargs) -> (bool, str)

Healthy scenarios: checks if the agent falsely reported failures
on a healthy cluster.

Failure scenarios: checks category alignment — verifies the
diagnosis mentions terms consistent with the expected failure
category.
"""

import json

from judges.common import get_diagnosis_text, word_match


def judge(outputs, **kwargs):
    annotations = outputs.get("annotations", {})
    if not annotations:
        return (False, "Cannot evaluate false positives: no ground truth "
                "annotations in outputs['annotations']")

    expected_classification = annotations.get("expected_classification", {})
    expected_error_code = expected_classification.get("error_code", 0)
    expected_category = (expected_classification.get("category", "") or "").strip()

    if not isinstance(expected_error_code, int) or isinstance(expected_error_code, bool):
        return (False, "Ground truth error_code must be an integer")

    diagnosis_text = get_diagnosis_text(outputs)
    if not diagnosis_text:
        return (True, "No diagnostic output produced — nothing to check "
                "for false positives")

    normalized_category = expected_category.lower()
    is_healthy = expected_error_code == 0 and normalized_category in ("none", "")
    if is_healthy:
        return _check_healthy_scenario(diagnosis_text)

    return _check_category_alignment(diagnosis_text, normalized_category)


def _check_healthy_scenario(diagnosis_text):
    """Healthy cluster — any reported active failure is a false positive."""
    text_lower = diagnosis_text.lower()

    # Config C: check JSON error_code directly
    try:
        data = json.loads(diagnosis_text.strip())
        if isinstance(data, dict) and "error_code" in data:
            code = data["error_code"]
            if not isinstance(code, int) or isinstance(code, bool):
                return (False, "Invalid error_code type in classifier JSON: "
                        "%s (%s)" % (code, type(code).__name__))
            if code == 0:
                return (True, "Classifier correctly returned error_code 0")
            return (False, "False positive: classifier returned error_code %d "
                    "on healthy cluster" % code)
    except (json.JSONDecodeError, ValueError):
        pass

    # Negated failure phrases that indicate the agent correctly dismissed issues
    negation_phrases = (
        "no root cause", "no failure", "no active failure",
        "no issues found", "no problems detected", "not a failure",
        "events are stale", "events are historical", "previously resolved",
        "does not reflect current", "no action needed",
    )
    has_negation = any(word_match(p, text_lower) for p in negation_phrases)

    # Phrases that indicate the agent is reporting an active failure
    active_failure_phrases = (
        "failure detected", "error detected", "root cause:",
        "root cause is", "identified the following failure",
        "unhealthy", "degraded state", "critical failure",
        "crashloopbackoff", "imagepullbackoff",
        "container oom", "oomkilled",
    )
    reported_failures = [
        p for p in active_failure_phrases
        if word_match(p, text_lower)
    ]

    # Healthy declaration phrases
    healthy_phrases = (
        "platform healthy", "platform is healthy",
        "cluster is healthy", "cluster healthy",
        "all checks pass", "all checks passed",
        "all health checks passed",
        "no issues", "no failures", "no problems", "healthy state",
        "operating normally",
    )
    declared_healthy = any(word_match(p, text_lower) for p in healthy_phrases)

    if declared_healthy and not reported_failures:
        return (True, "Correctly identified healthy cluster")

    if has_negation and not reported_failures:
        return (True, "Correctly dismissed stale events as non-active")

    if reported_failures:
        return (False, "False positive on healthy cluster: actively reported "
                "[%s]" % ", ".join(reported_failures))

    return (False, "Ambiguous output on healthy cluster — neither declared "
            "healthy nor reported failures. Output: %s" % text_lower[:200])


def _check_category_alignment(diagnosis_text, expected_category):
    """Check diagnosis mentions terms consistent with the expected category."""
    text_lower = diagnosis_text.lower()

    correct_category_terms = {
        "infrastructure": [
            "imagepullbackoff", "errimagepull", "image not found",
            "pull access denied",
            "crashloopbackoff", "back-off restarting", "restart loop",
            "oomkilled", "exit code 137", "out of memory", "memory limit",
            "node pressure", "disk pressure", "memory pressure",
            "pid pressure",
            "quota exceeded", "resource quota", "exceeded quota",
            "limit exceeded",
            "scaled to 0",
            "storage mount failed", "mount failed",
            "serviceaccount deleted",
            "readiness probe failed", "probe failed",
            "network not ready",
            "permission denied",
        ],
        "unknown": [
            "unclassifiable", "not installed", "missing dependency",
            "unknown error", "unrecognized", "cannot determine",
        ],
        "none": [],
        "": [],
    }

    terms = correct_category_terms.get(expected_category)
    if terms is None:
        return (False, "Unhandled category '%s' — add it to "
                "correct_category_terms in false_positive.py"
                % expected_category)

    matched = [t for t in terms if word_match(t, text_lower)]
    if matched:
        return (True, "Diagnosis consistent with category '%s': [%s]"
                % (expected_category, ", ".join(matched[:3])))

    return (False, "Category mismatch: expected '%s' but no matching "
            "terms found in output" % expected_category)
