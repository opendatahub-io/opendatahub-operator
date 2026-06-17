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


def _is_grounded(entity, truth):
    if entity in truth:
        return True
    return any(entity in t or t in entity for t in truth)
