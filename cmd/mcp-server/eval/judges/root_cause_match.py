"""Judge: Did the diagnosis correctly identify the root cause?

Contract: def judge(outputs, **kwargs) -> (bool, str)
"""

import json
import re

from judges.common import get_diagnosis_text, word_match, STOP_WORDS


def judge(outputs, **kwargs):
    annotations = outputs.get("annotations", {})
    if not annotations:
        return (False, "No ground truth annotations available")

    expected_root_cause = annotations.get("root_cause", "")
    if not expected_root_cause:
        return (False, "Ground truth has no root_cause field")

    expected_classification = annotations.get("expected_classification", {})
    expected_error_code = expected_classification.get("error_code", 0)
    expected_category = (expected_classification.get("category", "") or "").strip()
    expected_subcategory = expected_classification.get("subcategory", "")

    if not isinstance(expected_error_code, int) or isinstance(expected_error_code, bool):
        return (False, "Ground truth error_code must be an integer")

    diagnosis_text = get_diagnosis_text(outputs)
    if not diagnosis_text:
        return (False, "No diagnostic output found in files, stdout, "
                "or conversation")

    normalized_category = expected_category.lower()
    is_healthy = expected_error_code == 0 and normalized_category in ("none", "")
    if is_healthy:
        return _check_healthy(diagnosis_text)

    json_result = _try_json_match(diagnosis_text, expected_error_code)
    if json_result is not None:
        return json_result

    return _semantic_match(
        diagnosis_text, expected_root_cause,
        expected_error_code, expected_category, expected_subcategory,
    )


def _check_healthy(diagnosis_text):
    """For healthy scenarios, verify agent declared healthy."""
    text_lower = diagnosis_text.lower()

    # Config C: check JSON error_code directly
    try:
        data = json.loads(diagnosis_text.strip())
        if isinstance(data, dict) and "error_code" in data:
            if data["error_code"] == 0:
                return (True, "Correctly returned error_code 0")
            return (False, "Expected healthy (error_code 0), got %d"
                    % data["error_code"])
    except (json.JSONDecodeError, ValueError):
        pass

    healthy_phrases = (
        "platform healthy", "cluster is healthy", "no active failures",
        "no issues", "no failures", "no problems", "no action needed",
    )
    failure_phrases = (
        "failure detected", "error detected", "root cause is",
        "root cause:", "unhealthy", "degraded state",
    )

    declared_healthy = any(word_match(p, text_lower) for p in healthy_phrases)
    reported_failure = any(word_match(p, text_lower) for p in failure_phrases)

    if declared_healthy and not reported_failure:
        return (True, "Correctly identified cluster as healthy")
    if reported_failure:
        return (False, "Reported failure on healthy cluster")
    return (False, "Did not clearly declare cluster as healthy. "
            "Output: %s" % diagnosis_text[:200])


def _try_json_match(text, expected_error_code):
    """Match error_code from JSON output (Config C).

    Returns (bool, str) if JSON with error_code found, None otherwise.
    """
    candidates = [text.strip()]
    candidates.extend(
        line.strip() for line in text.strip().splitlines()
        if line.strip().startswith("{")
    )

    for candidate in candidates:
        try:
            data = json.loads(candidate)
        except (json.JSONDecodeError, ValueError):
            continue
        if not isinstance(data, dict) or "error_code" not in data:
            continue
        actual_code = data["error_code"]
        if not isinstance(actual_code, int) or isinstance(actual_code, bool):
            return (False, "error_code is not an integer: %s" % type(actual_code).__name__)
        if actual_code == expected_error_code:
            return (True, "Error code %d matched" % expected_error_code)
        return (False, "Error code mismatch: expected %d, got %d"
                % (expected_error_code, actual_code))
    return None


def _semantic_match(text, expected_root_cause, error_code, category, subcategory):
    """Semantic matching for free-form text (Config A/B)."""
    text_lower = text.lower()
    key_terms = _extract_key_terms(expected_root_cause.lower())
    matched_terms = [t for t in key_terms if word_match(t, text_lower)]
    match_ratio = len(matched_terms) / len(key_terms) if key_terms else 0.0

    # Word-boundary match for error code to prevent 100 matching 1001
    error_code_str = str(error_code)
    error_code_found = bool(
        re.search(r"(?<!\d)" + re.escape(error_code_str) + r"(?!\d)", text)
    )

    category_found = word_match(category.lower(), text_lower) if category else False
    subcategory_found = (
        word_match(subcategory.lower().replace("-", " "), text_lower)
        if subcategory else False
    )

    if match_ratio >= 0.5 or error_code_found or (category_found and subcategory_found):
        details = []
        if match_ratio >= 0.5:
            details.append("key terms %d/%d" % (len(matched_terms), len(key_terms)))
        if error_code_found:
            details.append("error code %d" % error_code)
        if category_found:
            details.append("category '%s'" % category)
        return (True, "Root cause identified: %s" % ", ".join(details))

    return (False,
            "Root cause not identified. Expected: '%s'. "
            "Terms matched: %d/%d, error code %d found: %s"
            % (expected_root_cause[:100], len(matched_terms),
               len(key_terms), error_code, error_code_found))


def _extract_key_terms(text):
    words = re.findall(r"[a-z0-9][-a-z0-9]*", text)
    return [w for w in words if w not in STOP_WORDS and len(w) > 2]
