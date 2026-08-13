#!/bin/bash
set -e

pod_info=$(oc get pod -l job-name=my-async-upload-job -o jsonpath='{.items[0]}')

echo "service-account: $(jq -r .spec.serviceAccountName <<< "$pod_info")"
echo "namespace: $(jq -r .metadata.namespace <<< "$pod_info")"
