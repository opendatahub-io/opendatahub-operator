#!/bin/bash
set -euo pipefail

echo "==================================="
echo "MaaS ModuleHandler E2E Test Script"
echo "==================================="
echo ""

# Configuration
export IMG=quay.io/rh-ee-sbhatnag/opendatahub-operator:dev
export MAAS_IMG=quay.io/rh-ee-sbhatnag/maas-controller:dev-29106c0d

echo "📋 Test Configuration:"
echo "  ODH Operator Image: ${IMG}"
echo "  MaaS Controller Image: ${MAAS_IMG}"
echo ""

# Check cluster access
echo "✅ Step 1: Verifying cluster access..."
oc whoami || { echo "❌ Not logged into OpenShift cluster"; exit 1; }
echo "   Cluster: $(oc whoami --show-server)"
echo "   User: $(oc whoami)"
echo ""

# Deploy ODH operator
echo "🚀 Step 2: Deploying ODH operator..."
make deploy IMG=${IMG}
echo ""

# Wait for operator
echo "⏳ Step 3: Waiting for operator to be ready..."
oc wait --for=condition=Available \
  deployment/opendatahub-operator-controller-manager \
  -n opendatahub-operator-system \
  --timeout=600s
echo "   ✅ Operator is ready"
echo ""

# Check operator logs for module registration
echo "🔍 Step 4: Verifying ModuleHandler registration..."
sleep 5
oc logs -n opendatahub-operator-system \
  deployment/opendatahub-operator-controller-manager \
  --tail=100 | grep -i "modelsasservice" || echo "   ⚠️  No modelsasservice logs yet"
echo ""

# Setup E2E cluster
echo "🔧 Step 5: Setting up E2E cluster..."
make e2e-setup-cluster
echo "   ✅ E2E cluster setup complete"
echo ""

# Run MaaS E2E tests
echo "🧪 Step 6: Running MaaS E2E tests..."
if make e2e-test \
  -e E2E_TEST_COMPONENT="modelsasservice" \
  -e E2E_TEST_SERVICES=false \
  -e E2E_TEST_WEBHOOK=false \
  -e E2E_TEST_OPERATOR_RESILIENCE=false \
  -e E2E_TEST_OPERATOR_CONTROLLER=false \
  -e E2E_TEST_DEPENDANT_OPERATORS_MANAGEMENT=false \
  -e E2E_TEST_OPERATOR_V2TOV3UPGRADE=false \
  -e E2E_TEST_DSC_MANAGEMENT=false \
  -e E2E_TEST_DSC_VALIDATION=false \
  -e E2E_TEST_DELETION_POLICY=never \
  -e E2E_TEST_CLEAN_UP_PREVIOUS_RESOURCES=false; then
  TEST_RESULT=0
else
  TEST_RESULT=$?
fi
echo ""

if [ $TEST_RESULT -eq 0 ]; then
  echo "✅ E2E tests PASSED!"
else
  echo "❌ E2E tests FAILED (exit code: $TEST_RESULT)"
fi
echo ""

# Manual verification
echo "==================================="
echo "📊 Manual Verification"
echo "==================================="
echo ""

echo "1. Check ModelsAsService CR:"
oc get modelsasservice -A
echo ""

echo "2. Find applications namespace:"
APPS_NS=$(oc get deployment --all-namespaces --field-selector metadata.name=maas-controller -o jsonpath='{.items[*].metadata.namespace}' 2>/dev/null || echo "")
# Validate we got exactly one namespace
if [ -z "${APPS_NS}" ]; then
  echo "   ⚠️  maas-controller deployment not found"
  APPS_NS="NOT_FOUND"
elif [ "$(echo "${APPS_NS}" | wc -w)" -ne 1 ]; then
  echo "   ❌ Multiple maas-controller deployments found: ${APPS_NS}"
  APPS_NS="NOT_FOUND"
fi

if [ "${APPS_NS}" != "NOT_FOUND" ]; then
  echo "   Applications namespace: ${APPS_NS}"
  echo ""

  echo "3. Check maas-controller Deployment:"
  oc get deployment maas-controller -n "${APPS_NS}"
  echo ""

  echo "4. Verify APPLICATIONS_NAMESPACE env var:"
  oc get deployment maas-controller -n "${APPS_NS}" -o yaml \
    | grep -A 5 APPLICATIONS_NAMESPACE | head -10
  echo ""

  echo "5. Check maas-api Deployment:"
  oc get deployment maas-api -n "${APPS_NS}"
  echo ""

  echo "6. Check Tenant CR status:"
  oc get tenant -n models-as-a-service default-tenant -o yaml | grep -A 3 "phase:"
fi
echo ""

echo "==================================="
echo "✅ Test execution complete!"
echo "==================================="
echo ""
echo "See TESTING_CHECKLIST.md for detailed verification steps."

exit $TEST_RESULT
