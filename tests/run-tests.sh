#!/bin/bash

set -o allexport
set +o allexport

mkdir -p /results

gotestsum ./tests/e2e/ --debug --format testname --junitfile results/xunit_report.xml