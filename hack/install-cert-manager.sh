#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
HELM="${1:?helm binary path is required as the first argument}"

if kubectl get crd certificates.cert-manager.io >/dev/null 2>&1; then
	echo "cert-manager CRDs already present, skipping installation"
	exit 0
fi

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

(
	cd "$REPO_ROOT"
	go run -C ./cmd/manifest-tools main.go download \
		--config "$REPO_ROOT/hack/cert-manager-config.yaml" \
		--charts-dir "$tmpdir"
)

"$HELM" upgrade --install cert-manager-operator "$tmpdir/cert-manager-operator" --create-namespace --take-ownership
kubectl rollout status deployment/cert-manager-operator-controller-manager -n cert-manager-operator --timeout=120s

for deployment in cert-manager cert-manager-webhook cert-manager-cainjector; do
	echo "Waiting for deployment/$deployment in cert-manager namespace..."
	deadline=$(( $(date +%s) + 300 ))
	while ! kubectl rollout status "deployment/$deployment" -n cert-manager --timeout=10s 2>/dev/null; do
		if (( $(date +%s) >= deadline )); then
			echo "Timed out waiting for deployment/$deployment" >&2
			exit 1
		fi
		sleep 5
	done
done
