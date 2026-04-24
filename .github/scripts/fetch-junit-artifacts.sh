#!/usr/bin/env bash
# Fetches JUnit XML artifacts from a GCS-backed Prow job.
#
# Usage: fetch-junit-artifacts.sh [max_builds]
# Env:
#   PROW_JOB       — Prow job name (required)
#   GCS_BUCKET     — GCS bucket name (required)
#   GITHUB_OUTPUT  — GitHub Actions output file (optional)
#
# Outputs (via GITHUB_OUTPUT or stdout):
#   count — number of downloaded JUnit files
#   dir   — path to directory containing downloaded files

set -euo pipefail

MAX_BUILDS="${1:-30}"
JUNIT_DIR="$(mktemp -d)"

BUILDS=$(curl -sf \
  "https://storage.googleapis.com/storage/v1/b/${GCS_BUCKET}/o?prefix=logs/${PROW_JOB}/&delimiter=/&maxResults=1000" \
  | jq -r '.prefixes // [] | .[]' \
  | grep -oP '\d+(?=/)' \
  | sort -rn \
  | head -n "$MAX_BUILDS")

if [ -z "$BUILDS" ]; then
  echo "No builds found for ${PROW_JOB} — periodic job may not have run yet"
  if [ -n "${GITHUB_OUTPUT:-}" ]; then
    echo "count=0" >> "$GITHUB_OUTPUT"
  fi
  exit 0
fi

# ci-operator places step artifacts at different paths depending on
# version; try the two known layouts for each build.
ARTIFACT_PATHS=(
  "opendatahub-operator-e2e-periodic/e2e/junit_report.xml"
  "opendatahub-operator-e2e-periodic/e2e/artifacts/junit_report.xml"
)

count=0
for build_id in $BUILDS; do
  output_file="${JUNIT_DIR}/junit_${build_id}.xml"
  for subpath in "${ARTIFACT_PATHS[@]}"; do
    artifact_url="https://storage.googleapis.com/${GCS_BUCKET}/logs/${PROW_JOB}/${build_id}/artifacts/${subpath}"
    if curl -sfL "$artifact_url" -o "$output_file" 2>/dev/null; then
      count=$((count + 1))
      break
    fi
  done
done

echo "Downloaded ${count} JUnit files from ${MAX_BUILDS} most recent builds"
if [ -n "${GITHUB_OUTPUT:-}" ]; then
  echo "count=${count}" >> "$GITHUB_OUTPUT"
  echo "dir=${JUNIT_DIR}" >> "$GITHUB_OUTPUT"
else
  echo "count=${count}"
  echo "dir=${JUNIT_DIR}"
fi
