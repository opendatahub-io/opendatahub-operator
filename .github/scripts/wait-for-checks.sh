#!/bin/bash
#
# @param $1 - PR number or URL
# wait for a bit until pr is created, otherwise it throws an error "no checks reported on the 'odh-release/e2e-test' branch"
set -euo

sleep 10

pr_has_status() {
    local pr=$1
    local status=$2
    local skip=${3:-tide}
    # This logic is essential because we need to strip-off "tide" checks otherwise the pr will be in pending state until someone approves.
    gh pr checks $pr | awk -v FS=$'\t' -v status=$status "\$1 ~ /$skip/{next} \$2 == status {found=1} END {if (!found) exit 1}"
}

# waiting for 5 minutes before each check as e2e can take a lot of time.
while pr_has_status $1 pending; do
  echo "PR checks still pending, retrying in 5 minutes..."
  sleep 5m
done

pr_has_status $1 fail && { echo "!!PR checks failed!!"; exit 1; }
pr_has_status $1 pass && { echo "!!PR checks passed!!"; exit 0; }
echo "!!An unknown error occurred!!"
exit 1