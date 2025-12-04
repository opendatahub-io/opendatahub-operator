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
    - RBAC Analyzer (text)
"""

import json
import os
import sys
import argparse
import re
from datetime import datetime
from pathlib import Path
from typing import Dict, List, Any, Optional


class SecurityReportGenerator:
    def __init__(self, workspace: str, github_context: Dict[str, str]):
        self.workspace = Path(workspace)
        self.github = github_context
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
                    stats['findings'] = len(data)
                    stats['status'] = '❌ FINDINGS'

                    for finding in data:
                        self.findings['critical'].append({
                            'tool': 'Gitleaks',
                            'type': 'Hardcoded Secret',
                            'severity': 'CRITICAL',
                            'file': finding.get('File', 'unknown'),
                            'line': finding.get('StartLine', '?'),
                            'rule': finding.get('RuleID', 'unknown'),
                            'description': finding.get(
                                'Description',
                                'Secret detected; see Gitleaks JSON artifact for details (value redacted)'
                            ),
                            'remediation': 'Remove secret from code, rotate credential, use secret manager'
                        })
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

            for file_findings in data.values():
                for finding in file_findings:
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

                # Count findings by looking for severity markers at start of lines
                # This prevents false matches in descriptions or remediation text
                critical_count = len(re.findall(r'(?m)^\s*CRITICAL\b', content))
                high_count = len(re.findall(r'(?m)^\s*HIGH\b', content))
                warning_count = len(re.findall(r'(?m)^\s*WARNING\b', content))

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
                            'file': 'rbac-analysis.txt',
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
                            'file': 'rbac-analysis.txt',
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
                            'file': 'rbac-analysis.txt',
                            'line': '?',
                            'rule': 'RBAC_ANALYZER_WARNING',
                            'description': 'RBAC warning; see RBAC analysis section',
                            'remediation': 'Review and tighten RBAC where feasible'
                        })

        except Exception as e:
            stats['status'] = '⚠️ ERROR: Failed to parse RBAC analyzer output'
            print(f"[ERROR] RBAC analyzer parser: {str(e)}", file=sys.stderr)

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

    def generate_report(self, output_file: str):
        """Generate comprehensive markdown security report"""

        # Parse all tool outputs
        self.tool_stats['gitleaks'] = self.parse_gitleaks(f'{self.workspace}/gitleaks.json')
        self.tool_stats['trufflehog'] = self.parse_trufflehog(f'{self.workspace}/trufflehog.json')
        self.tool_stats['semgrep'] = self.parse_semgrep_sarif(f'{self.workspace}/semgrep.sarif')
        self.tool_stats['hadolint'] = self.parse_hadolint_sarif(f'{self.workspace}/hadolint.sarif')
        self.tool_stats['shellcheck'] = self.parse_shellcheck(f'{self.workspace}/shellcheck.json')
        self.tool_stats['rbac'] = self.parse_rbac_analyzer(f'{self.workspace}/rbac-analysis.txt')

        # Calculate totals
        total_findings = sum(len(findings) for findings in self.findings.values())
        critical_count = len(self.findings['critical'])
        high_count = len(self.findings['high'])
        medium_count = len(self.findings['medium'])
        low_count = len(self.findings['low'])

        # Determine overall security posture
        if critical_count > 0:
            posture = '🔴 CRITICAL'
            posture_desc = 'Immediate action required - critical vulnerabilities detected'
        elif high_count > 0:
            posture = '🟠 HIGH'
            posture_desc = 'High-severity issues detected - prompt review needed'
        elif medium_count > 0:
            posture = '🟡 MEDIUM'
            posture_desc = 'Medium-severity issues detected - review recommended'
        elif low_count > 0:
            posture = '🟢 LOW'
            posture_desc = 'Low-severity issues detected - minor improvements suggested'
        else:
            posture = '✅ CLEAN'
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
                f.write(f"- 🔴 Critical: {critical_count}\n")
                f.write(f"- 🟠 High: {high_count}\n")
                f.write(f"- 🟡 Medium: {medium_count}\n")
                f.write(f"- 🔵 Low: {low_count}\n")
                f.write(f"- ℹ️ Info: {len(self.findings['info'])}\n\n")
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
                f.write(f"| {self.tool_stats['rbac']['tool']} | RBAC privilege chain analysis | {self.tool_stats['rbac']['status']} | {self.tool_stats['rbac']['findings']} |\n\n")
                f.write(f"---\n\n")

                # Critical Findings
                if self.findings['critical']:
                    f.write(f"## 🔴 Critical Findings ({len(self.findings['critical'])})\n\n")
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
                    f.write(f"## 🟠 High-Severity Findings ({len(self.findings['high'])})\n\n")
                    for i, finding in enumerate(self.findings['high'], 1):
                        f.write(f"### {i}. {finding['type']} ({finding['tool']})\n\n")
                        f.write(f"- **File:** `{finding['file']}:{finding['line']}`\n")
                        f.write(f"- **Rule:** {finding['rule']}\n")
                        f.write(f"- **Description:** {finding['description']}\n")
                        f.write(f"- **Remediation:** {finding['remediation']}\n\n")
                    f.write(f"---\n\n")

                # Medium Findings
                if self.findings['medium']:
                    f.write(f"## 🟡 Medium-Severity Findings ({len(self.findings['medium'])})\n\n")
                    for i, finding in enumerate(self.findings['medium'], 1):
                        f.write(f"### {i}. {finding['type']} ({finding['tool']})\n\n")
                        f.write(f"- **File:** `{finding['file']}:{finding['line']}`\n")
                        f.write(f"- **Rule:** {finding['rule']}\n")
                        f.write(f"- **Description:** {finding['description']}\n")
                        f.write(f"- **Remediation:** {finding['remediation']}\n\n")
                    f.write(f"---\n\n")

                # RBAC Analysis
                if self.tool_stats['rbac']['content']:
                    f.write(f"## 🔐 RBAC Privilege Chain Analysis\n\n")
                    f.write(f"```\n")
                    f.write(self.tool_stats['rbac']['content'])
                    f.write(f"```\n\n")
                    f.write(f"---\n\n")

                # Recommendations
                f.write(f"## 📋 Recommendations\n\n")
                if critical_count > 0:
                    f.write(f"### Immediate Actions (Critical)\n\n")
                    f.write(f"1. **Rotate all exposed credentials immediately** - especially verified credentials from TruffleHog\n")
                    f.write(f"2. **Remove hardcoded secrets** from codebase and use secret management\n")
                    f.write(f"3. **Fix dangerous RBAC permissions** - remove wildcards and dangerous verbs\n\n")

                if high_count > 0:
                    f.write(f"### High Priority (This Week)\n\n")
                    f.write(f"1. Review and fix high-severity Semgrep findings\n")
                    f.write(f"2. Address insecure TLS and weak cryptography usage\n")
                    f.write(f"3. Fix privileged container configurations\n\n")

                if medium_count > 0 or low_count > 0:
                    f.write(f"### Medium/Low Priority (Next Sprint)\n\n")
                    f.write(f"1. Address Dockerfile best practice violations\n")
                    f.write(f"2. Fix ShellCheck warnings in scripts\n")
                    f.write(f"3. Improve YAML formatting and validation\n\n")

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


def main():
    parser = argparse.ArgumentParser(description='Generate comprehensive security scan report')
    parser.add_argument('--output', default='security-report.md', help='Output file path')
    parser.add_argument('--workspace', default='.', help='Workspace directory')
    args = parser.parse_args()

    # Gather GitHub context from environment
    github_context = {
        'repository': os.getenv('GITHUB_REPOSITORY', 'unknown'),
        'sha': os.getenv('GITHUB_SHA', 'unknown'),
        'ref_name': os.getenv('GITHUB_REF_NAME', 'unknown'),
        'run_url': f"{os.getenv('GITHUB_SERVER_URL', '')}/{os.getenv('GITHUB_REPOSITORY', '')}/actions/runs/{os.getenv('GITHUB_RUN_ID', '')}"
    }

    try:
        generator = SecurityReportGenerator(args.workspace, github_context)
        generator.generate_report(args.output)
        print(f"✅ Comprehensive security report generated: {args.output}")
    except Exception as e:
        print(f"❌ Failed to generate security report: {str(e)}", file=sys.stderr)
        sys.exit(1)


if __name__ == '__main__':
    main()
