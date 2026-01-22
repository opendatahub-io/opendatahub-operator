#!/usr/bin/env python3
"""
Interactive Security Findings Acknowledgment Tool

Allows teams to interactively acknowledge security findings that are false positives
or accepted risks. Updates .github/config/security-baseline.yaml with detailed justifications.

Usage:
    python .github/scripts/acknowledge-findings.py
    python .github/scripts/acknowledge-findings.py --tool gitleaks
    python .github/scripts/acknowledge-findings.py --team security-team

Supports all 9 security tools:
    - Gitleaks (secrets)
    - TruffleHog (verified credentials)
    - Semgrep (SAST)
    - ShellCheck (shell scripts)
    - Hadolint (Dockerfiles)
    - yamllint (YAML validation)
    - actionlint (GitHub Actions)
    - kube-linter (Kubernetes manifests)
    - RBAC Analyzer (privilege chains)
"""

import json
import os
import sys
import argparse
import re
import hashlib
import yaml
from datetime import datetime, timezone
from pathlib import Path
from typing import Dict, List, Any, Optional, Tuple


class FindingAcknowledger:
    """Interactive tool for acknowledging security findings"""

    def __init__(self, workspace: str = '.', team: Optional[str] = None):
        self.workspace = Path(workspace)
        self.baseline_path = self.workspace / '.github' / 'config' / 'security-baseline.yaml'
        self.team = team or os.getenv('USER', 'unknown-user')
        self.baseline_data = {}
        self.available_tools = []

    def detect_available_findings(self) -> Dict[str, Path]:
        """Detect which tool output files exist in workspace

        Returns:
            Dict mapping tool name to file path
        """
        # Reset available_tools to prevent duplicates on repeated invocations
        self.available_tools = []

        tool_files = {
            'gitleaks': 'gitleaks.json',
            'trufflehog': 'trufflehog.json',
            'semgrep': 'semgrep.sarif',
            'shellcheck': 'shellcheck.json',
            'hadolint': 'hadolint.sarif',
            'yamllint': 'yamllint.txt',
            'actionlint': 'actionlint.txt',
            'kube-linter': 'kube-linter.json',
            'rbac-analyzer': 'rbac-analysis.md'
        }

        found = {}
        for tool, filename in tool_files.items():
            filepath = self.workspace / filename
            if filepath.exists() and filepath.stat().st_size > 0:
                found[tool] = filepath
                self.available_tools.append(tool)

        return found

    def load_baseline(self) -> None:
        """Load existing baseline file or create new structure

        Tries in order:
        1. .github/config/security-baseline.yaml (v2.0 - preferred)
        2. .security-baseline.json (v2.0 - backward compat)
        3. Create new baseline if neither exists
        """
        # Try YAML baseline first (preferred format)
        yaml_path = self.workspace / '.github' / 'config' / 'security-baseline.yaml'
        json_path = self.workspace / '.security-baseline.json'

        if yaml_path.exists():
            with open(yaml_path) as f:
                loaded = yaml.safe_load(f)
                # Handle empty YAML files (yaml.safe_load returns None)
                if not loaded or not isinstance(loaded, dict):
                    self.baseline_data = {
                        'version': '2.0',
                        'description': 'Acknowledged security findings that are not real issues',
                        '_comment': 'Findings acknowledged by teams using CLI tool or Claude skill',
                        'gitleaks': [],
                        'trufflehog': [],
                        'semgrep': [],
                        'shellcheck': [],
                        'hadolint': [],
                        'yamllint': [],
                        'actionlint': [],
                        'kube-linter': [],
                        'rbac-analyzer': []
                    }
                else:
                    self.baseline_data = loaded
        elif json_path.exists():
            try:
                with open(json_path) as f:
                    loaded = json.load(f)
                    # Validate loaded data is a dict
                    if not loaded or not isinstance(loaded, dict):
                        raise ValueError("Invalid baseline format")
                    self.baseline_data = loaded
                print("[INFO] Loaded legacy JSON baseline - will migrate to YAML on save", file=sys.stderr)
            except (json.JSONDecodeError, ValueError) as e:
                print(f"[WARN] Failed to load {json_path}: {e} - using empty baseline", file=sys.stderr)
                self.baseline_data = {
                    'version': '2.0',
                    'description': 'Acknowledged security findings that are not real issues',
                    '_comment': 'Findings acknowledged by teams using CLI tool or Claude skill',
                    'gitleaks': [],
                    'trufflehog': [],
                    'semgrep': [],
                    'shellcheck': [],
                    'hadolint': [],
                    'yamllint': [],
                    'actionlint': [],
                    'kube-linter': [],
                    'rbac-analyzer': []
                }
        else:
            # Create new baseline structure
            self.baseline_data = {
                'version': '2.0',
                'description': 'Acknowledged security findings that are not real issues',
                '_comment': 'Findings acknowledged by teams using CLI tool or Claude skill',
                'gitleaks': [],
                'trufflehog': [],
                'semgrep': [],
                'shellcheck': [],
                'hadolint': [],
                'yamllint': [],
                'actionlint': [],
                'kube-linter': [],
                'rbac-analyzer': []
            }

    def parse_gitleaks(self, filepath: Path) -> List[Dict[str, Any]]:
        """Parse Gitleaks JSON output"""
        findings = []
        try:
            with open(filepath) as f:
                data = json.load(f)
                if not data:
                    return []

                for item in data:
                    # Normalize file path
                    file_path = item.get('File', 'unknown')
                    if file_path.startswith('/repo/'):
                        file_path = file_path[6:]
                    file_path = os.path.normpath(file_path).lstrip('/')

                    description = item.get('Description', 'Secret detected')
                    desc_hash = hashlib.sha256(description.encode()).hexdigest()[:8]

                    findings.append({
                        'file': file_path,
                        'line': item.get('StartLine', '?'),
                        'rule': item.get('RuleID', 'unknown'),
                        'description_hash': desc_hash,
                        'description': description
                    })
        except Exception as e:
            print(f"Error parsing Gitleaks output: {e}", file=sys.stderr)

        return findings

    def parse_kube_linter(self, filepath: Path) -> List[Dict[str, Any]]:
        """Parse kube-linter JSON output

        Note: kube-linter v0.7.6+ structure has K8sObjectInfo fields under Object.K8sObject
        """
        findings = []
        try:
            with open(filepath) as f:
                data = json.load(f)
                reports = data.get('Reports', [])

                for report in reports:
                    # kube-linter v0.7.6+ structure: Object.K8sObject contains the resource info
                    # Fallback: if K8sObject doesn't exist, use Object directly for forward compatibility
                    obj_container = report.get('Object', {})
                    obj = obj_container.get('K8sObject', obj_container)
                    findings.append({
                        'check': report.get('Check', 'unknown'),
                        'object': {
                            'kind': obj.get('GroupVersionKind', {}).get('Kind', 'unknown'),
                            'name': obj.get('Name', 'unknown'),
                            'namespace': obj.get('Namespace') or None
                        },
                        'message': report.get('Diagnostic', {}).get('Message', '')
                    })
        except Exception as e:
            print(f"Error parsing kube-linter output: {e}", file=sys.stderr)

        return findings

    def parse_shellcheck(self, filepath: Path) -> List[Dict[str, Any]]:
        """Parse ShellCheck JSON output"""
        findings = []
        try:
            with open(filepath) as f:
                data = json.load(f)

                for item in data:
                    findings.append({
                        'file': item.get('file', ''),
                        'line': item.get('line', 0),
                        'code': item.get('code', 0),
                        'message': item.get('message', ''),
                        'level': item.get('level', 'warning')
                    })
        except Exception as e:
            print(f"Error parsing ShellCheck output: {e}", file=sys.stderr)

        return findings

    def parse_trufflehog(self, filepath: Path) -> List[Dict[str, Any]]:
        """Parse TruffleHog JSONL output"""
        findings = []
        try:
            with open(filepath) as f:
                for line in f:
                    if not line.strip():
                        continue

                    item = json.loads(line)

                    # Extract file and line from nested structure
                    fs_data = item.get('SourceMetadata', {}).get('Data', {}).get('Filesystem', {})
                    file_path = fs_data.get('file', '')
                    line_num = fs_data.get('line', 0)

                    raw_secret = item.get('Raw', '')

                    findings.append({
                        'detector': item.get('DetectorName', ''),
                        'file': file_path,
                        'line': line_num,
                        'verified': item.get('Verified', False),
                        'raw': f"[REDACTED - {len(raw_secret)} chars]"  # Security: Never expose secret content
                    })
        except Exception as e:
            print(f"Error parsing TruffleHog output: {e}", file=sys.stderr)

        return findings

    def parse_yamllint(self, filepath: Path) -> List[Dict[str, Any]]:
        """Parse yamllint text output"""
        findings = []
        try:
            # Regex pattern: file:line:col: [level] message (rule)
            pattern = r'^(.+?):(\d+):(\d+): \[(\w+)\] (.+?) \((.+?)\)$'

            with open(filepath) as f:
                for line in f:
                    match = re.match(pattern, line.strip())
                    if match:
                        findings.append({
                            'file': match.group(1),
                            'line': int(match.group(2)),
                            'column': int(match.group(3)),
                            'level': match.group(4),
                            'message': match.group(5),
                            'rule': match.group(6)
                        })
        except Exception as e:
            print(f"Error parsing yamllint output: {e}", file=sys.stderr)

        return findings

    def parse_actionlint(self, filepath: Path) -> List[Dict[str, Any]]:
        """Parse actionlint text output"""
        findings = []
        try:
            # Regex patterns:
            # Pattern 1: filepath:line:col: message [rule]
            # Pattern 2: filepath:line:col: message (no rule)
            pattern_with_rule = r'^(.+?):(\d+):(\d+): (.+?) \[(.+?)\]$'
            pattern_no_rule = r'^(.+?):(\d+):(\d+): (.+?)$'

            with open(filepath) as f:
                for line in f:
                    line = line.strip()
                    # Try pattern with rule first
                    match = re.match(pattern_with_rule, line)
                    if match:
                        findings.append({
                            'file': match.group(1),
                            'line': int(match.group(2)),
                            'column': int(match.group(3)),
                            'message': match.group(4),
                            'rule': match.group(5)
                        })
                    else:
                        # Try pattern without rule
                        match = re.match(pattern_no_rule, line)
                        if match:
                            findings.append({
                                'file': match.group(1),
                                'line': int(match.group(2)),
                                'column': int(match.group(3)),
                                'message': match.group(4),
                                'rule': None
                            })
        except Exception as e:
            print(f"Error parsing actionlint output: {e}", file=sys.stderr)

        return findings

    def _parse_sarif(self, filepath: Path, tool_name: str) -> List[Dict[str, Any]]:
        """Helper method to parse SARIF format output (used by Semgrep and Hadolint)"""
        findings = []
        try:
            with open(filepath) as f:
                data = json.load(f)

            for run in data.get('runs', []):
                for result in run.get('results', []):
                    # Extract location (may have multiple, use first)
                    locations = result.get('locations', [])
                    if not locations:
                        continue

                    phys_loc = locations[0].get('physicalLocation', {})
                    artifact = phys_loc.get('artifactLocation', {})
                    region = phys_loc.get('region', {})

                    findings.append({
                        'rule_id': result.get('ruleId', ''),
                        'file': artifact.get('uri', ''),
                        'line': region.get('startLine', 0),
                        'message': result.get('message', {}).get('text', ''),
                        'level': result.get('level', 'warning')
                    })
        except Exception as e:
            print(f"Error parsing {tool_name} SARIF output: {e}", file=sys.stderr)

        return findings

    def parse_semgrep(self, filepath: Path) -> List[Dict[str, Any]]:
        """Parse Semgrep SARIF output"""
        return self._parse_sarif(filepath, 'Semgrep')

    def parse_hadolint(self, filepath: Path) -> List[Dict[str, Any]]:
        """Parse Hadolint SARIF output"""
        return self._parse_sarif(filepath, 'Hadolint')

    def parse_rbac_analyzer(self, filepath: Path) -> List[Dict[str, Any]]:
        """Parse RBAC Analyzer Markdown output"""
        findings = []
        try:
            with open(filepath) as f:
                content = f.read()

            # Parse markdown sections by severity
            severity_pattern = r'### (CRITICAL|HIGH|WARNING|INFO) \((\d+) findings?\)'
            finding_pattern = r'\d+\. \*\*(.+?)\*\*\s*\n\s*- File: `(.+?)`\s*\n\s*- Issue: (.+?)\n\s*- Fix: (.+?)(?=\n\n|\n\d+\.|\Z)'

            # Split by severity sections
            sections = re.split(severity_pattern, content)

            for i in range(1, len(sections), 3):
                severity = sections[i]
                section_content = sections[i+2] if i+2 < len(sections) else ''

                # Extract findings from this severity section
                for match in re.finditer(finding_pattern, section_content, re.DOTALL):
                    findings.append({
                        'severity': severity,
                        'title': match.group(1).strip(),
                        'file': match.group(2).strip(),
                        'issue': match.group(3).strip(),
                        'fix': match.group(4).strip()
                    })
        except Exception as e:
            print(f"Error parsing RBAC Analyzer output: {e}", file=sys.stderr)

        return findings

    def filter_new_findings(self, tool: str, all_findings: List[Dict]) -> List[Dict]:
        """Filter out findings that are already in baseline

        Returns:
            List of findings not in baseline
        """
        baseline_findings = self.baseline_data.get(tool, [])
        new_findings = []

        for finding in all_findings:
            is_acknowledged = False

            # Check if this finding matches any baseline entry
            for baseline_entry in baseline_findings:
                if self._findings_match(tool, finding, baseline_entry):
                    is_acknowledged = True
                    break

            if not is_acknowledged:
                new_findings.append(finding)

        return new_findings

    def _findings_match(self, tool: str, finding: Dict, baseline_entry: Dict) -> bool:
        """Check if a finding matches a baseline entry

        Each tool has different matching criteria based on unique identifiers
        """
        # Helper: normalize line numbers for comparison (handles int vs str mismatch)
        def normalize_line(value):
            """Convert line number to string for comparison, handle None"""
            return None if value is None else str(value)

        if tool == 'gitleaks':
            return (finding.get('file') == baseline_entry.get('file') and
                    normalize_line(finding.get('line')) == normalize_line(baseline_entry.get('line')) and
                    finding.get('rule') == baseline_entry.get('rule') and
                    finding.get('description_hash') == baseline_entry.get('description_hash'))

        elif tool == 'kube-linter':
            obj1 = finding.get('object', {})
            obj2 = baseline_entry.get('object', {})
            return (finding.get('check') == baseline_entry.get('check') and
                    obj1.get('kind') == obj2.get('kind') and
                    obj1.get('name') == obj2.get('name') and
                    obj1.get('namespace') == obj2.get('namespace'))

        elif tool == 'shellcheck':
            return (finding.get('file') == baseline_entry.get('file') and
                    normalize_line(finding.get('line')) == normalize_line(baseline_entry.get('line')) and
                    finding.get('code') == baseline_entry.get('code'))

        elif tool == 'trufflehog':
            return (finding.get('detector') == baseline_entry.get('detector') and
                    finding.get('file') == baseline_entry.get('file') and
                    normalize_line(finding.get('line')) == normalize_line(baseline_entry.get('line')))

        elif tool == 'yamllint':
            return (finding.get('file') == baseline_entry.get('file') and
                    normalize_line(finding.get('line')) == normalize_line(baseline_entry.get('line')) and
                    finding.get('rule') == baseline_entry.get('rule'))

        elif tool == 'actionlint':
            return (finding.get('file') == baseline_entry.get('file') and
                    normalize_line(finding.get('line')) == normalize_line(baseline_entry.get('line')) and
                    finding.get('message') == baseline_entry.get('message'))

        elif tool in ('semgrep', 'hadolint'):
            return (finding.get('rule_id') == baseline_entry.get('rule_id') and
                    finding.get('file') == baseline_entry.get('file') and
                    normalize_line(finding.get('line')) == normalize_line(baseline_entry.get('line')))

        elif tool == 'rbac-analyzer':
            return (finding.get('title') == baseline_entry.get('title') and
                    finding.get('file') == baseline_entry.get('file'))

        # Add more tool-specific matching logic as needed
        return False

    def interactive_acknowledge(self, tool: str, findings: List[Dict]) -> List[Dict]:
        """Interactive workflow to acknowledge findings

        Returns:
            List of acknowledged findings with reason and metadata
        """
        print(f"\n{'='*80}")
        print(f"📋 {tool.upper()} - New Findings")
        print(f"{'='*80}\n")

        if not findings:
            print("✅ No new findings to acknowledge\n")
            return []

        print(f"Found {len(findings)} new {tool} findings:\n")

        # Display findings
        for idx, finding in enumerate(findings, 1):
            print(f"[{idx}] ", end="")
            if tool == 'gitleaks':
                print(f"CRITICAL: {finding['rule']}")
                print(f"    File: {finding['file']}:{finding['line']}")
                print(f"    Description: {finding['description']}")
            elif tool == 'kube-linter':
                obj = finding['object']
                obj_id = f"{obj['kind']}/{obj['name']}"
                if obj['namespace']:
                    obj_id = f"{obj['kind']}/{obj['namespace']}/{obj['name']}"
                print(f"{finding['check']}")
                print(f"    Object: {obj_id}")
                print(f"    Message: {finding['message']}")
            elif tool == 'shellcheck':
                print(f"SC{finding['code']}: {finding['level'].upper()}")
                print(f"    File: {finding['file']}:{finding['line']}")
                print(f"    Message: {finding['message']}")
            elif tool == 'trufflehog':
                verified_status = "⛔ YES (ACTIVE CREDENTIAL)" if finding['verified'] else "✅ No"
                print(f"{finding['detector']}")
                print(f"    File: {finding['file']}:{finding['line']}")
                print(f"    Verified: {verified_status}")
                if not finding['verified']:
                    print(f"    Raw: {finding['raw']}")
            elif tool == 'yamllint':
                print(f"{finding['rule']}: {finding['level'].upper()}")
                print(f"    File: {finding['file']}:{finding['line']}:{finding['column']}")
                print(f"    Message: {finding['message']}")
            elif tool == 'actionlint':
                rule_info = f" [{finding['rule']}]" if finding.get('rule') else ""
                print(f"GitHub Actions Issue{rule_info}")
                print(f"    File: {finding['file']}:{finding['line']}:{finding['column']}")
                print(f"    Message: {finding['message']}")
            elif tool == 'semgrep':
                print(f"{finding['rule_id']}: {finding['level'].upper()}")
                print(f"    File: {finding['file']}:{finding['line']}")
                print(f"    Message: {finding['message']}")
            elif tool == 'hadolint':
                print(f"{finding['rule_id']}: {finding['level'].upper()}")
                print(f"    File: {finding['file']}:{finding['line']}")
                print(f"    Message: {finding['message']}")
            elif tool == 'rbac-analyzer':
                print(f"{finding['severity']}: {finding['title']}")
                print(f"    File: {finding['file']}")
                print(f"    Issue: {finding['issue']}")
                print(f"    Fix: {finding['fix']}")
            print()

        # Select findings to acknowledge
        while True:
            selection = input(f"Select findings to acknowledge (comma-separated, e.g., 1,2,3) or 'skip': ").strip()
            if selection.lower() == 'skip':
                return []

            try:
                # Filter empty tokens and deduplicate indices
                indices = sorted(set(int(x.strip()) for x in selection.split(',') if x.strip()))
                if not indices:
                    print("⚠️  Please enter at least one number")
                    continue
                if all(1 <= idx <= len(findings) for idx in indices):
                    break
                else:
                    print(f"⚠️  Please enter numbers between 1 and {len(findings)}")
            except ValueError:
                print("⚠️  Invalid input. Please enter comma-separated numbers or 'skip'")

        # Acknowledge selected findings
        acknowledged = []
        for idx in indices:
            finding = findings[idx - 1]
            print(f"\n{'─'*80}")
            print(f"Acknowledging finding #{idx}")
            print(f"{'─'*80}")

            # Block verified TruffleHog secrets from being acknowledged
            if tool == 'trufflehog' and finding.get('verified'):
                print("  ⛔ VERIFIED SECRET - CANNOT ACKNOWLEDGE")
                print("  ⚠️  This is an active, usable credential")
                print("  📋 Action required: ROTATE this credential immediately")
                print("  ✅ Skipping to next finding...")
                continue

            # Collect reason
            while True:
                reason = input("\nReason (required, explain why this isn't a real issue): ").strip()
                if len(reason) >= 10:
                    break
                print("⚠️  Reason must be at least 10 characters. Explain why this is safe to ignore.")

            # Collect acknowledged_by
            acknowledged_by = input(f"Acknowledged by [{self.team}]: ").strip() or self.team

            # Add metadata
            finding['reason'] = reason
            finding['acknowledged_by'] = acknowledged_by
            finding['acknowledged_date'] = datetime.now(timezone.utc).strftime('%Y-%m-%d')

            # Remove temporary fields
            if 'description' in finding and tool == 'gitleaks':
                del finding['description']
            if 'message' in finding and tool == 'kube-linter':
                del finding['message']
            if 'raw' in finding and tool == 'trufflehog':
                del finding['raw']
            if 'issue' in finding and tool == 'rbac-analyzer':
                del finding['issue']
            if 'fix' in finding and tool == 'rbac-analyzer':
                del finding['fix']

            acknowledged.append(finding)
            print("✅ Finding acknowledged")

        return acknowledged

    def update_baseline(self, tool: str, new_acknowledgments: List[Dict]) -> None:
        """Update baseline file with new acknowledgments"""
        if tool not in self.baseline_data:
            self.baseline_data[tool] = []

        self.baseline_data[tool].extend(new_acknowledgments)

    def save_baseline(self) -> None:
        """Save baseline file with proper formatting (YAML)"""
        # Ensure directory exists
        self.baseline_path.parent.mkdir(parents=True, exist_ok=True)

        with open(self.baseline_path, 'w') as f:
            yaml.dump(self.baseline_data, f,
                     default_flow_style=False,  # Use block style (more readable)
                     sort_keys=False,            # Preserve insertion order
                     allow_unicode=True)

    def run_interactive(self, tool_filter: Optional[str] = None) -> None:
        """Run interactive acknowledgment workflow"""
        print(f"\n{'='*80}")
        print("🔒 Security Findings Acknowledgment Tool")
        print(f"{'='*80}\n")

        # Detect available findings
        available = self.detect_available_findings()
        if not available:
            print("❌ No security tool output files found in current directory.")
            print("\n📥 To use this tool:")
            print("1. Download workflow artifacts from failed security scan")
            print("2. Extract output files (gitleaks.json, kube-linter.json, etc.)")
            print("3. Run this tool from the directory containing the files\n")
            sys.exit(1)

        print(f"✅ Found output files for {len(available)} tools:")
        for tool in available:
            print(f"   - {tool}")
        print()

        # Load baseline
        self.load_baseline()
        if self.baseline_path.exists():
            total_acknowledged = sum(len(entries) for entries in self.baseline_data.values() if isinstance(entries, list))
            print(f"📋 Loaded existing baseline with {total_acknowledged} acknowledged findings\n")
        else:
            print("📋 No existing baseline - will create new file\n")

        # Filter tools if specified
        if tool_filter:
            if tool_filter not in available:
                print(f"❌ Tool '{tool_filter}' output not found")
                sys.exit(1)
            tools_to_process = [tool_filter]
        else:
            tools_to_process = self.available_tools

        # Process each tool
        total_acknowledged = 0
        acknowledgments_by_tool = {}

        for tool in tools_to_process:
            # Parse findings
            if tool == 'gitleaks':
                all_findings = self.parse_gitleaks(available[tool])
            elif tool == 'kube-linter':
                all_findings = self.parse_kube_linter(available[tool])
            elif tool == 'shellcheck':
                all_findings = self.parse_shellcheck(available[tool])
            elif tool == 'trufflehog':
                all_findings = self.parse_trufflehog(available[tool])
            elif tool == 'yamllint':
                all_findings = self.parse_yamllint(available[tool])
            elif tool == 'actionlint':
                all_findings = self.parse_actionlint(available[tool])
            elif tool == 'semgrep':
                all_findings = self.parse_semgrep(available[tool])
            elif tool == 'hadolint':
                all_findings = self.parse_hadolint(available[tool])
            elif tool == 'rbac-analyzer':
                all_findings = self.parse_rbac_analyzer(available[tool])
            else:
                # All parsers implemented
                print(f"⏭️  Skipping {tool} (parser not yet implemented)")
                continue

            # Filter new findings
            new_findings = self.filter_new_findings(tool, all_findings)

            # Interactive acknowledgment
            acknowledged = self.interactive_acknowledge(tool, new_findings)
            if acknowledged:
                self.update_baseline(tool, acknowledged)
                total_acknowledged += len(acknowledged)
                acknowledgments_by_tool[tool] = len(acknowledged)

        # Save baseline
        if total_acknowledged > 0:
            self.save_baseline()
            print(f"\n{'='*80}")
            print(f"✅ Acknowledged {total_acknowledged} findings:")
            for tool, count in acknowledgments_by_tool.items():
                print(f"   - {tool}: {count} finding(s)")
            print(f"\n📝 Updated {self.baseline_path}")
            print(f"\n{'='*80}")
            print("\n📋 Next steps:")
            print(f"1. Review changes: git diff {self.baseline_path}")
            print(f"2. Commit: git add {self.baseline_path} && git commit -m \"chore: Acknowledge security findings\"")
            print("3. Push to re-run security checks")
            print()
        else:
            print("\n✅ No findings acknowledged\n")


def main():
    parser = argparse.ArgumentParser(
        description='Interactively acknowledge security findings as false positives or accepted risks',
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  # Interactive mode (recommended)
  python .github/scripts/acknowledge-findings.py

  # Acknowledge only Gitleaks findings
  python .github/scripts/acknowledge-findings.py --tool gitleaks

  # Specify team name
  python .github/scripts/acknowledge-findings.py --team security-team

Workflow:
  1. Download security scan artifacts from failed workflow
  2. Extract output files to current directory
  3. Run this tool to interactively acknowledge false positives
  4. Commit updated .github/config/security-baseline.yaml
  5. Push to re-run security checks
        """
    )
    parser.add_argument('--tool', help='Process only specific tool (gitleaks, kube-linter, etc.)')
    parser.add_argument('--team', help='Team or person acknowledging findings (default: $USER)')
    parser.add_argument('--workspace', default='.', help='Workspace directory (default: current directory)')

    args = parser.parse_args()

    try:
        acknowledger = FindingAcknowledger(workspace=args.workspace, team=args.team)
        acknowledger.run_interactive(tool_filter=args.tool)
    except KeyboardInterrupt:
        print("\n\n⚠️  Interrupted by user - no changes saved\n")
        sys.exit(130)
    except Exception as e:
        print(f"\n❌ Error: {str(e)}", file=sys.stderr)
        sys.exit(1)


if __name__ == '__main__':
    main()
