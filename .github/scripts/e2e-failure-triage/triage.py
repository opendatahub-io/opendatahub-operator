#!/usr/bin/env python3
"""E2E failure triage script.

Fetches JUnit XML artifacts from Prow E2E job runs, identifies which
components had genuine test failures, and files/updates Jira blocker bugs
against the responsible component teams.
"""

import argparse
import json
import logging
import os
import re
import sys
import time
import defusedxml.ElementTree as ET
from xml.etree.ElementTree import Element  # noqa: S405 - type annotation only, not used for parsing
from dataclasses import dataclass, field
from pathlib import Path

import requests
import yaml

# ---------------------------------------------------------------------------
# Constants
# ---------------------------------------------------------------------------

GCSWEB_BASE = "https://gcsweb-ci.apps.ci.l2s4.p1.openshiftapps.com/gcs/test-platform-results"

JIRA_BASE_URL = "https://redhat.atlassian.net"
TEMPLATE_ISSUE_KEY = "RHOAIENG-79740"
JIRA_PROJECT_KEY = "RHOAIENG"
AUTO_BLOCKER_LABEL = "odh-operator-auto-e2e-blocker"

PROW_URL_PATTERNS = [
    re.compile(r"prow\.ci\.openshift\.org/view/gs/test-platform-results/(.+?)/?$"),
    re.compile(r"gcsweb-ci\.apps\.ci\.l2s4\.p1\.openshiftapps\.com/gcs/test-platform-results/(.+?)/?$"),
]

JOB_AS_NAME_PATTERN = re.compile(
    r"pull-ci-opendatahub-io-opendatahub-operator-main-(.+?)/"
)

COMPONENT_TEST_PATTERN = re.compile(
    r"^TestOdhOperator/components/group_\d+/([^/]+)/"
)

VERSION_PATTERN = re.compile(r"^\s*VERSION\s*[?:]?=\s*(\S+)", re.MULTILINE)
EA_PATTERN = re.compile(r"^(\d+\.\d+)\.\d+-ea\.(\d+)$")
GA_PATTERN = re.compile(r"^(\d+\.\d+)\.\d+$")

SCRIPT_DIR = Path(__file__).resolve().parent
DEFAULT_MAPPING_PATH = SCRIPT_DIR / "component-mapping.yaml"

log = logging.getLogger("e2e-triage")

# ---------------------------------------------------------------------------
# Data classes
# ---------------------------------------------------------------------------


@dataclass
class ComponentMapping:
    jira_component: str


@dataclass
class FailureClassification:
    category: str = ""
    subcategory: str = ""
    confidence: str = ""
    evidence: str = ""


@dataclass
class ComponentFailure:
    component: str
    test_name: str
    failure_message: str
    classification: FailureClassification = field(default_factory=FailureClassification)


@dataclass
class TriageResult:
    prow_url: str
    pr_number: str
    context: str
    junit_found: bool = False
    component_failures: list[ComponentFailure] = field(default_factory=list)
    component_mapping: dict[str, ComponentMapping] = field(default_factory=dict)
    non_component_failures_skipped: int = 0
    flaky_failures_skipped: int = 0


@dataclass
class JiraConfig:
    email: str
    api_token: str
    dry_run: bool = True


@dataclass
class BlockerBugResult:
    component: str
    jira_component: str
    issue_key: str
    action: str  # "created", "updated", "skipped", "dry_run", "failed"
    message: str


# ---------------------------------------------------------------------------
# Component mapping
# ---------------------------------------------------------------------------


def load_component_mapping(path: Path | None = None) -> dict[str, ComponentMapping]:
    """Load the component-to-Jira mapping from YAML."""
    mapping_path = path or DEFAULT_MAPPING_PATH
    if not mapping_path.exists():
        raise FileNotFoundError(f"Component mapping file not found: {mapping_path}")

    with open(mapping_path) as f:
        data = yaml.safe_load(f)

    if not isinstance(data, dict):
        raise ValueError(f"Component mapping must be a YAML mapping: {mapping_path}")

    mappings = {}
    for name, fields in (data.get("components") or {}).items():
        mappings[name] = ComponentMapping(
            jira_component=fields.get("jira_component", ""),
        )
    return mappings


# ---------------------------------------------------------------------------
# Prow artifact fetching
# ---------------------------------------------------------------------------


PR_NUMBER_PATTERN = re.compile(r"/pull/[^/]+/(\d+)/")


def extract_pr_number(prow_url: str) -> str | None:
    """Extract the PR number from a Prow presubmit URL."""
    match = PR_NUMBER_PATTERN.search(prow_url)
    if match:
        return match.group(1)
    return None


def extract_gcs_path(prow_url: str) -> str | None:
    """Extract the GCS bucket path from a Prow or gcsweb URL."""
    for pattern in PROW_URL_PATTERNS:
        match = pattern.search(prow_url)
        if match:
            return match.group(1).rstrip("/")
    return None


def extract_job_as_name(gcs_path: str) -> str | None:
    """Extract the ci-operator 'as:' name from the GCS path.

    E.g. from 'pr-logs/pull/.../pull-ci-opendatahub-io-opendatahub-operator-main-opendatahub-operator-e2e/123'
    extracts 'opendatahub-operator-e2e'.
    """
    match = JOB_AS_NAME_PATTERN.search(gcs_path)
    if match:
        return match.group(1)
    return None


def build_junit_url(gcs_path: str, job_as_name: str) -> str:
    """Build the full gcsweb URL to the JUnit XML file."""
    return f"{GCSWEB_BASE}/{gcs_path}/artifacts/{job_as_name}/e2e/artifacts/junit_report.xml"


def check_job_passed(gcs_path: str) -> bool | None:
    """Check finished.json to determine if the Prow job passed overall.

    Returns True if passed, False if failed, None if finished.json
    is unavailable.
    """
    url = f"{GCSWEB_BASE}/{gcs_path}/finished.json"
    try:
        resp = requests.get(url, timeout=15)
        if resp.status_code == 200:
            data = resp.json()
            passed = data.get("passed", None)
            result = data.get("result", "UNKNOWN")
            log.info("Prow job result: %s (passed=%s)", result, passed)
            return passed
    except (requests.RequestException, ValueError) as e:
        log.warning("Could not fetch finished.json: %s", e)
    return None


def fetch_junit_xml(url: str, max_retries: int = 3, retry_delay: float = 10.0) -> str | None:
    """Fetch the JUnit XML content from gcsweb, with retries."""
    for attempt in range(1, max_retries + 1):
        try:
            resp = requests.get(url, timeout=30)
            if resp.status_code == 200 and "<testsuite" in resp.text:
                log.info("Fetched JUnit XML from %s (attempt %d)", url, attempt)
                return resp.text
            if resp.status_code == 200 and "<testsuite" not in resp.text:
                log.info(
                    "URL returned 200 but response is not JUnit XML (likely directory listing). "
                    "JUnit XML does not exist at %s", url,
                )
                return None
            if resp.status_code == 404:
                if attempt < max_retries:
                    log.warning(
                        "JUnit XML not found (404) on attempt %d/%d, retrying in %.0fs...",
                        attempt, max_retries, retry_delay,
                    )
                    time.sleep(retry_delay)
                    continue
                log.warning("JUnit XML not found at %s after %d attempts", url, max_retries)
                return None
            log.warning(
                "Unexpected response %d from %s on attempt %d",
                resp.status_code, url, attempt,
            )
        except requests.RequestException as e:
            log.warning("Request failed on attempt %d: %s", attempt, e)
        if attempt < max_retries:
            time.sleep(retry_delay)
    return None


# ---------------------------------------------------------------------------
# JUnit XML parsing
# ---------------------------------------------------------------------------


def parse_classification(testcase: Element) -> FailureClassification | None:
    """Extract failure classification properties from a JUnit test case."""
    props_elem = testcase.find("properties")
    if props_elem is None:
        return None

    props = {}
    for prop in props_elem.findall("property"):
        name = prop.get("name", "")
        if name.startswith("failure."):
            props[name] = prop.get("value", "")

    if not props:
        return None

    return FailureClassification(
        category=props.get("failure.category", ""),
        subcategory=props.get("failure.subcategory", ""),
        confidence=props.get("failure.confidence", ""),
        evidence=props.get("failure.evidence", ""),
    )


def extract_component(test_name: str) -> str | None:
    """Extract the component name from a test path.

    Returns the component name for paths like:
        TestOdhOperator/components/group_1/dashboard/Validate_component_enabled
    Returns None for non-component tests or parent-level entries.
    """
    match = COMPONENT_TEST_PATTERN.match(test_name)
    if match:
        return match.group(1)
    return None


def analyze_junit(xml_content: str) -> tuple[list[ComponentFailure], int, int]:
    """Parse JUnit XML and identify component-level failures.

    Any component test failure is actionable regardless of test-retry's
    classification. The only true infrastructure failures are when the Prow
    job dies before the test suite starts (no JUnit XML produced), which is
    handled before this function is called.

    Flaky tests (tests that failed on one attempt but passed on a retry) are
    excluded. test-retry records both the failed and passed attempts as
    separate <testcase> entries with the same name. A test is only considered
    truly failed if it has no passing entry.

    Returns:
        - List of actionable component failures (deduplicated by component)
        - Count of skipped non-component failures
        - Count of skipped flaky failures
    """
    root = ET.fromstring(xml_content)

    passed_tests: set[str] = set()
    for testcase in root.findall(".//testcase"):
        if testcase.find("failure") is None:
            passed_tests.add(testcase.get("name", ""))

    component_failures: list[ComponentFailure] = []
    non_component_skipped = 0
    flaky_skipped = 0

    seen_components: dict[str, ComponentFailure] = {}

    for testcase in root.findall(".//testcase"):
        failure_elem = testcase.find("failure")
        if failure_elem is None:
            continue

        test_name = testcase.get("name", "")
        failure_msg = failure_elem.get("message", "")

        if test_name in passed_tests:
            flaky_skipped += 1
            log.debug("Skipping flaky test (passed on retry): %s", test_name)
            continue

        component = extract_component(test_name)
        if component is None:
            non_component_skipped += 1
            continue

        classification = parse_classification(testcase)

        if component not in seen_components:
            cf = ComponentFailure(
                component=component,
                test_name=test_name,
                failure_message=failure_msg[:500],
                classification=classification or FailureClassification(),
            )
            seen_components[component] = cf
            component_failures.append(cf)
            cat_info = ""
            if classification:
                cat_info = f" ({classification.category}/{classification.subcategory})"
            log.info("Actionable failure: component=%s test=%s%s", component, test_name, cat_info)
        else:
            log.debug(
                "Additional failure for already-identified component %s: %s",
                component, test_name,
            )

    return component_failures, non_component_skipped, flaky_skipped


# ---------------------------------------------------------------------------
# Makefile VERSION parsing
# ---------------------------------------------------------------------------


def parse_makefile_version(makefile_path: Path) -> str | None:
    """Parse the VERSION value from the Makefile.

    Searches for lines like 'VERSION = 3.5.0' or 'VERSION ?= 3.6.0-ea.1'.
    Returns the first match found.
    """
    if not makefile_path.exists():
        log.warning("Makefile not found at %s", makefile_path)
        return None

    content = makefile_path.read_text()
    matches = VERSION_PATTERN.findall(content)
    if not matches:
        log.warning("No VERSION found in Makefile")
        return None

    version = matches[0]
    log.info("Parsed VERSION from Makefile: %s", version)
    return version


def version_to_affects_version(version: str) -> str | None:
    """Convert a semver VERSION to Jira Affects Version format.

    Examples:
        '3.5.0'      -> '3.5 GA RHOAI RELEASE'
        '3.6.0-ea.1' -> '3.6 EA1 RHOAI RELEASE'
        '3.6.0-ea.2' -> '3.6 EA2 RHOAI RELEASE'
    """
    ea_match = EA_PATTERN.match(version)
    if ea_match:
        major_minor = ea_match.group(1)
        ea_num = ea_match.group(2)
        return f"{major_minor} EA{ea_num} RHOAI RELEASE"

    ga_match = GA_PATTERN.match(version)
    if ga_match:
        major_minor = ga_match.group(1)
        return f"{major_minor} GA RHOAI RELEASE"

    log.warning("Could not parse VERSION '%s' into affects version format", version)
    return None


# ---------------------------------------------------------------------------
# Jira client
# ---------------------------------------------------------------------------


class JiraClient:
    """Jira REST API client for blocker bug operations."""

    def __init__(self, config: JiraConfig):
        self.config = config
        self.session = requests.Session()
        self.session.auth = (config.email, config.api_token)
        self.session.headers.update({
            "Content-Type": "application/json",
            "Accept": "application/json",
        })

    def _api_url(self, path: str) -> str:
        return f"{JIRA_BASE_URL}/rest/api/3/{path.lstrip('/')}"

    def search_open_blockers(self, jira_component: str) -> list[dict] | None:
        """Search for existing open blocker bugs for a component.

        This is a read-only operation and runs regardless of dry_run.
        Returns a list of issues on success, or None on failure (so the
        caller can distinguish "no results" from "search failed").
        """
        escaped_component = jira_component.replace("\\", "\\\\").replace('"', '\\"')
        jql = (
            f'project = {JIRA_PROJECT_KEY} '
            f'AND type = Bug '
            f'AND resolution = Unresolved '
            f'AND labels = "{AUTO_BLOCKER_LABEL}" '
            f'AND component = "{escaped_component}"'
        )
        log.info("Searching for existing blockers: %s", jql)

        try:
            resp = self.session.post(
                self._api_url("search/jql"),
                json={"jql": jql, "maxResults": 5, "fields": ["key", "summary", "status"]},
                timeout=30,
            )
            resp.raise_for_status()
            data = resp.json()
            issues = data.get("issues", [])
            log.info("Found %d existing blocker(s) for component '%s'", len(issues), jira_component)
            return issues
        except requests.RequestException as e:
            log.error("Failed to search Jira: %s", e)
            return None

    def create_blocker_bug(
        self,
        jira_component: str,
        component_name: str,
        test_name: str,
        failure_message: str,
        prow_url: str,
        pr_number: str,
        context: str,
        affects_version: str | None,
    ) -> str | None:
        """Create a new blocker bug by cloning the template.

        Returns the new issue key, or None if dry_run or on failure.
        """
        summary = f"[Auto] E2E blocker: {component_name} tests failing"

        description = {
            "type": "doc",
            "version": 1,
            "content": [
                {
                    "type": "paragraph",
                    "content": [
                        {"type": "text", "text": "Automated E2E blocker bug filed by the e2e-failure-triage automation.", "marks": [{"type": "strong"}]},
                    ],
                },
                {
                    "type": "paragraph",
                    "content": [
                        {"type": "text", "text": f"Component: {component_name}"},
                    ],
                },
                {
                    "type": "paragraph",
                    "content": [
                        {"type": "text", "text": f"Jira Component: {jira_component}"},
                    ],
                },
                {
                    "type": "paragraph",
                    "content": [
                        {"type": "text", "text": "PR: "},
                        {"type": "text", "text": f"#{pr_number}", "marks": [{"type": "link", "attrs": {"href": f"https://github.com/opendatahub-io/opendatahub-operator/pull/{pr_number}"}}]},
                    ],
                },
                {
                    "type": "paragraph",
                    "content": [
                        {"type": "text", "text": "Prow run: "},
                        {"type": "text", "text": "View logs", "marks": [{"type": "link", "attrs": {"href": prow_url}}]},
                    ],
                },
                {
                    "type": "paragraph",
                    "content": [
                        {"type": "text", "text": f"E2E suite: {context}"},
                    ],
                },
                {
                    "type": "paragraph",
                    "content": [
                        {"type": "text", "text": f"Failed test: {test_name}"},
                    ],
                },
                {
                    "type": "paragraph",
                    "content": [
                        {"type": "text", "text": f"Failure message: {failure_message[:1000]}"},
                    ],
                },
            ],
        }

        issue_data = {
            "fields": {
                "project": {"key": JIRA_PROJECT_KEY},
                "issuetype": {"name": "Bug"},
                "summary": summary,
                "description": description,
                "priority": {"name": "Blocker"},
                "components": [{"name": jira_component}],
                "labels": [AUTO_BLOCKER_LABEL],
            }
        }

        if affects_version:
            issue_data["fields"]["versions"] = [{"name": affects_version}]

        if self.config.dry_run:
            log.info("[DRY RUN] Would create blocker bug:")
            log.info("[DRY RUN]   Summary: %s", summary)
            log.info("[DRY RUN]   Component: %s", jira_component)
            log.info("[DRY RUN]   Labels: %s", [AUTO_BLOCKER_LABEL])
            log.info("[DRY RUN]   Affects Version: %s", affects_version)
            log.info("[DRY RUN]   PR: #%s", pr_number)
            log.info("[DRY RUN]   Prow URL: %s", prow_url)
            return None

        try:
            resp = self.session.post(
                self._api_url("issue"),
                data=json.dumps(issue_data),
                timeout=30,
            )
            resp.raise_for_status()
            new_key = resp.json().get("key")
            log.info("Created blocker bug: %s", new_key)
            return new_key
        except requests.RequestException as e:
            log.error("Failed to create blocker bug: %s", e)
            if hasattr(e, "response") and e.response is not None:
                log.error("Response: %s", e.response.text[:500])
            return None

    def create_clone_link(self, new_issue_key: str) -> bool:
        """Create a 'Cloners' link between the new issue and the template."""
        link_data = {
            "type": {"name": "Cloners"},
            "inwardIssue": {"key": new_issue_key},
            "outwardIssue": {"key": TEMPLATE_ISSUE_KEY},
        }

        if self.config.dry_run:
            log.info("[DRY RUN] Would create clone link: %s clones %s", new_issue_key, TEMPLATE_ISSUE_KEY)
            return True

        try:
            resp = self.session.post(
                self._api_url("issueLink"),
                data=json.dumps(link_data),
                timeout=30,
            )
            resp.raise_for_status()
            log.info("Created clone link: %s clones %s", new_issue_key, TEMPLATE_ISSUE_KEY)
            return True
        except requests.RequestException as e:
            log.warning("Failed to create clone link for %s: %s", new_issue_key, e)
            return False

    def add_comment(self, issue_key: str, prow_url: str, pr_number: str, context: str) -> bool:
        """Add a comment to an existing blocker bug noting a repeated failure."""
        comment_body = {
            "type": "doc",
            "version": 1,
            "content": [
                {
                    "type": "paragraph",
                    "content": [
                        {"type": "text", "text": "This component is still failing in E2E tests.", "marks": [{"type": "strong"}]},
                    ],
                },
                {
                    "type": "paragraph",
                    "content": [
                        {"type": "text", "text": "PR: "},
                        {"type": "text", "text": f"#{pr_number}", "marks": [{"type": "link", "attrs": {"href": f"https://github.com/opendatahub-io/opendatahub-operator/pull/{pr_number}"}}]},
                    ],
                },
                {
                    "type": "paragraph",
                    "content": [
                        {"type": "text", "text": "Prow run: "},
                        {"type": "text", "text": "View logs", "marks": [{"type": "link", "attrs": {"href": prow_url}}]},
                    ],
                },
                {
                    "type": "paragraph",
                    "content": [
                        {"type": "text", "text": f"E2E suite: {context}"},
                    ],
                },
            ],
        }

        if self.config.dry_run:
            log.info("[DRY RUN] Would add comment to %s: still failing on PR #%s", issue_key, pr_number)
            return True

        try:
            resp = self.session.post(
                self._api_url(f"issue/{issue_key}/comment"),
                data=json.dumps({"body": comment_body}),
                timeout=30,
            )
            resp.raise_for_status()
            log.info("Added comment to %s", issue_key)
            return True
        except requests.RequestException as e:
            log.error("Failed to add comment to %s: %s", issue_key, e)
            return False


# ---------------------------------------------------------------------------
# Jira orchestration
# ---------------------------------------------------------------------------


def process_component_failures(
    jira_client: JiraClient,
    component_failures: list[ComponentFailure],
    component_mapping: dict[str, ComponentMapping],
    prow_url: str,
    pr_number: str,
    context: str,
    affects_version: str | None,
) -> list[BlockerBugResult]:
    """Process all component failures: dedup, create, or update blocker bugs."""
    results: list[BlockerBugResult] = []
    handled: dict[str, str] = {}

    for cf in component_failures:
        mapping = component_mapping.get(cf.component)
        if not mapping or not mapping.jira_component:
            results.append(BlockerBugResult(
                component=cf.component,
                jira_component="",
                issue_key="",
                action="skipped",
                message=f"Component '{cf.component}' not mapped in component-mapping.yaml",
            ))
            continue

        jira_component = mapping.jira_component

        if jira_component in handled:
            results.append(BlockerBugResult(
                component=cf.component,
                jira_component=jira_component,
                issue_key=handled[jira_component],
                action="skipped",
                message=f"Already handled '{jira_component}' in this run",
            ))
            continue

        existing = jira_client.search_open_blockers(jira_component)
        if existing is None:
            results.append(BlockerBugResult(
                component=cf.component,
                jira_component=jira_component,
                issue_key="",
                action="skipped",
                message=f"Jira search failed for '{jira_component}', skipping to avoid duplicates",
            ))
            continue
        if existing:
            existing_key = existing[0]["key"]
            jira_client.add_comment(existing_key, prow_url, pr_number, context)
            handled[jira_component] = existing_key
            results.append(BlockerBugResult(
                component=cf.component,
                jira_component=jira_component,
                issue_key=existing_key,
                action="updated",
                message=f"Existing blocker {existing_key} updated with comment",
            ))
        else:
            new_key = jira_client.create_blocker_bug(
                jira_component=jira_component,
                component_name=cf.component,
                test_name=cf.test_name,
                failure_message=cf.failure_message,
                prow_url=prow_url,
                pr_number=pr_number,
                context=context,
                affects_version=affects_version,
            )
            if new_key:
                jira_client.create_clone_link(new_key)
                handled[jira_component] = new_key
                results.append(BlockerBugResult(
                    component=cf.component,
                    jira_component=jira_component,
                    issue_key=new_key,
                    action="created",
                    message=f"New blocker {new_key} created",
                ))
            else:
                action = "dry_run" if jira_client.config.dry_run else "failed"
                handled[jira_component] = ""
                results.append(BlockerBugResult(
                    component=cf.component,
                    jira_component=jira_component,
                    issue_key="",
                    action=action,
                    message=f"Would create blocker for {jira_component}" if jira_client.config.dry_run else "Failed to create blocker",
                ))

    return results


# ---------------------------------------------------------------------------
# Triage pipeline
# ---------------------------------------------------------------------------


def run_triage(args: argparse.Namespace) -> TriageResult:
    """Main triage logic: fetch artifacts, parse JUnit, identify failures."""
    component_mapping = load_component_mapping()

    pr_number = extract_pr_number(args.prow_url)
    if pr_number is None:
        raise ValueError(f"Could not extract PR number from Prow URL: {args.prow_url}")

    gcs_path = extract_gcs_path(args.prow_url)
    if gcs_path is None:
        raise ValueError(f"Could not extract GCS path from Prow URL: {args.prow_url}")

    job_as_name = extract_job_as_name(gcs_path)
    if job_as_name is None:
        raise ValueError(f"Could not extract job-as-name from GCS path: {gcs_path}")

    context = job_as_name  # e.g. "opendatahub-operator-e2e" or "opendatahub-operator-rhoai-e2e"

    result = TriageResult(
        prow_url=args.prow_url,
        pr_number=pr_number,
        context=context,
        component_mapping=component_mapping,
    )

    log.info("GCS path: %s", gcs_path)
    log.info("Job as-name: %s", job_as_name)

    job_passed = check_job_passed(gcs_path)
    if job_passed is True:
        log.info("Prow job passed overall — no blocker bugs to file.")
        result.junit_found = True
        return result

    junit_url = build_junit_url(gcs_path, job_as_name)
    log.info("Fetching JUnit XML from: %s", junit_url)

    xml_content = fetch_junit_xml(junit_url)
    if xml_content is None:
        log.warning(
            "No JUnit XML found — Prow job likely failed before E2E tests ran "
            "(infrastructure failure). No component blockers to file."
        )
        result.junit_found = False
        return result

    result.junit_found = True

    try:
        component_failures, non_component_skipped, flaky_skipped = analyze_junit(xml_content)
    except ET.ParseError as e:
        log.warning("JUnit XML is malformed (%s) — treating as no usable test data.", e)
        result.junit_found = False
        return result

    result.component_failures = component_failures
    result.non_component_failures_skipped = non_component_skipped
    result.flaky_failures_skipped = flaky_skipped

    return result


def print_summary(result: TriageResult) -> None:
    """Print a human-readable summary of the triage result."""
    print("\n" + "=" * 60)
    print("E2E FAILURE TRIAGE SUMMARY")
    print("=" * 60)
    print(f"Prow URL:  {result.prow_url}")
    print(f"PR:        #{result.pr_number}")
    print(f"Context:   {result.context}")
    print(f"JUnit XML: {'Found' if result.junit_found else 'NOT FOUND (infra failure before tests ran)'}")

    if not result.junit_found:
        print("\nNo test data to analyze. Exiting.")
        print("=" * 60)
        return

    print(f"\nFailures skipped (flaky/passed on retry): {result.flaky_failures_skipped}")
    print(f"Failures skipped (non-component):         {result.non_component_failures_skipped}")

    if not result.component_failures:
        print("\nNo actionable component failures found.")
    else:
        print(f"\nActionable component failures: {len(result.component_failures)}")
        unmapped = []
        for cf in result.component_failures:
            mapping = result.component_mapping.get(cf.component)
            print(f"\n  Component:      {cf.component}")
            print(f"  Test:           {cf.test_name}")
            if mapping and mapping.jira_component:
                print(f"  Jira component: {mapping.jira_component}")
            else:
                print("  Jira component: NOT MAPPED")
                unmapped.append(cf.component)
            print(f"  Message:        {cf.failure_message[:200]}")

        if unmapped:
            print(f"\n  WARNING: {len(unmapped)} component(s) not mapped in component-mapping.yaml: {', '.join(unmapped)}")
            print("  Blocker bugs cannot be filed for unmapped components.")

    print("=" * 60)


# ---------------------------------------------------------------------------
# PR comment generation
# ---------------------------------------------------------------------------


SECTION_START_MARKER = "<!-- e2e-triage-section:{suite} -->"
SECTION_END_MARKER = "<!-- /e2e-triage-section:{suite} -->"

# The thollander/actions-comment-pull-request action appends its own tracking
# marker (e.g. `<!-- thollander/actions-comment-pull-request "e2e-failure-triage" -->`)
# to the comment body on every run. Since we carry forward the previous
# comment's raw text when merging suite sections, that marker would otherwise
# accumulate one extra line per run. The action re-appends a fresh marker
# itself whenever it (re)creates the comment, so it's safe to strip any
# existing occurrences before merging.
THOLLANDER_MARKER_PATTERN = re.compile(
    r'[ \t]*<!-- thollander/actions-comment-pull-request "[^"]*" -->[ \t]*\n?'
)


def strip_thollander_markers(text: str) -> str:
    """Remove stale thollander/actions-comment-pull-request tracking markers."""
    stripped = THOLLANDER_MARKER_PATTERN.sub("", text)
    return re.sub(r"\n{3,}", "\n\n", stripped).rstrip("\n")


def _generate_section(
    result: TriageResult,
    bug_results: list[BlockerBugResult] | None,
    action_run_url: str | None = None,
) -> str:
    """Generate the content for one suite's section."""
    lines: list[str] = []

    if not result.junit_found:
        lines.append("Prow job failed before E2E tests ran (infrastructure).")
        lines.append("No component blocker bugs filed.")
    elif not result.component_failures:
        lines.append("No actionable component test failures detected.")
        if result.flaky_failures_skipped > 0:
            lines.append(f"({result.flaky_failures_skipped} flaky test(s) passed on retry)")
    elif bug_results:
        for br in bug_results:
            if br.action == "created" and br.issue_key:
                lines.append(
                    f"- **{br.component}**: [{br.issue_key}]"
                    f"(https://redhat.atlassian.net/browse/{br.issue_key}) (new)"
                )
            elif br.action == "updated" and br.issue_key:
                lines.append(
                    f"- **{br.component}**: [{br.issue_key}]"
                    f"(https://redhat.atlassian.net/browse/{br.issue_key}) (existing, comment added)"
                )
            elif br.action == "dry_run":
                lines.append(f"- **{br.component}**: would file blocker for {br.jira_component} (dry run)")
            elif br.action == "skipped":
                lines.append(f"- **{br.component}**: skipped ({br.message})")
            elif br.action == "failed":
                lines.append(f"- **{br.component}**: failed to file blocker ({br.message})")
    else:
        for cf in result.component_failures:
            mapping = result.component_mapping.get(cf.component)
            jira_comp = mapping.jira_component if mapping else "unmapped"
            lines.append(f"- **{cf.component}** ({jira_comp}): `{cf.test_name}`")

    lines.append("")
    links = f"[Prow run]({result.prow_url})"
    if action_run_url:
        links += f" | [Triage action]({action_run_url})"
    lines.append(links)

    return "\n".join(lines)


def generate_pr_comment(
    result: TriageResult,
    bug_results: list[BlockerBugResult] | None,
    action_run_url: str | None = None,
    existing_comment: str | None = None,
) -> str:
    """Generate a combined PR comment with sections for each E2E suite.

    Each suite's results are wrapped in HTML comment markers so subsequent
    runs can update their section without overwriting the other suite's results.
    """
    suite = result.context
    section_content = _generate_section(result, bug_results, action_run_url)

    start_marker = SECTION_START_MARKER.format(suite=suite)
    end_marker = SECTION_END_MARKER.format(suite=suite)

    new_section = f"{start_marker}\n### `{suite}`\n{section_content}\n{end_marker}"

    if existing_comment and start_marker in existing_comment:
        pattern = re.compile(
            re.escape(start_marker) + r".*?" + re.escape(end_marker),
            re.DOTALL,
        )
        combined = pattern.sub(new_section, existing_comment)
        return combined

    if existing_comment and "## E2E Failure Triage" in existing_comment:
        return existing_comment.rstrip() + "\n\n" + new_section

    return f"## E2E Failure Triage\n\n{new_section}"


# ---------------------------------------------------------------------------
# CLI entrypoint
# ---------------------------------------------------------------------------


def main() -> None:
    parser = argparse.ArgumentParser(
        description="Triage E2E test failures from Prow CI runs",
    )
    parser.add_argument(
        "--prow-url",
        required=True,
        help="Prow job URL (from github.event.target_url)",
    )
    parser.add_argument(
        "--comment-output",
        default=None,
        help="Path to write the PR comment markdown body (for GHA to post)",
    )
    parser.add_argument(
        "--existing-comment",
        default=None,
        help="Path to file containing the existing PR comment body (for merging sections)",
    )
    parser.add_argument(
        "--action-run-url",
        default=os.environ.get("GITHUB_ACTION_RUN_URL", ""),
        help="URL of the GitHub Actions run (for linking in PR comments)",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        default=False,
        help="Run analysis only, skip Jira and PR comment actions",
    )
    parser.add_argument(
        "--verbose",
        action="store_true",
        default=False,
        help="Enable debug logging",
    )

    args = parser.parse_args()

    logging.basicConfig(
        level=logging.DEBUG if args.verbose else logging.INFO,
        format="%(asctime)s %(levelname)s %(name)s: %(message)s",
    )

    result = run_triage(args)
    print_summary(result)

    action_run_url = args.action_run_url or None
    existing_comment = None
    if args.existing_comment and Path(args.existing_comment).exists():
        existing_comment = strip_thollander_markers(Path(args.existing_comment).read_text())

    if not result.component_failures:
        comment_body = generate_pr_comment(result, None, action_run_url, existing_comment)
        if args.comment_output:
            Path(args.comment_output).write_text(comment_body)
            log.info("PR comment written to %s", args.comment_output)
        return

    makefile_path = SCRIPT_DIR.parent.parent.parent / "Makefile"
    version = parse_makefile_version(makefile_path)
    affects_version = version_to_affects_version(version) if version else None

    print(f"\nMakefile VERSION: {version}")
    print(f"Affects Version:  {affects_version}")

    jira_email = os.environ.get("E2E_TRIAGE_JIRA_EMAIL", "")
    jira_token = os.environ.get("E2E_TRIAGE_JIRA_API_TOKEN", "")

    if not args.dry_run and (not jira_email or not jira_token):
        raise ValueError("Jira credentials required for non-dry-run mode. Set E2E_TRIAGE_JIRA_EMAIL and E2E_TRIAGE_JIRA_API_TOKEN.")

    jira_config = JiraConfig(
        email=jira_email,
        api_token=jira_token,
        dry_run=args.dry_run,
    )

    jira_client = JiraClient(jira_config)

    bug_results = process_component_failures(
        jira_client=jira_client,
        component_failures=result.component_failures,
        component_mapping=result.component_mapping,
        prow_url=result.prow_url,
        pr_number=result.pr_number,
        context=result.context,
        affects_version=affects_version,
    )

    print("\n" + "=" * 60)
    print("JIRA BLOCKER BUG RESULTS")
    print("=" * 60)
    for br in bug_results:
        print(f"\n  Component:      {br.component}")
        print(f"  Jira component: {br.jira_component}")
        print(f"  Action:         {br.action}")
        print(f"  Issue key:      {br.issue_key or 'N/A'}")
        print(f"  Message:        {br.message}")
    print("=" * 60)

    comment_body = generate_pr_comment(result, bug_results, action_run_url, existing_comment)
    if args.comment_output:
        Path(args.comment_output).write_text(comment_body)
        log.info("PR comment written to %s", args.comment_output)


if __name__ == "__main__":
    try:
        main()
    except (ValueError, FileNotFoundError) as e:
        log.error("%s", e)
        sys.exit(1)
