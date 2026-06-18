"""Judge: Does the diagnosis fabricate resources or conditions?

Contract: def judge(outputs, **kwargs) -> (float, str)
Returns 1.0 when fully grounded, 0.0 when nothing is.
"""

import json
import re

from judges.common import get_diagnosis_text

_RESOURCE_RE = re.compile(
    r"(?:deployment|deploy|pod|service|svc|configmap|cm|secret|namespace|ns|"
    r"statefulset|sts|daemonset|ds|replicaset|rs|job|cronjob|route|"
    r"networkpolicy|pvc|serviceaccount|sa)"
    r"[/\s]+([a-z][a-z0-9-]{2,})",
    re.IGNORECASE,
)
_NS_RE = re.compile(r"(?:namespace|ns)[:/\s]+([a-z][a-z0-9-]{2,})", re.IGNORECASE)
_CMD_RE = re.compile(r"(?:kubectl|oc)\s+\w+\s+(?:\w+/)?([a-z][a-z0-9-]{2,})", re.IGNORECASE)

_GENERIC = frozenset({
    "the", "all", "any", "new", "old", "test", "default",
    "kube-system", "kube-public", "openshift", "openshift-operators",
    "openshift-monitoring", "openshift-ingress", "openshift-config",
    "redhat-ods-operator", "redhat-ods-applications",
    "redhat-ods-monitoring", "opendatahub", "opendatahub-operator",
})


def judge(outputs, **kwargs):
    annotations = outputs.get("annotations", {})
    if not annotations:
        return (0.0, "No ground truth annotations")

    text = get_diagnosis_text(outputs)
    if not text:
        return (1.0, "No output — nothing to hallucinate")

    try:
        json.loads(text.strip())
        return (1.0, "Config C JSON — no hallucination risk")
    except (json.JSONDecodeError, ValueError):
        pass

    truth = _build_truth(annotations)
    if not truth:
        return (1.0, "No ground truth terms to validate against")

    entities = _extract(text)
    if not entities:
        return (1.0, "No resource entities found in output")

    grounded = {e for e in entities if _is_grounded(e, truth)}
    fabricated = entities - grounded
    score = len(grounded) / len(entities)

    if fabricated:
        return (score, "Hallucinated %d/%d: [%s]" % (
            len(fabricated), len(entities), ", ".join(sorted(fabricated)[:5])))
    return (score, "All %d entities grounded" % len(entities))


def _build_truth(annotations):
    parts = []
    for key in ("root_cause", "expected_remediation", "description",
                "scenario_name", "scenario_id"):
        val = annotations.get(key, "")
        if val:
            parts.append(val)
    for s in annotations.get("expected_symptoms", []):
        parts.append(s)
    for t in annotations.get("tools_that_detect", []):
        parts.append(t)
    return set(re.findall(r"[a-z][a-z0-9-]{2,}", " ".join(parts).lower()))


def _extract(text):
    found = set()
    for pattern in (_RESOURCE_RE, _NS_RE, _CMD_RE):
        for m in pattern.finditer(text):
            name = m.group(1).lower().rstrip("-")
            if name not in _GENERIC and len(name) > 2:
                found.add(name)
    return found


_MIN_SUBSTR_LEN = 5
_OVERLAP_RATIO = 0.65


def _segments_match(a_segs, b_segs):
    """True if a's segments are a contiguous subsequence of b's."""
    if len(a_segs) > len(b_segs):
        return False
    for i in range(len(b_segs) - len(a_segs) + 1):
        if b_segs[i:i + len(a_segs)] == a_segs:
            return True
    return False


def _is_grounded(entity, truth):
    if entity in truth:
        return True
    if len(entity) < _MIN_SUBSTR_LEN:
        return False
    e_segs = entity.split("-")
    for t in truth:
        if len(t) < _MIN_SUBSTR_LEN:
            continue
        t_segs = t.split("-")
        if len(e_segs) > 1 or len(t_segs) > 1:
            if _segments_match(e_segs, t_segs) or _segments_match(t_segs, e_segs):
                return True
        else:
            shorter, longer = sorted([entity, t], key=len)
            if shorter in longer and len(shorter) >= len(longer) * _OVERLAP_RATIO:
                return True
    return False
