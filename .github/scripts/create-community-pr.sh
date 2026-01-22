#!/bin/bash

# Script to create a PR from fork to upstream community-operators-prod

set -euo pipefail

: "${GITHUB_TOKEN:?GITHUB_TOKEN is required}"
: "${VERSION:?VERSION is required}"
: "${COMMUNITY_FORK:?COMMUNITY_FORK is required}"
: "${COMMUNITY_UPSTREAM:?COMMUNITY_UPSTREAM is required}"
: "${BUNDLE_DIR:?BUNDLE_DIR is required (path to community-operators-prod directory)}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
"${SCRIPT_DIR}/validate-semver.sh" "v${VERSION}"

BRANCH_NAME="odh-operator-${VERSION}"
COMMITTER_NAME="ODH Release Bot"
COMMITTER_EMAIL="noreply@opendatahub.io"

echo "    Creating PR from ${COMMUNITY_FORK} to ${COMMUNITY_UPSTREAM}"
echo "    Version: ${VERSION}"
echo "    Branch: ${BRANCH_NAME}"

cd "${BUNDLE_DIR}"

git config user.name "${COMMITTER_NAME}"
git config user.email "${COMMITTER_EMAIL}"

echo " Creating branch: ${BRANCH_NAME}"
git checkout -b "${BRANCH_NAME}"

echo " Staging changes"
git add .

if git diff --cached --quiet; then
  echo "Error: No changes to commit"
  exit 1
fi

COMMIT_MSG="Release opendatahub-operator version ${VERSION}"
echo " Committing: ${COMMIT_MSG}"
git commit -s -m "${COMMIT_MSG}"

echo " Pushing to fork: ${COMMUNITY_FORK}:${BRANCH_NAME}"
git push --force-with-lease "https://x-access-token:${GITHUB_TOKEN}@github.com/${COMMUNITY_FORK}.git" "${BRANCH_NAME}"

echo " Creating pull request"

PR_TITLE="operator opendatahub-operator (${VERSION})"
PR_HEAD="${COMMUNITY_FORK%%/*}:${BRANCH_NAME}"
UPSTREAM_REPO="${COMMUNITY_UPSTREAM}"

PAYLOAD=$(cat <<EOF
{
  "title": "${PR_TITLE}",
  "head": "${PR_HEAD}",
  "base": "main"
}
EOF
)

RESPONSE=$(curl -f -s -X POST \
  -H "Authorization: Bearer ${GITHUB_TOKEN}" \
  -H "Accept: application/vnd.github+json" \
  -H "X-GitHub-Api-Version: 2022-11-28" \
  "https://api.github.com/repos/${UPSTREAM_REPO}/pulls" \
  -d "${PAYLOAD}")

if ! echo "${RESPONSE}" | jq -e '.html_url and .number' > /dev/null 2>&1; then
  echo "Error: Invalid API response structure" >&2
  echo "Response:" >&2
  echo "${RESPONSE}" | jq . 2>/dev/null || echo "${RESPONSE}" >&2
  exit 1
fi

PR_URL=$(echo "${RESPONSE}" | jq -r '.html_url')
PR_NUMBER=$(echo "${RESPONSE}" | jq -r '.number')

echo "    Successfully created PR"
echo "    URL: ${PR_URL}"
echo "    Number: ${PR_NUMBER}"

if [ -n "${GITHUB_OUTPUT:-}" ]; then
  echo "pull-request-url=${PR_URL}" >> "${GITHUB_OUTPUT}"
  echo "pull-request-number=${PR_NUMBER}" >> "${GITHUB_OUTPUT}"
fi
