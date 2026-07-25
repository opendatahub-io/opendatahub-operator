#!/usr/bin/env python3
"""Loki Query Proxy - stdlib-only implementation."""
import contextvars
import hashlib
import json
import logging
import os
import ssl
import sys
import time
import uuid
from http.server import ThreadingHTTPServer, BaseHTTPRequestHandler
from threading import Lock
from urllib.parse import urlparse, parse_qs, urlencode, urljoin

from auth import extract_from_bearer_token, create_ssl_context
from rewriter import inject_user_filter

# Context variable for request ID
request_id_var = contextvars.ContextVar('request_id', default='')

# Simple in-memory cache for query results
query_cache = {}
cache_lock = Lock()
CACHE_TTL_SECONDS = 30  # Cache responses for 30 seconds
MAX_RESPONSE_SIZE = 10 * 1024 * 1024  # 10 MB limit for response bodies
MAX_CACHEABLE_SIZE = 1 * 1024 * 1024  # Only cache responses up to 1 MB

class RequestIDFilter(logging.Filter):
    """Inject request ID into log records."""
    def filter(self, record):
        record.request_id = request_id_var.get() or '-'
        return True

# Configure logging with request ID support
log_level = os.getenv('LOG_LEVEL', 'INFO').upper()
logging.basicConfig(
    level=getattr(logging, log_level, logging.INFO),
    format="%(asctime)s [%(levelname)s] [%(request_id)s] %(message)s",
    datefmt="%Y-%m-%d %H:%M:%S",
)
# Add filter to all handlers so request_id is available in all log records
for handler in logging.getLogger().handlers:
    handler.addFilter(RequestIDFilter())
logger = logging.getLogger(__name__)

# Global configuration
ssl_context = None
token_review_url = None
namespace = None

ALLOWED_PATHS = {
    "/loki/api/v1/query",
    "/loki/api/v1/query_range",
    "/health",
    "/ready",
}

HOP_BY_HOP_HEADERS = {
    "connection",
    "keep-alive",
    "proxy-authenticate",
    "proxy-authorization",
    "te",
    "trailers",
    "transfer-encoding",
    "upgrade",
    "authorization",
    "content-length",
}


def safe_read_response(response, max_size=MAX_RESPONSE_SIZE):
    """
    Read response body with size limit to prevent memory exhaustion.

    Args:
        response: HTTP response object with read() method
        max_size: Maximum bytes to read (default: MAX_RESPONSE_SIZE)

    Returns:
        bytes: Response body (up to max_size)

    Raises:
        ValueError: If response exceeds max_size
    """
    chunks = []
    bytes_read = 0
    chunk_size = 64 * 1024  # Read in 64KB chunks

    while True:
        chunk = response.read(chunk_size)
        if not chunk:
            break

        bytes_read += len(chunk)
        if bytes_read > max_size:
            raise ValueError(f"Response exceeds maximum size of {max_size} bytes")

        chunks.append(chunk)

    return b''.join(chunks)


def is_allowed_path(path):
    """Check if path is allowed."""
    if ".." in path.split("/"):
        return False
    if path in ALLOWED_PATHS:
        return True
    if path.startswith("/loki/api/v1/label/") and path.endswith("/values"):
        return True
    return False


class ProxyHandler(BaseHTTPRequestHandler):
    """HTTP request handler for the Loki proxy."""

    protocol_version = "HTTP/1.1"

    def log_message(self, format, *args):
        """Suppress default logging - we handle it ourselves."""
        pass

    def send_json_error(self, msg, code):
        """Send JSON error response."""
        response = {
            "status": "error",
            "error": msg,
            "errorType": "proxy",
            "data": None,
        }
        body = json.dumps(response).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        """Handle GET requests."""
        # Generate unique request ID for log correlation
        req_id = str(uuid.uuid4())[:8]
        request_id_var.set(req_id)

        logger.info(f"REQ GET {self.path}")

        # Health check endpoints
        if self.path in ("/health", "/ready"):
            try:
                body = b"OK\n"
                self.send_response(200)
                self.send_header("Content-Type", "text/plain")
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)
            except (BrokenPipeError, ConnectionResetError):
                # Kubernetes probes may close connection early - this is normal
                pass
            return

        # Authenticate and extract username
        auth_header = self.headers.get("Authorization", "")
        if not auth_header:
            logger.error("Missing Authorization header")
            self.send_json_error("Unauthorized", 401)
            return

        try:
            user_info = extract_from_bearer_token(
                auth_header, token_review_url, ssl_context
            )
        except Exception as e:
            logger.error(f"Auth extraction failed: {e}")
            self.send_json_error("Unauthorized", 401)
            return

        username = user_info.username

        # Parse path and check if allowed
        parsed = urlparse(self.path)
        if not is_allowed_path(parsed.path):
            logger.warning(f"Blocked access to {parsed.path} for user={username}")
            self.send_json_error("Forbidden: endpoint not available", 403)
            return

        # Parse and rewrite query parameters
        query_params = parse_qs(parsed.query, keep_blank_values=True)

        if "query" in query_params:
            original_query = query_params["query"][0]
            # Label values endpoints only support label matchers, not pipeline filters
            is_label_values = parsed.path.startswith("/loki/api/v1/label/") and parsed.path.endswith("/values")
            rewritten_query = inject_user_filter(original_query, username, namespace, labels_only=is_label_values)
            query_params["query"] = [rewritten_query]
            logger.debug(
                f"Query rewritten for user={username} (labels_only={is_label_values}): "
                f"original={original_query} -> rewritten={rewritten_query}"
            )

        # Build upstream URL
        upstream_parsed = urlparse(loki_upstream)
        upstream_path = upstream_parsed.path + parsed.path
        if query_params:
            query_string = urlencode(query_params, doseq=True)
            upstream_url = f"{upstream_parsed.scheme}://{upstream_parsed.netloc}{upstream_path}?{query_string}"
        else:
            upstream_url = f"{upstream_parsed.scheme}://{upstream_parsed.netloc}{upstream_path}"

        # Prepare upstream request
        from urllib.request import Request, urlopen

        # Copy headers, excluding hop-by-hop
        headers = {}
        for key, value in self.headers.items():
            if key.lower() not in HOP_BY_HOP_HEADERS:
                headers[key] = value

        headers["Host"] = upstream_parsed.netloc
        headers["X-Scope-OrgID"] = "application"

        # Add service account token for upstream authentication
        try:
            with open("/var/run/secrets/kubernetes.io/serviceaccount/token", "r") as f:
                sa_token = f.read().strip()
                headers["Authorization"] = f"Bearer {sa_token}"
        except OSError:
            pass

        # Check cache (cache key = rewritten query + user)
        cache_key = hashlib.sha256(f"{username}:{upstream_url}".encode()).hexdigest()
        cached_entry = None
        with cache_lock:
            if cache_key in query_cache:
                entry = query_cache[cache_key]
                if time.time() - entry["timestamp"] < CACHE_TTL_SECONDS:
                    cached_entry = {
                        **entry,
                        "headers": dict(entry["headers"]),
                    }
                else:
                    # Expired entry
                    del query_cache[cache_key]

        if cached_entry is not None:
            logger.debug(f"CACHE HIT {parsed.path}")
            self.send_response(cached_entry["code"])
            for key, value in cached_entry["headers"].items():
                self.send_header(key, value)
            self.end_headers()
            self.wfile.write(cached_entry["body"])
            return

        # Make upstream request
        try:
            from urllib.error import HTTPError
            req = Request(upstream_url, headers=headers, method="GET")
            start_time = time.time()
            with urlopen(req, timeout=60, context=ssl_context) as resp:
                try:
                    resp_body = safe_read_response(resp)
                except ValueError as e:
                    logger.error(f"Response too large: {e}")
                    self.send_json_error(f"Upstream response exceeds size limit", 502)
                    return
                resp_code = resp.status
                resp_headers = resp.headers

            elapsed_ms = int((time.time() - start_time) * 1000)
            if elapsed_ms > 3000:
                logger.warning(f"SLOW QUERY {elapsed_ms}ms {parsed.path}")
            logger.info(f"RESP {parsed.path} {resp_code} ({len(resp_body)} bytes) {elapsed_ms}ms")

            # Handle non-JSON error responses
            is_json = len(resp_body) > 0 and resp_body[0:1] in (b"{", b"[")
            if resp_code >= 400 and not is_json:
                err_msg = resp_body.decode("utf-8", errors="replace").strip()
                if not err_msg:
                    err_msg = f"Loki returned HTTP {resp_code}"
                logger.error(f"Upstream non-JSON error {resp_code}: {err_msg[:200]}")
                self.send_json_error(err_msg, resp_code)
                return

            # Filter hop-by-hop headers for forwarding
            filtered_headers = {
                k: v for k, v in resp_headers.items()
                if k.lower() not in HOP_BY_HOP_HEADERS
            }
            filtered_headers["Content-Length"] = str(len(resp_body))

            # Store successful responses in cache (with filtered headers)
            # Only cache responses under MAX_CACHEABLE_SIZE to prevent memory exhaustion
            if resp_code == 200 and len(resp_body) <= MAX_CACHEABLE_SIZE:
                with cache_lock:
                    query_cache[cache_key] = {
                        "timestamp": time.time(),
                        "code": resp_code,
                        "headers": filtered_headers,
                        "body": resp_body,
                    }
                    # Simple cache size limit (keep last 100 entries)
                    if len(query_cache) > 100:
                        oldest_key = min(query_cache.keys(), key=lambda k: query_cache[k]["timestamp"])
                        del query_cache[oldest_key]
            elif resp_code == 200:
                logger.debug(f"Response too large to cache ({len(resp_body)} bytes > {MAX_CACHEABLE_SIZE})")

            # Forward response
            self.send_response(resp_code)
            for key, value in filtered_headers.items():
                self.send_header(key, value)
            self.end_headers()
            self.wfile.write(resp_body)

        except HTTPError as e:
            # Log the error response body (with size limit)
            try:
                # Limit error body reads to 1MB to prevent memory exhaustion
                error_body = safe_read_response(e, max_size=1024 * 1024).decode('utf-8', errors='replace')
                logger.error(f"Upstream HTTP {e.code} error body: {error_body[:500]}")
            except ValueError:
                logger.error(f"Upstream HTTP {e.code}: error body too large (>1MB)")
            except Exception:
                logger.error(f"Upstream HTTP {e.code}: {e.reason}")
            self.send_json_error(f"Bad gateway: HTTP {e.code} {e.reason}", 502)
        except Exception as e:
            logger.error(f"Upstream request failed: {type(e).__name__}: {e}")
            self.send_json_error("Bad gateway: upstream request failed", 502)


def initialize():
    """Initialize global configuration."""
    global loki_upstream, ssl_context, token_review_url, namespace

    namespace = os.getenv("NAMESPACE")
    if not namespace:
        logger.fatal("NAMESPACE environment variable is required")
        sys.exit(1)

    # Build Loki upstream URL from namespace
    loki_upstream = f"https://usage-gateway-http.{namespace}.svc:8080/api/logs/v1/application"
    logger.info(f"Loki upstream: {loki_upstream}")

    # Create SSL context with CA certificates
    ssl_context = create_ssl_context()

    # Build TokenReview URL
    api_host = os.getenv("KUBERNETES_SERVICE_HOST")
    api_port = os.getenv("KUBERNETES_SERVICE_PORT", "443")
    token_review_url = (
        f"https://{api_host}:{api_port}/apis/authentication.k8s.io/v1/tokenreviews"
    )
    logger.info(f"TokenReview endpoint: {token_review_url}")


def main():
    """Main entry point."""
    initialize()

    tls_cert_file = "/etc/tls/private/tls.crt"
    tls_key_file = "/etc/tls/private/tls.key"

    if not os.path.exists(tls_cert_file):
        logger.fatal(f"TLS cert file not found: {tls_cert_file}")
        sys.exit(1)
    if not os.path.exists(tls_key_file):
        logger.fatal(f"TLS key file not found: {tls_key_file}")
        sys.exit(1)

    server = ThreadingHTTPServer(("0.0.0.0", 8443), ProxyHandler)

    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.load_cert_chain(tls_cert_file, tls_key_file)
    # Disable client cert verification - authentication is via bearer tokens
    context.check_hostname = False
    context.verify_mode = ssl.CERT_NONE
    server.socket = context.wrap_socket(server.socket, server_side=True)

    logger.info("Proxy listening on https://0.0.0.0:8443")

    try:
        server.serve_forever()
    except KeyboardInterrupt:
        logger.info("Shutting down")
        server.shutdown()


if __name__ == "__main__":
    main()
