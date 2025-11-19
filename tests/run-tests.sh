#!/bin/bash

set -o allexport
set +o allexport

mkdir -p /results

go run -C ./cmd/test-retry main.go e2e --verbose --junit-output=results/xunit_report.xml