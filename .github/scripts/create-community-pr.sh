#!/bin/bash

# Script to create a PR from fork to upstream community-operators-prod

set -euo pipefail

: "${VERSION:?VERSION is required}"
: "${COMMUNITY_FORK:?COMMUNITY_FORK is required}"
: "${COMMUNITY_UPSTREAM:?COMMUNITY_UPSTREAM is required}"
: "${BUNDLE_DIR:?BUNDLE_DIR is required (path to community-operators-prod directory)}"
: "${BRANCH_PREFIX:?BRANCH_PREFIX is required}"
: "${COMMITTER_NAME:?COMMITTER_NAME is required}"
: "${COMMITTER_EMAIL:?COMMITTER_EMAIL is required}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
"${SCRIPT_DIR}/validate-semver.sh" "v${VERSION}"

BRANCH_NAME="${BRANCH_PREFIX}${VERSION}"

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

git push --force-with-lease origin "${BRANCH_NAME}"

echo " Creating pull request"

PR_TITLE="operator opendatahub-operator (${VERSION})"
PR_HEAD="${COMMUNITY_FORK%%/*}:${BRANCH_NAME}"

PR_URL=$(gh pr create \
  --repo "${COMMUNITY_UPSTREAM}" \
  --title "${PR_TITLE}" \
  --head "${PR_HEAD}" \
  --base main \
  --body "" \
  --json url \
  --jq '.url')

if [ -z "${PR_URL}" ]; then
  echo "Error: Failed to create pull request" >&2
  exit 1
fi

# Extract PR number from URL (e.g., https://github.com/owner/repo/pull/123 -> 123)
PR_NUMBER=$(echo "${PR_URL}" | grep -oE '[0-9]+$')

echo "    Successfully created PR"
echo "    URL: ${PR_URL}"
echo "    Number: ${PR_NUMBER}"

if [ -n "${GITHUB_OUTPUT:-}" ]; then
  echo "pull-request-url=${PR_URL}" >> "${GITHUB_OUTPUT}"
  echo "pull-request-number=${PR_NUMBER}" >> "${GITHUB_OUTPUT}"
fi
