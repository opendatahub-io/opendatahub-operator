#!/usr/bin/env python3
"""
Comprehensive Security Report Generator

Parses outputs from all security scanning tools and generates a detailed
markdown report suitable for security review and JIRA attachments.

Usage:
    python .github/scripts/generate-security-report.py --output security-report.md

Tools parsed:
    - Gitleaks (JSON)
    - TruffleHog (JSON)
    - Semgrep (SARIF)
    - Checkov (JSON) - if available
    - OSV Scanner (JSON) - if available
    - ShellCheck (JSON)
    - Hadolint (SARIF)
    - yamllint (text)
    - kube-linter (JSON)
    - RBAC Analyzer (text)
"""

import json
import os
import sys
import argparse
import re
import hashlib
from datetime import datetime
from pathlib import Path
from typing import Dict, List, Any, Optional


# Report Format Version: 1.0
# Breaking changes require updating .github/workflows/security-full-scan.yml badge parsing
class SecurityReportGenerator:
    def __init__(self, workspace: str, github_context: Dict[str, str], yamllint_limit: int = 50):
        self.workspace = Path(workspace)
        self.github = github_context
        self.yamllint_limit = yamllint_limit
        self.findings = {
            'critical': [],
            'high': [],
            'medium': [],
            'low': [],
            'info': []
        }
        self.tool_stats = {}

    def parse_gitleaks(self, filepath: str) -> Dict[str, Any]:
        """Parse Gitleaks JSON output"""
        stats = {'tool': 'Gitleaks', 'findings': 0, 'status': '✅ PASS'}

        if not Path(filepath).exists():
            stats['status'] = '⏭️ SKIPPED'
            return stats

        try:
            with open(filepath) as f:
                data = json.load(f)
                if data:
                    # Deduplicate findings by file:line:rule combination
                    seen = set()
                    unique_findings = []

                    for finding in data:
                        # Strip /repo/ prefix from Docker container mount path
                        file_path = finding.get('File', 'unknown')
                        if file_path.startswith('/repo/'):
                            file_path = file_path[6:]  # Remove '/repo/' prefix

                        # Normalize path using os.path.normpath for robust handling
                        file_path = os.path.normpath(file_path).lstrip('/')
                        # Ensure no leading path traversal after normalization
                        if file_path.startswith('..'):
                            file_path = file_path.lstrip('./')

                        # Include description hash to differentiate multiple secrets at same location
                        description = finding.get('Description', 'Secret detected')
                        desc_hash = hashlib.sha256(description.encode()).hexdigest()[:8]
                        dedup_key = f"{file_path}:{finding.get('StartLine', '?')}:{finding.get('RuleID', 'unknown')}:{desc_hash}"

                        if dedup_key not in seen:
                            seen.add(dedup_key)
                            unique_findings.append({
                                'tool': 'Gitleaks',
                                'type': 'Hardcoded Secret',
                                'severity': 'CRITICAL',
                                'file': file_path,
                                'line': finding.get('StartLine', '?'),
                                'rule': finding.get('RuleID', 'unknown'),
                                'description': finding.get(
                                    'Description',
                                    'Secret detected; see Gitleaks JSON artifact for details (value redacted)'
                                ),
                                'remediation': 'Remove secret from code, rotate credential, use secret manager'
                            })

                    self.findings['critical'].extend(unique_findings)
                    stats['findings'] = len(unique_findings)
                    if unique_findings:
                        stats['status'] = '❌ FINDINGS'

        except Exception as e:
            stats['status'] = '⚠️ ERROR: Failed to parse Gitleaks output'
            print(f"[ERROR] Gitleaks parser: {str(e)}", file=sys.stderr)

        return stats

    def parse_trufflehog(self, filepath: str) -> Dict[str, Any]:
        """Parse TruffleHog JSON output"""
        stats = {'tool': 'TruffleHog', 'findings': 0, 'status': '✅ PASS'}

        if not Path(filepath).exists():
            stats['status'] = '⏭️ SKIPPED'
            return stats

        try:
            findings_count = 0
            with open(filepath) as f:
                for line in f:
                    if line.strip():
                        finding = json.loads(line)
                        findings_count += 1

                        self.findings['critical'].append({
                            'tool': 'TruffleHog',
                            'type': 'Verified Credential',
                            'severity': 'CRITICAL',
                            'file': finding.get('SourceMetadata', {}).get('Data', {}).get('Filesystem', {}).get('file', 'unknown'),
                            'line': '?',
                            'rule': finding.get('DetectorName', 'unknown'),
                            'description': f"Verified {finding.get('DetectorName', 'credential')} found",
                            'remediation': 'URGENT: Rotate this credential immediately - it has been verified as active'
                        })

            stats['findings'] = findings_count
            if findings_count > 0:
                stats['status'] = '❌ FINDINGS'

        except Exception as e:
            stats['status'] = '⚠️ ERROR: Failed to parse TruffleHog output'
            print(f"[ERROR] TruffleHog parser: {str(e)}", file=sys.stderr)

        return stats

    def parse_semgrep_sarif(self, filepath: str) -> Dict[str, Any]:
        """Parse Semgrep SARIF output"""
        stats = {'tool': 'Semgrep', 'findings': 0, 'status': '✅ PASS'}

        if not Path(filepath).exists():
            stats['status'] = '⏭️ SKIPPED'
            return stats

        try:
            with open(filepath) as f:
                sarif = json.load(f)

            for run in sarif.get('runs', []):
                for result in run.get('results', []):
                    stats['findings'] += 1

                    level = result.get('level', 'note')
                    severity_map = {
                        'error': 'high',
                        'warning': 'medium',
                        'note': 'info'
                    }
                    severity = severity_map.get(level, 'info')

                    rule = result.get('ruleId', 'unknown')
                    message = result.get('message', {}).get('text', 'No description')

                    location = result.get('locations', [{}])[0]
                    artifact = location.get('physicalLocation', {}).get('artifactLocation', {})
                    file_path = artifact.get('uri', 'unknown')

                    region = location.get('physicalLocation', {}).get('region', {})
                    line = region.get('startLine', '?')

                    self.findings[severity].append({
                        'tool': 'Semgrep',
                        'type': rule,
                        'severity': severity.upper(),
                        'file': file_path,
                        'line': line,
                        'rule': rule,
                        'description': message,
                        'remediation': self._get_semgrep_remediation(rule)
                    })

            if stats['findings'] > 0:
                stats['status'] = '❌ FINDINGS'

        except Exception as e:
            stats['status'] = '⚠️ ERROR: Failed to parse Semgrep SARIF output'
            print(f"[ERROR] Semgrep parser: {str(e)}", file=sys.stderr)

        return stats

    def parse_hadolint_sarif(self, filepath: str) -> Dict[str, Any]:
        """Parse Hadolint SARIF output"""
        stats = {'tool': 'Hadolint', 'findings': 0, 'status': '✅ PASS'}

        if not Path(filepath).exists():
            stats['status'] = '⏭️ SKIPPED'
            return stats

        try:
            with open(filepath) as f:
                sarif = json.load(f)

            for run in sarif.get('runs', []):
                for result in run.get('results', []):
                    stats['findings'] += 1

                    level = result.get('level', 'note')
                    severity_map = {
                        'error': 'high',
                        'warning': 'medium',
                        'note': 'low'
                    }
                    severity = severity_map.get(level, 'low')

                    rule = result.get('ruleId', 'unknown')
                    message = result.get('message', {}).get('text', 'No description')

                    location = result.get('locations', [{}])[0]
                    artifact = location.get('physicalLocation', {}).get('artifactLocation', {})
                    file_path = artifact.get('uri', 'unknown')

                    region = location.get('physicalLocation', {}).get('region', {})
                    line = region.get('startLine', '?')

                    self.findings[severity].append({
                        'tool': 'Hadolint',
                        'type': 'Dockerfile Issue',
                        'severity': severity.upper(),
                        'file': file_path,
                        'line': line,
                        'rule': rule,
                        'description': message,
                        'remediation': 'Follow Dockerfile best practices and CIS benchmarks'
                    })

            if stats['findings'] > 0:
                stats['status'] = '❌ FINDINGS'

        except Exception as e:
            stats['status'] = '⚠️ ERROR: Failed to parse Hadolint SARIF output'
            print(f"[ERROR] Hadolint parser: {str(e)}", file=sys.stderr)

        return stats

    def parse_shellcheck(self, filepath: str) -> Dict[str, Any]:
        """Parse ShellCheck JSON output"""
        stats = {'tool': 'ShellCheck', 'findings': 0, 'status': '✅ PASS'}

        if not Path(filepath).exists():
            stats['status'] = '⏭️ SKIPPED'
            return stats

        try:
            with open(filepath) as f:
                data = json.load(f)

            # ShellCheck outputs either a flat list (legacy) or {comments: [...]} (json1).
            # Support both formats robustly.
            if isinstance(data, list):
                findings_iter = data
            elif isinstance(data, dict):
                # Check for json1 format with 'comments' key, or fall back to iterating all values
                if 'comments' in data:
                    findings_iter = data['comments']
                else:
                    findings_iter = [item for v in data.values() if isinstance(v, list) for item in v]
            else:
                findings_iter = []

            for finding in findings_iter:
                stats['findings'] += 1

                level = finding.get('level', 'info')
                severity_map = {
                    'error': 'high',
                    'warning': 'medium',
                    'info': 'low',
                    'style': 'info'
                }
                severity = severity_map.get(level, 'low')

                self.findings[severity].append({
                    'tool': 'ShellCheck',
                    'type': 'Shell Script Issue',
                    'severity': severity.upper(),
                    'file': finding.get('file', 'unknown'),
                    'line': finding.get('line', '?'),
                    'rule': f"SC{finding.get('code', '????')}",
                    'description': finding.get('message', 'No description'),
                    'remediation': 'Follow ShellCheck recommendations for safe shell scripting'
                })

            if stats['findings'] > 0:
                stats['status'] = '❌ FINDINGS'

        except Exception as e:
            stats['status'] = '⚠️ ERROR: Failed to parse ShellCheck output'
            print(f"[ERROR] ShellCheck parser: {str(e)}", file=sys.stderr)

        return stats

    def parse_kubelinter(self, filepath: str) -> Dict[str, Any]:
        """Parse kube-linter JSON output

        kube-linter JSON format:
        {
          "Reports": [
            {
              "Object": {
                "K8sObject": {
                  "Namespace": "...",
                  "Name": "...",
                  "GroupVersionKind": {...}
                }
              },
              "Check": "check-name",
              "Diagnostic": {
                "Message": "...",
                "Description": "..."
              }
            }
          ]
        }
        """
        stats = {'tool': 'kube-linter', 'findings': 0, 'status': '✅ PASS'}

        if not Path(filepath).exists():
            stats['status'] = '⏭️ SKIPPED'
            return stats

        try:
            with open(filepath) as f:
                data = json.load(f)

            reports = data.get('Reports', [])
            if not reports:
                return stats

            # Deduplicate findings by check:object:message combination
            seen = set()
            unique_findings = []

            for report in reports:
                check_name = report.get('Check', 'unknown')
                diagnostic = report.get('Diagnostic', {})
                message = diagnostic.get('Message', 'kube-linter finding')
                description = diagnostic.get('Description', '')

                # Extract object information
                k8s_obj = report.get('Object', {}).get('K8sObject', {})
                namespace = k8s_obj.get('Namespace', '')
                name = k8s_obj.get('Name', 'unknown')
                gvk = k8s_obj.get('GroupVersionKind', {})
                kind = gvk.get('Kind', 'unknown')

                # Construct object identifier
                if namespace:
                    object_id = f"{kind}/{namespace}/{name}"
                else:
                    object_id = f"{kind}/{name}"

                # Deduplication key
                dedup_key = f"{check_name}:{object_id}:{message}"
                if dedup_key in seen:
                    continue
                seen.add(dedup_key)

                # Map check severity (kube-linter doesn't provide severity in JSON)
                # Critical: cluster-admin, privileged containers, host access
                # High: RBAC wildcards, secret access, missing probes
                # Medium: resource limits, namespace issues
                # Low: image tags, best practices
                critical_checks = {
                    'cluster-admin-role-binding', 'privileged-container',
                    'host-network', 'host-pid', 'host-ipc', 'docker-sock',
                    'access-to-create-pods', 'privilege-escalation-container'
                }
                high_checks = {
                    'access-to-secrets', 'wildcard-in-rules', 'sensitive-host-mounts',
                    'writable-host-mount', 'unsafe-proc-mount', 'unsafe-sysctls',
                    'default-service-account', 'env-var-secret', 'read-secret-from-env-var',
                    'drop-net-raw-capability'
                }
                medium_checks = {
                    'unset-cpu-requirements', 'unset-memory-requirements',
                    'no-liveness-probe', 'no-readiness-probe', 'use-namespace',
                    'non-isolated-pod', 'exposed-services', 'no-read-only-root-fs'
                }

                if check_name in critical_checks:
                    severity = 'CRITICAL'
                    severity_bucket = 'critical'
                elif check_name in high_checks:
                    severity = 'HIGH'
                    severity_bucket = 'high'
                elif check_name in medium_checks:
                    severity = 'MEDIUM'
                    severity_bucket = 'medium'
                else:
                    severity = 'LOW'
                    severity_bucket = 'low'

                finding = {
                    'tool': 'kube-linter',
                    'type': 'Kubernetes Manifest Security',
                    'severity': severity,
                    'file': object_id,  # Use object ID as "file" for display
                    'line': check_name,  # Use check name as "line" for display
                    'rule': check_name,
                    'description': f"{message} (Object: {object_id})",
                    'remediation': description or 'Fix Kubernetes manifest according to check requirements'
                }

                unique_findings.append(finding)
                self.findings[severity_bucket].append(finding)

            stats['findings'] = len(unique_findings)
            if stats['findings'] > 0:
                stats['status'] = '❌ FINDINGS'

        except Exception as e:
            stats['status'] = '⚠️ ERROR: Failed to parse kube-linter JSON'
            print(f"[ERROR] kube-linter parser: {str(e)}", file=sys.stderr)

        return stats

    def parse_rbac_analyzer(self, filepath: str) -> Dict[str, Any]:
        """Parse RBAC Analyzer text output"""
        stats = {'tool': 'RBAC Analyzer', 'findings': 0, 'status': '✅ PASS', 'content': ''}

        if not Path(filepath).exists():
            stats['status'] = '⏭️ SKIPPED'
            return stats

        try:
            with open(filepath) as f:
                content = f.read()
                stats['content'] = content

                # Count findings by matching actual RBAC analyzer heading format: "### CRITICAL (N findings)"
                # This prevents false matches in descriptions or remediation text
                critical_count = len(re.findall(r'(?m)^###\s+CRITICAL\b', content))
                high_count = len(re.findall(r'(?m)^###\s+HIGH\b', content))
                warning_count = len(re.findall(r'(?m)^###\s+WARNING\b', content))

                stats['findings'] = critical_count + high_count + warning_count

                if stats['findings'] > 0:
                    stats['status'] = '❌ FINDINGS'
                    stats['breakdown'] = {
                        'critical': critical_count,
                        'high': high_count,
                        'warning': warning_count
                    }

                    # Feed RBAC findings into global severity buckets for posture/summary
                    # This ensures RBAC privilege issues affect the overall security posture
                    for _ in range(critical_count):
                        self.findings['critical'].append({
                            'tool': 'RBAC Analyzer',
                            'type': 'RBAC Privilege Chain',
                            'severity': 'CRITICAL',
                            'file': 'rbac-analysis.md',
                            'line': '?',
                            'rule': 'RBAC_ANALYZER_CRITICAL',
                            'description': 'Critical RBAC privilege chain issue; see RBAC analysis section',
                            'remediation': 'Tighten roles/bindings; remove wildcard or dangerous verbs; apply least privilege'
                        })
                    for _ in range(high_count):
                        self.findings['high'].append({
                            'tool': 'RBAC Analyzer',
                            'type': 'RBAC Privilege Chain',
                            'severity': 'HIGH',
                            'file': 'rbac-analysis.md',
                            'line': '?',
                            'rule': 'RBAC_ANALYZER_HIGH',
                            'description': 'High-severity RBAC issue; see RBAC analysis section',
                            'remediation': 'Scope RBAC rules more narrowly; justify and document remaining access'
                        })
                    for _ in range(warning_count):
                        self.findings['medium'].append({
                            'tool': 'RBAC Analyzer',
                            'type': 'RBAC Privilege Chain',
                            'severity': 'MEDIUM',
                            'file': 'rbac-analysis.md',
                            'line': '?',
                            'rule': 'RBAC_ANALYZER_WARNING',
                            'description': 'RBAC warning; see RBAC analysis section',
                            'remediation': 'Review and tighten RBAC where feasible'
                        })

        except Exception as e:
            stats['status'] = '⚠️ ERROR: Failed to parse RBAC analyzer output'
            print(f"[ERROR] RBAC analyzer parser: {str(e)}", file=sys.stderr)

        return stats

    def parse_yamllint(self, filepath: str, max_findings: int = 50) -> Dict[str, Any]:
        """Parse yamllint parsable format output

        Args:
            filepath: Path to yamllint parsable output (file:line:col: [level] message (rule))
            max_findings: Maximum number of findings to include in report (default: 50)

        Format: file:line:column: [level] message (rule)
        Example: ./config/rbac/role.yaml:10:5: [error] line too long (120 > 80 characters) (line-length)
        """
        stats = {'tool': 'yamllint', 'findings': 0, 'status': '✅ PASS', 'findings_data': []}

        if not Path(filepath).exists():
            stats['status'] = '⏭️ SKIPPED'
            return stats

        try:
            with open(filepath) as f:
                lines = f.readlines()

            # Parse each line in parsable format
            # Pattern: filepath:line:column: [level] message (rule)
            pattern = r'^(.+?):(\d+):(\d+): \[(error|warning)\] (.+?) \(([^)]+)\)$'

            for line in lines:
                line = line.strip()
                if not line:
                    continue

                match = re.match(pattern, line)
                if not match:
                    # Skip lines that don't match expected format
                    continue

                file_path, line_num, col, level, message, rule = match.groups()
                stats['findings'] += 1

                # Store yamllint findings in separate list (not in main findings dict)
                # This prevents them from cluttering the security report
                stats['findings_data'].append({
                    'tool': 'yamllint',
                    'type': 'YAML Issue',
                    'level': level,
                    'file': file_path,
                    'line': int(line_num),
                    'rule': rule,
                    'description': message,
                })

            # Store all findings for dedicated report, but track truncation for comprehensive report
            stats['findings_data_all'] = stats['findings_data'].copy()  # Keep all for dedicated report
            if len(stats['findings_data']) > max_findings:
                stats['findings_data'] = stats['findings_data'][:max_findings]  # Limit for comprehensive report
                stats['truncated'] = True
                stats['total_findings'] = stats['findings']
            else:
                stats['truncated'] = False

            if stats['findings'] > 0:
                stats['status'] = '❌ FINDINGS'

        except Exception as e:
            stats['status'] = '⚠️ ERROR: Failed to parse yamllint output'
            print(f"[ERROR] yamllint parser: {str(e)}", file=sys.stderr)

        return stats

    def parse_actionlint(self, filepath: str) -> Dict[str, Any]:
        """Parse actionlint text output

        Format: <file>:<line>:<col>: <message> [<rule>]
        Example: .github/workflows/test.yml:10:5: invalid expression syntax [expression]
        """
        stats = {'tool': 'actionlint', 'findings': 0, 'status': '✅ PASS', 'findings_data': []}

        if not Path(filepath).exists():
            stats['status'] = '⏭️ SKIPPED'
            return stats

        try:
            with open(filepath) as f:
                content = f.read()

            # Pattern: filepath:line:col: message [rule]
            # actionlint uses this format for all findings
            pattern = r'^(.+?):(\d+):(\d+):\s+(.+?)(?:\s+\[(.+?)\])?$'

            for line in content.splitlines():
                if not line.strip():
                    continue

                match = re.match(pattern, line)
                if not match:
                    continue

                file_path, line_num, col, message, rule = match.groups()
                stats['findings'] += 1

                # Map severity based on message content
                # GitHub Actions security issues are generally MEDIUM (workflow errors can break CI/CD)
                severity = 'medium'
                severity_bucket = 'medium'

                # Upgrade to HIGH for security-related issues
                if any(keyword in message.lower() for keyword in ['permission', 'token', 'secret', 'credential']):
                    severity = 'HIGH'
                    severity_bucket = 'high'

                finding = {
                    'tool': 'actionlint',
                    'type': 'GitHub Actions Workflow Issue',
                    'severity': severity,
                    'file': file_path,
                    'line': int(line_num),
                    'rule': rule or 'workflow-syntax',
                    'description': message,
                    'remediation': 'Fix GitHub Actions workflow syntax according to actionlint recommendation'
                }

                stats['findings_data'].append(finding)
                self.findings[severity_bucket].append(finding)

            if stats['findings'] > 0:
                stats['status'] = '❌ FINDINGS'

        except Exception as e:
            stats['status'] = '⚠️ ERROR: Failed to parse actionlint output'
            print(f"[ERROR] actionlint parser: {str(e)}", file=sys.stderr)

        return stats

    def _get_semgrep_remediation(self, rule_id: str) -> str:
        """Get remediation guidance for Semgrep rules"""
        remediations = {
            'hardcoded-secret-generic': 'Remove hardcoded secret, use environment variables or secret manager',
            'rbac-wildcard-resources': 'Replace wildcard with specific resources following least privilege',
            'rbac-wildcard-verbs': 'Replace wildcard with specific verbs needed for operation',
            'rbac-dangerous-verbs': 'Remove dangerous verbs (escalate/impersonate/bind) or justify usage',
            'insecure-tls-skip-verify': 'Remove InsecureSkipVerify, properly configure certificate validation',
            'weak-crypto-md5': 'Replace MD5 with SHA-256 or stronger hash function',
            'weak-crypto-sha1': 'Replace SHA-1 with SHA-256 or stronger hash function',
            'operator-privileged-pod': 'Remove privileged: true, use specific capabilities if needed',
        }
        return remediations.get(rule_id, 'Follow security best practices for this finding')

    def generate_report(self, output_file: str, json_summary_file: Optional[str] = None, yamllint_report_file: Optional[str] = None):
        """Generate comprehensive markdown security report, optional JSON summary, and optional yamllint report"""

        # Parse all tool outputs
        self.tool_stats['gitleaks'] = self.parse_gitleaks(f'{self.workspace}/gitleaks.json')
        self.tool_stats['trufflehog'] = self.parse_trufflehog(f'{self.workspace}/trufflehog.json')
        self.tool_stats['semgrep'] = self.parse_semgrep_sarif(f'{self.workspace}/semgrep.sarif')
        self.tool_stats['hadolint'] = self.parse_hadolint_sarif(f'{self.workspace}/hadolint.sarif')
        self.tool_stats['shellcheck'] = self.parse_shellcheck(f'{self.workspace}/shellcheck.json')
        self.tool_stats['yamllint'] = self.parse_yamllint(f'{self.workspace}/yamllint.txt', max_findings=self.yamllint_limit)
        self.tool_stats['actionlint'] = self.parse_actionlint(f'{self.workspace}/actionlint.txt')
        self.tool_stats['kube-linter'] = self.parse_kubelinter(f'{self.workspace}/kube-linter.json')
        self.tool_stats['rbac'] = self.parse_rbac_analyzer(f'{self.workspace}/rbac-analysis.md')

        # Calculate totals
        total_findings = sum(len(findings) for findings in self.findings.values())
        critical_count = len(self.findings['critical'])
        high_count = len(self.findings['high'])
        medium_count = len(self.findings['medium'])
        low_count = len(self.findings['low'])

        # Determine overall security posture
        if critical_count > 0:
            posture = 'CRITICAL'
            posture_desc = 'Immediate action required - critical vulnerabilities detected'
        elif high_count > 0:
            posture = 'HIGH'
            posture_desc = 'High-severity issues detected - prompt review needed'
        elif medium_count > 0:
            posture = 'MEDIUM'
            posture_desc = 'Medium-severity issues detected - review recommended'
        elif low_count > 0:
            posture = 'LOW'
            posture_desc = 'Low-severity issues detected - minor improvements suggested'
        else:
            posture = 'CLEAN'
            posture_desc = 'No security issues detected'

        # Generate report
        try:
            with open(output_file, 'w') as f:
                # Header
                f.write(f"# Comprehensive Security Scan Report\n\n")
                f.write(f"**Generated:** {datetime.utcnow().strftime('%Y-%m-%d %H:%M:%S')} UTC\n\n")
                f.write(f"**Repository:** {self.github.get('repository', 'unknown')}\n\n")
                f.write(f"**Commit:** {self.github.get('sha', 'unknown')}\n\n")
                f.write(f"**Branch:** {self.github.get('ref_name', 'unknown')}\n\n")
                f.write(f"**Workflow Run:** {self.github.get('run_url', 'N/A')}\n\n")
                f.write(f"---\n\n")

                # Executive Summary
                f.write(f"## Executive Summary\n\n")
                f.write(f"**Security Posture:** {posture}\n\n")
                f.write(f"{posture_desc}\n\n")
                f.write(f"**Total Findings:** {total_findings}\n\n")
                f.write(f"- Critical: {critical_count}\n")
                f.write(f"- High: {high_count}\n")
                f.write(f"- Medium: {medium_count}\n")
                f.write(f"- Low: {low_count}\n")
                f.write(f"- Info: {len(self.findings['info'])}\n\n")
                f.write(f"---\n\n")

                # Tool Status Table
                f.write(f"## Scan Tool Status\n\n")
                f.write(f"| Tool | Purpose | Status | Findings |\n")
                f.write(f"|------|---------|--------|----------|\n")
                f.write(f"| {self.tool_stats['gitleaks']['tool']} | Pattern-based secret detection | {self.tool_stats['gitleaks']['status']} | {self.tool_stats['gitleaks']['findings']} |\n")
                f.write(f"| {self.tool_stats['trufflehog']['tool']} | Verified credential detection (800+ types) | {self.tool_stats['trufflehog']['status']} | {self.tool_stats['trufflehog']['findings']} |\n")
                f.write(f"| {self.tool_stats['semgrep']['tool']} | Custom security rules (27 operator-focused) | {self.tool_stats['semgrep']['status']} | {self.tool_stats['semgrep']['findings']} |\n")
                f.write(f"| {self.tool_stats['hadolint']['tool']} | Dockerfile best practices | {self.tool_stats['hadolint']['status']} | {self.tool_stats['hadolint']['findings']} |\n")
                f.write(f"| {self.tool_stats['shellcheck']['tool']} | Shell script security | {self.tool_stats['shellcheck']['status']} | {self.tool_stats['shellcheck']['findings']} |\n")
                f.write(f"| {self.tool_stats['yamllint']['tool']} | YAML syntax and style validation | {self.tool_stats['yamllint']['status']} | {self.tool_stats['yamllint']['findings']} |\n")
                f.write(f"| {self.tool_stats['actionlint']['tool']} | GitHub Actions workflow validation | {self.tool_stats['actionlint']['status']} | {self.tool_stats['actionlint']['findings']} |\n")
                f.write(f"| {self.tool_stats['rbac']['tool']} | RBAC privilege chain analysis | {self.tool_stats['rbac']['status']} | {self.tool_stats['rbac']['findings']} |\n\n")
                f.write(f"---\n\n")

                # Critical Findings
                if self.findings['critical']:
                    f.write(f"## Critical Findings ({len(self.findings['critical'])})\n\n")
                    f.write(f"**IMMEDIATE ACTION REQUIRED**\n\n")
                    for i, finding in enumerate(self.findings['critical'], 1):
                        f.write(f"### {i}. {finding['type']} ({finding['tool']})\n\n")
                        f.write(f"- **File:** `{finding['file']}:{finding['line']}`\n")
                        f.write(f"- **Rule:** {finding['rule']}\n")
                        f.write(f"- **Description:** {finding['description']}\n")
                        f.write(f"- **Remediation:** {finding['remediation']}\n\n")
                    f.write(f"---\n\n")

                # High Findings
                if self.findings['high']:
                    f.write(f"## High-Severity Findings ({len(self.findings['high'])})\n\n")
                    for i, finding in enumerate(self.findings['high'], 1):
                        f.write(f"### {i}. {finding['type']} ({finding['tool']})\n\n")
                        f.write(f"- **File:** `{finding['file']}:{finding['line']}`\n")
                        f.write(f"- **Rule:** {finding['rule']}\n")
                        f.write(f"- **Description:** {finding['description']}\n")
                        f.write(f"- **Remediation:** {finding['remediation']}\n\n")
                    f.write(f"---\n\n")

                # Medium Findings
                if self.findings['medium']:
                    f.write(f"## Medium-Severity Findings ({len(self.findings['medium'])})\n\n")
                    for i, finding in enumerate(self.findings['medium'], 1):
                        f.write(f"### {i}. {finding['type']} ({finding['tool']})\n\n")
                        f.write(f"- **File:** `{finding['file']}:{finding['line']}`\n")
                        f.write(f"- **Rule:** {finding['rule']}\n")
                        f.write(f"- **Description:** {finding['description']}\n")
                        f.write(f"- **Remediation:** {finding['remediation']}\n\n")
                    f.write(f"---\n\n")

                # RBAC Analysis
                if self.tool_stats['rbac']['content']:
                    f.write(f"## RBAC Privilege Chain Analysis\n\n")
                    # RBAC analyzer already outputs markdown format - no code blocks needed
                    f.write(self.tool_stats['rbac']['content'])
                    f.write(f"\n---\n\n")

                # YAML Lint Issues (separate section, non-security)
                yamllint_stats = self.tool_stats.get('yamllint', {})
                if yamllint_stats.get('findings', 0) > 0:
                    f.write(f"## Code Quality: YAML Formatting Issues\n\n")
                    f.write(f"**Note:** These are style/formatting issues, not security vulnerabilities.\n\n")

                    yamllint_findings = yamllint_stats.get('findings_data', [])
                    if yamllint_stats.get('truncated', False):
                        total = yamllint_stats.get('total_findings', len(yamllint_findings))
                        f.write(f"Showing {len(yamllint_findings)} of {total} yamllint findings (truncated for readability).\n\n")

                    # Group by severity
                    errors = [f for f in yamllint_findings if f.get('level') == 'error']
                    warnings = [f for f in yamllint_findings if f.get('level') == 'warning']

                    if errors:
                        f.write(f"<details>\n")
                        f.write(f"<summary>YAML Errors ({len(errors)}) - Click to expand</summary>\n\n")
                        for i, finding in enumerate(errors, 1):
                            f.write(f"{i}. **{finding['rule']}** in `{finding['file']}:{finding['line']}`\n")
                            f.write(f"   - {finding['description']}\n\n")
                        f.write(f"</details>\n\n")

                    if warnings:
                        f.write(f"<details>\n")
                        f.write(f"<summary>YAML Warnings ({len(warnings)}) - Click to expand</summary>\n\n")
                        for i, finding in enumerate(warnings, 1):
                            f.write(f"{i}. **{finding['rule']}** in `{finding['file']}:{finding['line']}`\n")
                            f.write(f"   - {finding['description']}\n\n")
                        f.write(f"</details>\n\n")

                    f.write(f"**Remediation:** These are YAML style and formatting issues, not security vulnerabilities. ")
                    f.write(f"See the dedicated **yamllint-report.md** artifact for complete findings and detailed remediation instructions.\n\n")
                    f.write(f"---\n\n")

                # Recommendations (dynamic based on actual findings)
                f.write(f"## 📋 Recommendations\n\n")

                if critical_count > 0:
                    f.write(f"### Immediate Actions (Critical)\n\n")
                    rec_num = 1

                    # Check for specific tool findings
                    has_gitleaks = any(f['tool'] == 'Gitleaks' for f in self.findings['critical'])
                    has_trufflehog = any(f['tool'] == 'TruffleHog' for f in self.findings['critical'])
                    has_rbac_critical = any(f['tool'] == 'RBAC Analyzer' for f in self.findings['critical'])

                    if has_trufflehog:
                        f.write(f"{rec_num}. **URGENT: Rotate verified credentials immediately** - TruffleHog confirmed these credentials are active\n")
                        rec_num += 1
                    if has_gitleaks:
                        f.write(f"{rec_num}. **Remove hardcoded secrets** from codebase and use secret management\n")
                        rec_num += 1
                    if has_rbac_critical:
                        f.write(f"{rec_num}. **Fix critical RBAC permissions** - remove wildcards and dangerous verbs\n")
                        rec_num += 1
                    f.write("\n")

                if high_count > 0:
                    f.write(f"### High Priority (This Week)\n\n")
                    rec_num = 1

                    has_semgrep_high = any(f['tool'] == 'Semgrep' for f in self.findings['high'])
                    has_shellcheck_high = any(f['tool'] == 'ShellCheck' for f in self.findings['high'])
                    has_rbac_high = any(f['tool'] == 'RBAC Analyzer' for f in self.findings['high'])

                    if has_semgrep_high:
                        f.write(f"{rec_num}. Review and fix high-severity Semgrep findings\n")
                        rec_num += 1
                    if has_shellcheck_high:
                        f.write(f"{rec_num}. Fix high-severity ShellCheck issues to prevent command injection\n")
                        rec_num += 1
                    if has_rbac_high:
                        f.write(f"{rec_num}. Tighten high-risk RBAC permissions\n")
                        rec_num += 1
                    f.write("\n")

                if medium_count > 0 or low_count > 0:
                    f.write(f"### Medium/Low Priority (Next Sprint)\n\n")
                    rec_num = 1

                    has_hadolint = self.tool_stats.get('hadolint', {}).get('findings', 0) > 0
                    has_shellcheck_medium = any(f['tool'] == 'ShellCheck' for f in self.findings['medium'])
                    has_semgrep_medium = any(f['tool'] == 'Semgrep' for f in self.findings['medium'])

                    if has_hadolint:
                        f.write(f"{rec_num}. Address Dockerfile best practice violations\n")
                        rec_num += 1
                    if has_shellcheck_medium:
                        f.write(f"{rec_num}. Fix ShellCheck warnings in scripts\n")
                        rec_num += 1
                    if has_semgrep_medium:
                        f.write(f"{rec_num}. Review medium-severity Semgrep findings\n")
                        rec_num += 1
                    f.write("\n")

                # Next Steps
                f.write(f"## 🎯 Next Steps\n\n")
                f.write(f"1. **Review this report** and triage findings by severity\n")
                f.write(f"2. **Check SARIF results** in the Security tab for detailed code locations\n")
                f.write(f"3. **Download artifacts** from the workflow run for raw tool outputs\n")
                f.write(f"4. **Create JIRA tickets** for remediation work and track progress\n\n")
                f.write(f"---\n\n")
                f.write(f"*This report was automatically generated by the Security Full Codebase Scan workflow.*\n")
        except IOError as e:
            print(f"[ERROR] Failed to write security report to {output_file}: {str(e)}", file=sys.stderr)
            sys.exit(1)

        # Generate JSON summary if requested
        if json_summary_file:
            self._generate_json_summary(json_summary_file, posture, total_findings, critical_count, high_count, medium_count, low_count)

        # Generate dedicated yamllint report if requested
        if yamllint_report_file:
            self._generate_yamllint_report(yamllint_report_file)

    def _generate_json_summary(self, output_file: str, posture: str, total: int, critical: int, high: int, medium: int, low: int):
        """Generate machine-parseable JSON summary for workflow badge extraction"""

        # Map display names to tool_stats keys
        tool_key_map = {
            'Gitleaks': 'gitleaks',
            'TruffleHog': 'trufflehog',
            'Semgrep': 'semgrep',
            'Hadolint': 'hadolint',
            'ShellCheck': 'shellcheck',
            'yamllint': 'yamllint',
            'actionlint': 'actionlint',
            'RBAC Analyzer': 'rbac'
        }

        # Calculate per-tool severity breakdowns
        tool_breakdowns = {}
        for tool_name in ['Gitleaks', 'TruffleHog', 'Semgrep', 'Hadolint', 'ShellCheck', 'yamllint', 'actionlint', 'RBAC Analyzer']:
            stats_key = tool_key_map[tool_name]
            tool_breakdowns[tool_name] = {
                'status': self.tool_stats.get(stats_key, {}).get('status', 'UNKNOWN'),
                'total': 0,
                'critical': 0,
                'high': 0,
                'medium': 0,
                'low': 0,
                'info': 0
            }

            # Count findings per severity for this tool
            for severity in ['critical', 'high', 'medium', 'low', 'info']:
                count = sum(1 for f in self.findings.get(severity, []) if f.get('tool') == tool_name)
                tool_breakdowns[tool_name][severity] = count
                tool_breakdowns[tool_name]['total'] += count

        # Add yamllint as code quality (separate from security findings)
        yamllint_summary = {
            'total': self.tool_stats.get('yamllint', {}).get('findings', 0),
            'errors': len([f for f in self.tool_stats.get('yamllint', {}).get('findings_data_all', []) if f.get('level') == 'error']),
            'warnings': len([f for f in self.tool_stats.get('yamllint', {}).get('findings_data_all', []) if f.get('level') == 'warning']),
            'status': self.tool_stats.get('yamllint', {}).get('status', 'SKIPPED')
        }

        # Calculate AI measurement metrics
        metrics = {
            'security_density': {
                'critical_per_scan': critical,
                'high_per_scan': high,
                'total_security_findings': total,
                'security_tools_run': sum(1 for t in tool_breakdowns.values() if t['status'] not in ['⏭️ SKIPPED', 'UNKNOWN'])
            },
            'code_quality_density': {
                'yamllint_total': yamllint_summary['total'],
                'yamllint_errors': yamllint_summary['errors'],
                'yamllint_warnings': yamllint_summary['warnings']
            },
            'remediation_priority': {
                'immediate_action_required': critical > 0,
                'high_priority_count': critical + high,
                'medium_priority_count': medium,
                'low_priority_count': low
            },
            'trend_indicators': {
                'has_critical_secrets': tool_breakdowns.get('Gitleaks', {}).get('critical', 0) > 0 or tool_breakdowns.get('TruffleHog', {}).get('critical', 0) > 0,
                'has_verified_secrets': tool_breakdowns.get('TruffleHog', {}).get('critical', 0) > 0,
                'has_rbac_issues': tool_breakdowns.get('RBAC Analyzer', {}).get('total', 0) > 0,
                'has_code_quality_issues': yamllint_summary['total'] > 0
            }
        }

        summary = {
            'format_version': '1.0',
            'generated': datetime.utcnow().strftime('%Y-%m-%d %H:%M:%S UTC'),
            'commit': self.github.get('sha', 'unknown'),
            'branch': self.github.get('ref_name', 'unknown'),
            'repository': self.github.get('repository', 'unknown'),
            'posture': posture,
            'total_findings': total,
            'severity_counts': {
                'critical': critical,
                'high': high,
                'medium': medium,
                'low': low,
                'info': len(self.findings.get('info', []))
            },
            'tools': tool_breakdowns,
            'code_quality': {
                'yamllint': yamllint_summary
            },
            'metrics': metrics
        }

        try:
            with open(output_file, 'w') as f:
                json.dump(summary, f, indent=2)
        except IOError as e:
            print(f"[ERROR] Failed to write JSON summary to {output_file}: {str(e)}", file=sys.stderr)
            sys.exit(1)

    def _generate_yamllint_report(self, output_file: str):
        """Generate dedicated yamllint report with all findings"""

        yamllint_stats = self.tool_stats.get('yamllint', {})
        if yamllint_stats.get('findings', 0) == 0:
            return  # Skip if no yamllint findings

        try:
            with open(output_file, 'w') as f:
                f.write(f"# YAMLlint Code Quality Report\n\n")
                f.write(f"**Generated:** {datetime.utcnow().strftime('%Y-%m-%d %H:%M:%S UTC')}\n\n")
                f.write(f"**Repository:** {self.github.get('repository', 'unknown')}\n\n")
                f.write(f"**Commit:** {self.github.get('sha', 'unknown')}\n\n")
                f.write(f"**Branch:** {self.github.get('ref_name', 'unknown')}\n\n")
                f.write(f"---\n\n")

                total = yamllint_stats.get('findings', 0)
                f.write(f"## Summary\n\n")
                f.write(f"**Total Issues:** {total}\n\n")

                # Use ALL findings for dedicated report (not truncated)
                yamllint_findings = yamllint_stats.get('findings_data_all', [])
                errors = [fi for fi in yamllint_findings if fi.get('level') == 'error']
                warnings = [fi for fi in yamllint_findings if fi.get('level') == 'warning']

                f.write(f"- Errors: {len(errors)}\n")
                f.write(f"- Warnings: {len(warnings)}\n\n")

                f.write(f"---\n\n")

                if errors:
                    f.write(f"## Errors ({len(errors)})\n\n")
                    for i, finding in enumerate(errors, 1):
                        f.write(f"### {i}. {finding['rule']}\n\n")
                        f.write(f"- **File:** `{finding['file']}:{finding['line']}`\n")
                        f.write(f"- **Description:** {finding['description']}\n\n")

                if warnings:
                    f.write(f"## Warnings ({len(warnings)})\n\n")
                    for i, finding in enumerate(warnings, 1):
                        f.write(f"### {i}. {finding['rule']}\n\n")
                        f.write(f"- **File:** `{finding['file']}:{finding['line']}`\n")
                        f.write(f"- **Description:** {finding['description']}\n\n")

                f.write(f"---\n\n")
                f.write(f"## Remediation\n\n")
                f.write(f"These are YAML style and formatting issues, not security vulnerabilities.\n\n")
                f.write(f"To fix automatically (where possible):\n")
                f.write(f"```bash\n")
                f.write(f"# Install yamllint\n")
                f.write(f"pip install yamllint\n\n")
                f.write(f"# Check current issues\n")
                f.write(f"yamllint .\n\n")
                f.write(f"# Many issues can be fixed manually or with automated formatters\n")
                f.write(f"```\n\n")
                f.write(f"Common fixes:\n")
                f.write(f"- **line-length**: Break long lines, use YAML multi-line strings\n")
                f.write(f"- **trailing-spaces**: Remove whitespace at end of lines\n")
                f.write(f"- **indentation**: Use consistent 2-space indentation\n")
                f.write(f"- **truthy**: Use `true`/`false` instead of `yes`/`no`\n\n")

        except IOError as e:
            print(f"[ERROR] Failed to write yamllint report to {output_file}: {str(e)}", file=sys.stderr)


def main():
    parser = argparse.ArgumentParser(description='Generate comprehensive security scan report')
    parser.add_argument('--output', default='security-report.md', help='Output file path')
    parser.add_argument('--json-summary', default=None, help='JSON summary output file for workflow parsing')
    parser.add_argument('--yamllint-report', default=None, help='Dedicated yamllint report output file (all findings)')
    parser.add_argument('--workspace', default='.', help='Workspace directory')
    parser.add_argument('--yamllint-limit', type=int, default=50, help='Maximum yamllint findings to show in comprehensive report (default: 50)')
    args = parser.parse_args()

    # Gather GitHub context from environment
    github_context = {
        'repository': os.getenv('GITHUB_REPOSITORY', 'unknown'),
        'sha': os.getenv('GITHUB_SHA', 'unknown'),
        'ref_name': os.getenv('GITHUB_REF_NAME', 'unknown'),
        'run_url': f"{os.getenv('GITHUB_SERVER_URL', '')}/{os.getenv('GITHUB_REPOSITORY', '')}/actions/runs/{os.getenv('GITHUB_RUN_ID', '')}"
    }

    try:
        generator = SecurityReportGenerator(args.workspace, github_context, yamllint_limit=args.yamllint_limit)
        generator.generate_report(args.output, args.json_summary, args.yamllint_report)
        print(f"✅ Comprehensive security report generated: {args.output}")
        if args.json_summary:
            print(f"✅ JSON summary generated: {args.json_summary}")
        if args.yamllint_report:
            print(f"✅ Dedicated yamllint report generated: {args.yamllint_report}")
    except Exception as e:
        print(f"❌ Failed to generate security report: {str(e)}", file=sys.stderr)
        sys.exit(1)


if __name__ == '__main__':
    main()
