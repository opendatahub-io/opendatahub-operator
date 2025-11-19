#!/bin/bash

set -o allexport
set +o allexport

mkdir -p /results

gotestsum ./tests/e2e/ --verbose --junitfile results/xunit_report.xml