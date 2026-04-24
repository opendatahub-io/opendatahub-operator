#!/usr/bin/env bash
# Validates a Jira API token by calling the /myself endpoint.
# Outputs "valid=true" or "valid=false" to GITHUB_OUTPUT (if set) or stdout.
#
# Usage: validate-jira-token.sh
# Env: QUARANTINE_JIRA_API_TOKEN — the token to validate
#      JIRA_BASE_URL — Jira server URL (default: https://redhat.atlassian.net)

set -euo pipefail

JIRA_BASE_URL="${JIRA_BASE_URL:-https://redhat.atlassian.net}"

if [ -z "${QUARANTINE_JIRA_API_TOKEN:-}" ]; then
  echo "No QUARANTINE_JIRA_API_TOKEN secret configured — skipping Jira integration"
  result="false"
else
  HTTP_CODE=$(curl -s -o /dev/null -w '%{http_code}' \
    -H "Authorization: Bearer $QUARANTINE_JIRA_API_TOKEN" \
    -H "Content-Type: application/json" \
    "${JIRA_BASE_URL}/rest/api/2/myself")

  if [ "$HTTP_CODE" = "200" ]; then
    echo "Jira token is valid"
    result="true"
  else
    echo "::warning::Jira token returned HTTP $HTTP_CODE — token may be expired. Rotate at https://id.atlassian.com/manage-profile/security/api-tokens" >&2
    result="false"
  fi
fi

if [ -n "${GITHUB_OUTPUT:-}" ]; then
  echo "valid=${result}" >> "$GITHUB_OUTPUT"
else
  echo "valid=${result}"
fi
