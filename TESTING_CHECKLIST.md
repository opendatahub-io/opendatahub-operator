# MaaS ModuleHandler Integration Testing Checklist

## Test Environment
- **ROSA Cluster**: ✅ Ready
- **maas-controller image**: `quay.io/rh-ee-sbhatnag/maas-controller:dev-29106c0d`
- **ODH operator image**: `quay.io/rh-ee-sbhatnag/opendatahub-operator:dev`
- **Branch (MaaS)**: `somya-migrate-maas-to-ModuleHandler` (commit `5b75a637`)
- **Branch (ODH)**: `somya-maas-modulehandler-migration` (commit `dc9928a5`)

---

## Pre-Deployment Checklist

- [x] maas-controller PR created (#980)
- [x] maas-controller image built with APPLICATIONS_NAMESPACE support
- [x] maas-controller image pushed to Quay.io
- [ ] ODH operator image built with ModuleHandler
- [ ] ODH operator image pushed to Quay.io
- [x] get_all_manifests.sh updated to point to maas fork
- [x] Unit tests passing (6/6)
- [x] Integration tests passing (6/6)

---

## Deployment Steps

### 1. Deploy ODH Operator
```bash
# Change to the opendatahub-operator repository root
cd <path-to-opendatahub-operator-repo>

# Deploy operator
IMG=quay.io/rh-ee-sbhatnag/opendatahub-operator:dev make deploy

# Wait for operator ready
oc wait --for=condition=Available \
  deployment/opendatahub-operator-controller-manager \
  -n opendatahub-operator-system \
  --timeout=600s

# Verify operator logs
oc logs -n opendatahub-operator-system \
  deployment/opendatahub-operator-controller-manager \
  | grep -i modelsasservice
```

**Expected**:
- ✅ Operator deploys successfully
- ✅ Logs show "modelsasservice" in module registry
- ✅ No errors about missing ModuleHandler

---

### 2. Setup E2E Cluster
```bash
make e2e-setup-cluster
```

**Expected**:
- ✅ Cluster prerequisites installed
- ✅ Required namespaces created

---

### 3. Run MaaS E2E Tests
```bash
make e2e-test \
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
  -e E2E_TEST_CLEAN_UP_PREVIOUS_RESOURCES=false
```

---

## Verification Checklist

### A. ModelsAsService CR Creation
```bash
oc get modelsasservice -A
```
**Expected**:
- ✅ CR `default-modelsasservice` exists
- ✅ Status shows Ready condition

### B. maas-controller Deployment
```bash
# Find applications namespace (should be from DSC/DSCI)
APPS_NS=$(oc get deployment -A | grep maas-controller | awk '{print $1}')

# Check deployment
oc get deployment maas-controller -n ${APPS_NS}

# Verify APPLICATIONS_NAMESPACE env var
oc get deployment maas-controller -n ${APPS_NS} -o yaml \
  | grep -A 5 APPLICATIONS_NAMESPACE

# Check logs
oc logs -n ${APPS_NS} deployment/maas-controller | head -50
```

**Expected**:
- ✅ Deployment exists and is 1/1 Ready
- ✅ APPLICATIONS_NAMESPACE env var present (downward API: metadata.namespace)
- ✅ No error about missing namespace
- ✅ Logs show successful namespace resolution

### C. maas-api Deployment
```bash
oc get deployment maas-api -n ${APPS_NS}
oc get svc maas-api -n ${APPS_NS}
```

**Expected**:
- ✅ maas-api deployed in correct namespace
- ✅ Service exists

### D. Tenant CR Status
```bash
oc get tenant -n models-as-a-service default-tenant -o yaml
```

**Expected**:
- ✅ Status.Phase = "Active"
- ✅ Ready condition = True
- ✅ No errors in status

### E. Platform Integration
```bash
# Check DSC status reflects MaaS
oc get datasciencecluster default-dsc -o yaml | grep -A 20 modelsAsService
```

**Expected**:
- ✅ MaaS status populated in DSC
- ✅ ManagementState matches DSC spec

---

## Troubleshooting Commands

### If ModelsAsService CR doesn't appear:
```bash
# Check operator logs for module registration
oc logs -n opendatahub-operator-system \
  deployment/opendatahub-operator-controller-manager \
  | grep -i "modelsasservice\|module"

# Check DSC status
oc get dsc default-dsc -o yaml

# Verify KServe is enabled
oc get dsc default-dsc -o jsonpath='{.spec.components.kserve.managementState}'
```

### If maas-controller fails to start:
```bash
# Check Deployment events
oc describe deployment maas-controller -n ${APPS_NS}

# Check Pod events
oc get pods -n ${APPS_NS} -l app=maas-controller
oc describe pod <pod-name> -n ${APPS_NS}

# Check logs
oc logs -n ${APPS_NS} <pod-name>
```

### If APPLICATIONS_NAMESPACE not set:
```bash
# This should NOT happen - if it does, ModuleHandler injection failed
oc get deployment maas-controller -n ${APPS_NS} -o yaml \
  | grep -B 5 -A 10 APPLICATIONS_NAMESPACE

# Expected: env var with valueFrom.fieldRef.fieldPath: metadata.namespace
```

---

## Success Criteria

- [ ] ✅ ModelsAsService CR created automatically by ModuleHandler
- [ ] ✅ maas-controller Deployment has APPLICATIONS_NAMESPACE env var
- [ ] ✅ maas-controller successfully reads namespace from APPLICATIONS_NAMESPACE
- [ ] ✅ maas-api deployed to correct namespace
- [ ] ✅ Tenant CR reaches Active status
- [ ] ✅ All E2E tests pass
- [ ] ✅ No regression in existing MaaS functionality

---

## Test Results

### Build Results
- **maas-controller build**: ✅ Success (310 MB image)
- **maas-controller push**: ✅ Success
- **ODH operator build**: ⏳ In progress
- **ODH operator push**: ⏳ Pending

### Deployment Results
- **ODH operator deployment**: ⏳ Pending
- **E2E cluster setup**: ⏳ Pending
- **MaaS E2E tests**: ⏳ Pending

### Verification Results
- **ModelsAsService CR**: ⏳ Pending
- **maas-controller Deployment**: ⏳ Pending
- **APPLICATIONS_NAMESPACE env var**: ⏳ Pending
- **maas-api Deployment**: ⏳ Pending
- **Tenant CR status**: ⏳ Pending

---

## Notes

- MaaS manifests fetched from `somya-bhatnagar:models-as-a-service:somya-migrate-maas-to-ModuleHandler@5b75a637`
- Testing ModuleHandler framework (NOT ComponentHandler)
- ModuleHandler uses Kustomize (not Helm as originally planned)
- APPLICATIONS_NAMESPACE injected via module framework pipeline action
