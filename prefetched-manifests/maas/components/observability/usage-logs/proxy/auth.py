"""Authentication via Kubernetes TokenReview API."""
import json
import logging
import os
import ssl
from urllib.request import Request, urlopen

SA_TOKEN_PATH = "/var/run/secrets/kubernetes.io/serviceaccount/token"

logger = logging.getLogger(__name__)


class UserInfo:
    """User information extracted from token."""

    def __init__(self, username):
        self.username = username


def create_ssl_context():
    """Create SSL context with Kubernetes CA certificates."""
    ca_paths = [
        "/var/run/secrets/kubernetes.io/serviceaccount/service-ca.crt",
        "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt",
    ]

    context = ssl.create_default_context()

    for ca_path in ca_paths:
        try:
            context.load_verify_locations(ca_path)
            logger.info(f"Loaded CA certificate from {ca_path}")
        except OSError:
            pass

    return context


def extract_from_bearer_token(auth_header, token_review_url, ssl_context):
    """
    Extract user info from Authorization: Bearer <token>.

    All tokens are validated server-side via the Kubernetes TokenReview API.
    """
    if not auth_header:
        raise ValueError("missing Authorization header")

    parts = auth_header.split(" ", 1)
    if len(parts) != 2 or parts[0].lower() != "bearer":
        raise ValueError("invalid Authorization header format")

    return resolve_token(parts[1], token_review_url, ssl_context)


def resolve_token(token, token_review_url, ssl_context):
    """
    Call the Kubernetes TokenReview API to validate bearer token.

    This is the sole authentication path -- tokens are never parsed or
    trusted locally.
    """
    try:
        with open(SA_TOKEN_PATH, "r") as f:
            sa_token = f.read().strip()
    except OSError as e:
        raise ValueError(f"cannot read SA token for TokenReview: {e}") from e

    body = {
        "apiVersion": "authentication.k8s.io/v1",
        "kind": "TokenReview",
        "spec": {"token": token},
    }

    body_bytes = json.dumps(body).encode("utf-8")

    req = Request(
        token_review_url,
        data=body_bytes,
        headers={
            "Content-Type": "application/json",
            "Authorization": f"Bearer {sa_token}",
        },
        method="POST",
    )

    try:
        with urlopen(req, timeout=10, context=ssl_context) as resp:
            resp_body = resp.read()
            resp_code = resp.status
    except Exception as e:
        raise ValueError(f"TokenReview request failed: {e}") from e

    if resp_code not in (200, 201):
        raise ValueError(f"TokenReview returned HTTP {resp_code}")

    try:
        result = json.loads(resp_body)
    except json.JSONDecodeError as e:
        raise ValueError(f"failed to parse TokenReview response: {e}") from e

    status = result.get("status", {})
    if not status.get("authenticated", False):
        error_msg = status.get("error", "token not authenticated")
        raise ValueError(f"TokenReview: {error_msg}")

    user = status.get("user", {})
    username = user.get("username", "")
    if not username:
        raise ValueError("TokenReview returned empty username")

    return UserInfo(username=username)
