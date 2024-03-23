#!/bin/bash
#
# @param $1 - PR number or URL
# wait for a bit until pr is created, otherwise it throws an error "no checks reported on the 'odh-release/e2e-test' branch"
set -euo pipefail

sleep 10

while [ $(gh pr checks "$1" | grep -v 'tide' | grep 'pending' | wc -m) -gt 0 ]; do
  printf "PR checks still pending, retrying in 10 seconds...\n"
  sleep 30 # replace with 60000
done

if [ $(gh pr checks "$1" | grep 'fail' | wc -m) -gt 0 ]; then
  printf "!!PR checks failed!!\n"
  exit 1
fi

if [ $(gh pr checks "$1" | grep 'pass' | wc -m) -gt 0 ]; then
  printf "!!PR checks passed!!\n"
  exit 0
fi

printf "!!An unknown error occurred!!\n"
exit 1