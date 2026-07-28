#!/bin/bash
set -ex

kubectl delete configmap model-metadata || true
kubectl delete job my-async-upload-job || true

if [[ -e model-metadata.yaml ]]; then
  kubectl apply -f model-metadata.yaml,job.yaml
else
  kubectl apply -f job.yaml
fi

sleep 2

kubectl logs job/my-async-upload-job -f
