/**
 * Create GitHub Security Advisory for Critical Security Findings
 *
 * This script creates a private GitHub security advisory when critical security
 * issues are detected by the full codebase security scan workflow.
 *
 * The advisory includes:
 * - Workflow run details (commit, branch, run URL)
 * - Confidential RBAC attack scenarios with exploitation chains
 * - Next steps for remediation
 *
 * Usage (via github-script action):
 *   - uses: actions/github-script@v7
 *     with:
 *       script: |
 *         const createAdvisory = require('./.github/scripts/create-security-advisory.js')
 *         await createAdvisory({
 *           github,
 *           context,
 *           core,
 *           severity: 'high',
 *           workflowRunUrl: '...',
 *           commit: '...',
 *           branch: '...'
 *         })
 */

const fs = require('fs').promises;

/**
 * Read private RBAC analysis with attack scenarios.
 *
 * @returns {Promise<string>} Content of rbac-analysis-private.md or fallback message
 */
async function readRbacAnalysis() {
    try {
        const content = await fs.readFile('rbac-analysis-private.md', 'utf8');
        return content;
    } catch (error) {
        if (error.code === 'ENOENT') {
            console.error('rbac-analysis-private.md not found, skipping attack scenarios');
        } else {
            console.error(`Failed to read rbac-analysis-private.md: ${error.message}`);
        }
        return '*RBAC analysis with attack scenarios not available*';
    }
}

/**
 * Create a private GitHub security advisory.
 *
 * @param {Object} params - Parameters object
 * @param {Object} params.github - GitHub API client (@octokit/rest)
 * @param {Object} params.context - GitHub Actions context
 * @param {Object} params.core - GitHub Actions core utilities
 * @param {string} params.severity - Advisory severity (critical, high, moderate, low)
 * @param {string} params.workflowRunUrl - URL to the workflow run
 * @param {string} params.commit - Commit SHA
 * @param {string} params.branch - Branch name
 */
module.exports = async ({ github, context, core, severity, workflowRunUrl, commit, branch }) => {
    // Validate required parameters
    const validSeverities = ['critical', 'high', 'moderate', 'low'];
    if (!severity || !validSeverities.includes(severity)) {
        const error = `Invalid severity: '${severity}'. Must be one of: ${validSeverities.join(', ')}`;
        core.setFailed(`❌ ${error}`);
        throw new Error(error);
    }

    if (!workflowRunUrl || typeof workflowRunUrl !== 'string' || workflowRunUrl.trim().length === 0) {
        const error = 'Invalid workflow run URL: must be a non-empty string';
        core.setFailed(`❌ ${error}`);
        throw new Error(error);
    }

    if (!workflowRunUrl.startsWith('https://')) {
        const error = `Invalid workflow run URL scheme: '${workflowRunUrl}'. Must start with https://`;
        core.setFailed(`❌ ${error}`);
        throw new Error(error);
    }

    if (!commit || !/^[a-f0-9]{7,40}$/i.test(commit)) {
        const error = `Invalid commit SHA: '${commit}'. Must be 7-40 hexadecimal characters`;
        core.setFailed(`❌ ${error}`);
        throw new Error(error);
    }

    if (!branch || typeof branch !== 'string' || branch.trim().length === 0) {
        const error = 'Invalid branch name: must be a non-empty string';
        core.setFailed(`❌ ${error}`);
        throw new Error(error);
    }

    const rbacAnalysis = await readRbacAnalysis();

    const description = `## Critical Security Issues Detected

The full codebase security scan has detected critical issues.

**Workflow Run:** ${workflowRunUrl}
**Commit:** ${commit}
**Branch:** ${branch}

---

## ⚠️ CONFIDENTIAL: RBAC Attack Scenarios

**WARNING**: This section contains detailed exploitation chains with step-by-step commands.
This information is CONFIDENTIAL and should only be shared with authorized security personnel.

${rbacAnalysis}

---

### Next Steps
1. Review the workflow run logs at the link above
2. Download the **comprehensive-security-report** artifact for detailed findings
3. Check the Security tab for SARIF results (Semgrep, Hadolint)
4. Review individual tool artifacts (gitleaks.json, trufflehog.json, etc.)
5. Triage and remediate findings

### Report Contents
The comprehensive security report includes:
- Executive summary with overall security posture
- Critical/High/Medium/Low findings with file paths and line numbers
- Remediation guidance for each finding
- RBAC privilege chain analysis
- Prioritized recommendations

**IMPORTANT**: This advisory contains CONFIDENTIAL attack scenarios with detailed
exploitation chains. Do NOT share externally or publish. Remains private until manually published.

This is an automated private security advisory created by the security scanning workflow.`;

    try {
        // Create security advisory using GitHub API
        // SDK automatically handles: retry logic, rate limiting, authentication
        // Note: Input validation performed by caller (see parameter validation above)
        const response = await github.rest.securityAdvisories.createRepositoryAdvisory({
            owner: context.repo.owner,
            repo: context.repo.repo,
            summary: 'Security Full Scan - Critical Findings Detected',
            description: description,
            severity: severity,
            vulnerabilities: [{
                package: {
                    ecosystem: 'other',
                    name: context.repo.repo
                },
                vulnerable_version_range: 'all versions',
                patched_versions: 'none'
            }]
        });

        console.log(`✅ Created security advisory: ${response.data.html_url}`);
        core.setOutput('advisory-url', response.data.html_url);
    } catch (error) {
        core.setFailed(`❌ Failed to create security advisory: ${error.message}`);
        throw error;
    }
};
