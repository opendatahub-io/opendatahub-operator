#!/bin/bash

set -o allexport
set +o allexport

mkdir -p /results

gotestsum --verbose --format standard-verbose --junitfile results/xunit_report.xml

sleep 120s