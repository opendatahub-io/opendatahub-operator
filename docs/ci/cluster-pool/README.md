# Cluster Pool Migration: IPI -> Hive Cluster Pools

**JIRA**: RHOAIENG-46694
**Goal**: Remove 25-40 min of IPI cluster provisioning from the E2E test critical path by
switching to pre-provisioned Hive cluster pools.

---

## Table of Contents

1. [Background](#background)
2. [Architecture Overview](#architecture-overview)
3. [AWS Account Details](#aws-account-details)
4. [Phase 1 -- AWS Account Preparation](#phase-1----aws-account-preparation)
   - [1.1 Verify Route 53 Hosted Zone](#11-verify-route-53-hosted-zone)
   - [1.2 Create IAM User with Static Credentials](#12-create-iam-user-with-static-credentials)
   - [1.3 Verify EC2 and Service Quotas](#13-verify-ec2-and-service-quotas)
5. [Phase 2 -- Vault Secret Setup](#phase-2----vault-secret-setup)
   - [2.1 Create a Vault Secret Collection](#21-create-a-vault-secret-collection)
   - [2.2 Create the AWS Credentials Secret](#22-create-the-aws-credentials-secret)
   - [2.3 Verify Secret Sync](#23-verify-secret-sync)
6. [Phase 3 -- Cluster Pool Manifests PR](#phase-3----cluster-pool-manifests-pr)
   - [3.1 Fork and Clone openshift/release](#31-fork-and-clone-openshiftrelease)
   - [3.2 Finalize Manifests](#32-finalize-manifests)
   - [3.3 File Inventory](#33-file-inventory)
   - [3.4 Submit the PR](#34-submit-the-pr)
   - [3.5 Wait for Merge and Pool Bootstrap](#35-wait-for-merge-and-pool-bootstrap)
7. [Phase 4 -- Validate the Pool](#phase-4----validate-the-pool)
   - [4.1 Check Pool Status](#41-check-pool-status)
   - [4.2 Debug Installation Failures](#42-debug-installation-failures)
8. [Phase 5 -- Switch CI Jobs to Cluster Pool](#phase-5----switch-ci-jobs-to-cluster-pool)
   - [5.1 ci-operator Config Changes](#51-ci-operator-config-changes)
   - [5.2 Full Diff: main Branch](#52-full-diff-main-branch)
   - [5.3 Full Diff: stable-2.x Branch](#53-full-diff-stable-2x-branch)
   - [5.4 Submit and Validate](#54-submit-and-validate)
9. [Pool Sizing Rationale](#pool-sizing-rationale)
10. [Operational Runbook](#operational-runbook)
    - [Scaling Pool Up/Down](#scaling-pool-updown)
    - [Rotating AWS Credentials](#rotating-aws-credentials)
    - [Adding a New OCP Version Pool](#adding-a-new-ocp-version-pool)
    - [Decommissioning a Pool](#decommissioning-a-pool)
11. [Troubleshooting](#troubleshooting)
12. [References](#references)

---

## Background

Currently, every opendatahub-operator E2E CI job provisions a fresh GCP cluster via IPI
(`cluster_profile: gcp-opendatahub` with the `optional-operators-ci-operator-sdk-gcp` workflow).
This adds 25-40 minutes to every test run just for cluster installation.

Hive cluster pools maintain a set of pre-provisioned, hibernating OCP clusters. When a CI job
claims a cluster, Hive wakes it from hibernation (~3-6 min) or provides an already-running one
(~instant). This eliminates the IPI install from the critical path.

Key characteristics of cluster pools vs. IPI:
- **Same cluster topology** as IPI (standard control plane + workers) -- unlike HyperShift
- **Clusters are destroyed 4 hours after claim** by CI infrastructure
- **Hibernating clusters consume minimal cloud cost** (EBS volumes only, no running instances)
- **Pool auto-replenishes** -- Hive creates a new cluster when one is claimed

A prior HyperShift-based attempt (RHOAIENG-31926) was reverted because HyperShift uses hosted
control planes, which caused test failures due to topology differences. Cluster pools avoid
this entirely.

---

## Architecture Overview

```
                            openshift/release repo (GitOps)
                                      |
                                      v
                         hosted-mgmt cluster (Hive)
                                      |
                    +------------------+------------------+
                    |                                     |
        ClusterPool "4.19"                   ClusterPool "4.18"
        namespace: opendatahub-cluster-pool
        AWS account: 585132637328
                    |                                     |
          +---------+---------+                  +--------+--------+
          |         |         |                  |                 |
        CD-aaa    CD-bbb    CD-ccc             CD-ddd           CD-eee
       (ready)  (hibernating) (claimed)       (ready)         (claimed)
                    |                                     |
                    v                                     v
              AWS us-east-1                         AWS us-east-1


    CI Job runs  -->  ClusterClaim  -->  Hive fulfills  -->  Job gets KUBECONFIG
                                         from pool           and runs E2E tests
```

---

## AWS Account Details

| Field | Value |
|-------|-------|
| Account Name | `?` |
| Account ID | `?` |
| Role Name | `?` |
| Role ARN | `?` |
| Target Region | `us-east-1` |

---

## Phase 1 -- AWS Account Preparation

### 1.1 Verify Route 53 Hosted Zone

Every cluster in the pool needs a unique DNS name under a base domain. The base domain must have
a **public Route 53 hosted zone** in the AWS account.

```bash
# Assume the role (or use your preferred auth method)
export AWS_PROFILE=?  # or use assume-role

# List hosted zones
aws route53 list-hosted-zones --query 'HostedZones[*].[Name,Id,Config.PrivateZone]' --output table
```

**If a public hosted zone already exists** (e.g. `rhaiseng.com` or `openshift-ci-aws.rhaiseng.com`):
- Note the zone name -- this becomes your `baseDomain` in the manifests
- Hive will create sub-zones like `<cluster-name>.<baseDomain>` for each cluster

**If no public hosted zone exists**, create one:

```bash
# Option A: Use a subdomain of an existing domain you control
aws route53 create-hosted-zone \
  --name "odh-ci-pool.rhaiseng.com" \
  --caller-reference "odh-cluster-pool-$(date +%s)"

# Then set up NS delegation from the parent zone (rhaiseng.com) to this zone.
# Get the NS records from the new zone:
aws route53 get-hosted-zone --id <ZONE_ID> --query 'DelegationSet.NameServers'

# Add these as NS records in the parent zone for "odh-ci-pool.rhaiseng.com"
```

**Record your base domain** -- you'll need it in later steps (referred to as `<BASE_DOMAIN>`).

### 1.2 Create IAM User with Static Credentials

Hive requires static AWS credentials (`aws_access_key_id` + `aws_secret_access_key`) to
provision and manage clusters. It cannot use IAM role ARNs directly.

**Create a dedicated IAM user:**

```bash
# Create the user
aws iam create-user --user-name opendatahub-ci-hive

# Attach the required policies
# Option A: Use the managed AdministratorAccess (simplest, broadest)
aws iam attach-user-policy \
  --user-name opendatahub-ci-hive \
  --policy-arn arn:aws:iam::aws:policy/AdministratorAccess

# Option B: Use a custom policy with minimum OpenShift installer permissions
# See: https://docs.openshift.com/container-platform/4.19/installing/installing_aws/installing-aws-account.html#installation-aws-permissions_installing-aws-account
# This is more secure but requires maintaining the policy document.

# Generate access keys
aws iam create-access-key --user-name opendatahub-ci-hive
```

**Save the output** -- you need both `AccessKeyId` and `SecretAccessKey` for the Vault secret.

> **Security note**: The `AdministratorAccess` approach is what most CI pools use (rhdh,
> serverless, etc.) because OpenShift installation requires broad permissions across EC2, ELB,
> Route53, S3, IAM, and VPC. If your security posture requires least-privilege, use the
> [documented installer permissions](https://docs.openshift.com/container-platform/4.19/installing/installing_aws/installing-aws-account.html#installation-aws-permissions_installing-aws-account).

### 1.3 Verify EC2 and Service Quotas

Each OCP cluster requires approximately:
- **3 control plane nodes**: `m5.xlarge` (4 vCPU each) = 12 vCPUs
- **3 worker nodes**: `m5.2xlarge` (8 vCPU each) = 24 vCPUs
- **1 bootstrap node** (temporary, during install only): `m5.xlarge` = 4 vCPUs
- **Total per cluster**: ~36-40 vCPUs at install time, ~36 vCPUs steady state
- **EBS volumes**: ~6 * 120GB = 720GB per cluster
- **Elastic IPs**: ~3 per cluster
- **VPCs**: 1 per cluster
- **NAT Gateways**: up to 3 per cluster (one per AZ)

With our planned pools (`maxSize=4` for 4.19, `maxSize=2` for 4.18), worst case is
**6 concurrent clusters = ~216 vCPUs**.

```bash
# Check running on-demand instance vCPU limit
aws service-quotas get-service-quota \
  --service-code ec2 \
  --quota-code L-1216C47A \
  --region us-east-1 \
  --query 'Quota.Value'

# Check Elastic IP limit
aws service-quotas get-service-quota \
  --service-code ec2 \
  --quota-code L-0263D0A3 \
  --region us-east-1 \
  --query 'Quota.Value'

# Check VPC limit
aws service-quotas get-service-quota \
  --service-code vpc \
  --quota-code L-F678F1CE \
  --region us-east-1 \
  --query 'Quota.Value'

# Check NAT Gateway limit
aws service-quotas get-service-quota \
  --service-code vpc \
  --quota-code L-FE5A380F \
  --region us-east-1 \
  --query 'Quota.Value'

# Check EBS General Purpose SSD (gp3) volume storage (in TiB)
aws service-quotas get-service-quota \
  --service-code ebs \
  --quota-code L-7A658000 \
  --region us-east-1 \
  --query 'Quota.Value'
```

**Minimum recommended quotas for `us-east-1`:**

| Resource | Quota Code | Minimum Needed | Notes |
|----------|-----------|----------------|-------|
| Running On-Demand Standard vCPUs | `L-1216C47A` | 250+ | 6 clusters * ~36 vCPUs + buffer |
| Elastic IPs | `L-0263D0A3` | 20+ | 6 clusters * ~3 EIPs |
| VPCs per region | `L-F678F1CE` | 10+ | 6 clusters + default VPC + buffer |
| NAT Gateways per AZ | `L-FE5A380F` | 20+ | 6 clusters * 3 AZs |
| EBS gp3 storage (TiB) | `L-7A658000` | 10+ | 6 clusters * ~720GB |

If any quota is too low, request an increase via the AWS Service Quotas console. Increases for
`us-east-1` typically take 1-3 business days.

---

## Phase 2 -- Vault Secret Setup

OpenShift CI uses HashiCorp Vault for secret management. Secrets are synced to build-farm
clusters automatically.

### 2.1 Create a Vault Secret Collection

1. Go to [selfservice.vault.ci.openshift.org](https://selfservice.vault.ci.openshift.org/)
2. Log in with your Red Hat SSO credentials
3. Click **"New Collection"**
4. Name: `opendatahub-cluster-pool` (or similar -- must be globally unique)
5. Click **"Submit"**
6. Add your teammates as members (they must have logged in to Vault at least once before they
   appear as potential members)

### 2.2 Create the AWS Credentials Secret

1. Go to [vault.ci.openshift.org](https://vault.ci.openshift.org/)
2. Log in with OIDC (leave Role blank)
3. Click on `kv` in the sidebar
4. Navigate to your collection (e.g. `selfservice/opendatahub-cluster-pool`)
5. Click **"Create secret +"**
6. Set the path to: `selfservice/opendatahub-cluster-pool/aws-credentials`
7. Add the following key-value pairs:

| Key | Value |
|-----|-------|
| `aws_access_key_id` | `<the AccessKeyId from step 1.2>` |
| `aws_secret_access_key` | `<the SecretAccessKey from step 1.2>` |
| `secretsync/target-clusters` | `hosted-mgmt` |
| `secretsync/target-namespace` | `opendatahub-cluster-pool` |
| `secretsync/target-name` | `opendatahub-aws-credentials` |

8. **Switch to JSON mode** (toggle near top of page) to verify all 5 key-value pairs are
   correct and none are blank
9. Click **"Save"**

> **Important**: The `secretsync/target-clusters` value **must** be `hosted-mgmt` (the cluster
> where Hive runs). This is different from the normal `test-credentials` target used for
> regular CI secrets. The `secretsync/target-namespace` must match the namespace in your pool
> manifests.

### 2.3 Verify Secret Sync

Secret propagation typically completes within **30 minutes**. You cannot directly verify this
unless you have access to the `hosted-mgmt` cluster. However, once the pool manifests are
applied (Phase 3), Hive will immediately tell you if the credentials secret is missing.

If you have access to `hosted-mgmt` (via the pool-admins RBAC created later):

```bash
oc --context hosted-mgmt get secret opendatahub-aws-credentials \
  -n opendatahub-cluster-pool
```

---

## Phase 3 -- Cluster Pool Manifests PR

### 3.1 Fork and Clone openshift/release

```bash
# If you don't already have a fork
gh repo fork openshift/release --clone
cd release

# Or if you already have it cloned
cd /path/to/openshift/release
git fetch upstream
git checkout -b opendatahub-cluster-pool upstream/main
```

### 3.2 Finalize Manifests

Copy the manifest files from this directory into the openshift/release repo:

```bash
# Create the pool directory
mkdir -p clusters/hosted-mgmt/hive/pools/opendatahub

# Copy the reference manifests
cp /path/to/opendatahub-operator/docs/ci/cluster-pool/OWNERS \
   clusters/hosted-mgmt/hive/pools/opendatahub/

cp /path/to/opendatahub-operator/docs/ci/cluster-pool/admins_opendatahub-cluster-pool_rbac.yaml \
   clusters/hosted-mgmt/hive/pools/opendatahub/

cp /path/to/opendatahub-operator/docs/ci/cluster-pool/install-config-aws_secret.yaml \
   clusters/hosted-mgmt/hive/pools/opendatahub/

cp /path/to/opendatahub-operator/docs/ci/cluster-pool/opendatahub-ocp-4-19-amd64-aws_clusterpool.yaml \
   clusters/hosted-mgmt/hive/pools/opendatahub/

cp /path/to/opendatahub-operator/docs/ci/cluster-pool/opendatahub-ocp-4-18-amd64-aws_clusterpool.yaml \
   clusters/hosted-mgmt/hive/pools/opendatahub/
```

**Now replace all placeholders** with your actual base domain:

```bash
# Replace the placeholder in all files
BASE_DOMAIN="<your-route53-base-domain>"  # e.g. "odh-ci-pool.rhaiseng.com"

# macOS (BSD sed)
find clusters/hosted-mgmt/hive/pools/opendatahub -type f -name '*.yaml' \
  -exec sed -i '' "s/REPLACE_WITH_YOUR_ROUTE53_DOMAIN/${BASE_DOMAIN}/g" {} +

# Linux (GNU sed)
find clusters/hosted-mgmt/hive/pools/opendatahub -type f -name '*.yaml' \
  -exec sed -i "s/REPLACE_WITH_YOUR_ROUTE53_DOMAIN/${BASE_DOMAIN}/g" {} +
```

**Update the OWNERS file** with actual GitHub usernames who should be able to approve changes
to these pool manifests.

**Review the install-config worker instance type.** The default is `m5.2xlarge` (8 vCPU, 32GB)
to match the current GCP `n2-standard-8` instances. Adjust if needed.

### 3.3 File Inventory

After finalization, the directory should contain exactly these files:

```
clusters/hosted-mgmt/hive/pools/opendatahub/
├── OWNERS                                            # PR approval config
├── admins_opendatahub-cluster-pool_rbac.yaml         # Namespace + RBAC
├── install-config-aws_secret.yaml                    # Cluster install template
├── opendatahub-ocp-4-18-amd64-aws_clusterpool.yaml   # Pool: OCP 4.18 (stable-2.x)
└── opendatahub-ocp-4-19-amd64-aws_clusterpool.yaml   # Pool: OCP 4.19 (main)
```

Quick self-review checklist before submitting:

- [ ] `baseDomain` is set to your actual Route 53 domain in all 3 files
       (install-config + both ClusterPools)
- [ ] `credentialsSecretRef.name` matches the Vault `secretsync/target-name`
       (should be `opendatahub-aws-credentials`)
- [ ] `namespace` is consistent across all files (`opendatahub-cluster-pool`)
- [ ] `imageSetRef.name` references existing ClusterImageSets (verify at
       `clusters/hosted-mgmt/hive/pools/ocp-release-*_clusterimageset.yaml`)
- [ ] `version_lower` / `version_upper` labels match the `imageSetRef` version range
- [ ] `OWNERS` has real GitHub usernames (not placeholder group names)
- [ ] Worker instance type (`m5.2xlarge`) in install-config is correct
- [ ] Region (`us-east-1`) matches your Vault secret and Route 53 zone

### 3.4 Submit the PR

```bash
git add clusters/hosted-mgmt/hive/pools/opendatahub/
git commit -m "Add opendatahub Hive cluster pools for E2E CI

Creates cluster pools for opendatahub-operator E2E jobs to replace
per-run IPI provisioning with pre-provisioned clusters.

- OCP 4.19 pool for main branch (size=1, maxSize=4)
- OCP 4.18 pool for stable-2.x branch (size=1, maxSize=2)
- AWS us-east-1 using account iaps-rhods-odh-dev
- Workers: m5.2xlarge to match current n2-standard-8 GCP nodes

JIRA: RHOAIENG-46694"

git push origin opendatahub-cluster-pool

# Create the PR
gh pr create \
  --repo openshift/release \
  --title "Add opendatahub Hive cluster pools for E2E CI [RHOAIENG-46694]" \
  --body "## Summary

- Adds Hive cluster pools for opendatahub-operator E2E CI jobs
- Replaces per-run IPI provisioning with pre-provisioned clusters
- Saves 25-40 min per E2E job execution
- OCP 4.19 pool for main branch, OCP 4.18 pool for stable-2.x
- AWS us-east-1, account iaps-rhods-odh-dev (585132637328)

## Files Added

\`\`\`
clusters/hosted-mgmt/hive/pools/opendatahub/
├── OWNERS
├── admins_opendatahub-cluster-pool_rbac.yaml
├── install-config-aws_secret.yaml
├── opendatahub-ocp-4-18-amd64-aws_clusterpool.yaml
└── opendatahub-ocp-4-19-amd64-aws_clusterpool.yaml
\`\`\`

## Vault Secret

AWS credentials stored in Vault, syncing to \`hosted-mgmt\` cluster in
namespace \`opendatahub-cluster-pool\` as secret \`opendatahub-aws-credentials\`.

JIRA: RHOAIENG-46694
/cc @openshift/test-platform"
```

> **Note**: The PR will need approval from DPTP (`/cc @openshift/test-platform`) since it
> touches Hive pool infrastructure. Expect them to review the manifests. Tag `#forum-ocp-testplatform`
> on Slack to speed up review.

### 3.5 Wait for Merge and Pool Bootstrap

After the PR merges:

1. The GitOps automation applies your manifests to the `hosted-mgmt` cluster
2. The namespace `opendatahub-cluster-pool` is created
3. Hive reads the ClusterPool resources and begins provisioning clusters
4. First cluster install takes **40-60 minutes** (this is a one-time bootstrap cost)
5. Once installed, the cluster enters `ready` state (or hibernates if idle)

---

## Phase 4 -- Validate the Pool

### 4.1 Check Pool Status

If you're in the `opendatahub-pool-admins` group on `hosted-mgmt`:

```bash
# Check pool status
oc --context hosted-mgmt get clusterpool -n opendatahub-cluster-pool

# Expected output (after bootstrap):
# NAME                              READY   SIZE   STANDBY   MAXSIZE
# opendatahub-ocp-4-19-amd64-aws   1       1      0         4
# opendatahub-ocp-4-18-amd64-aws   1       1      0         2

# Check individual cluster deployments
oc --context hosted-mgmt get clusterdeployment -n opendatahub-cluster-pool

# Check for provisioning failures
oc --context hosted-mgmt get clusterpool -n opendatahub-cluster-pool \
  -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.status}{"\n"}{end}'
```

If you do NOT have access to `hosted-mgmt`, ask in `#forum-ocp-testplatform` for someone to
check the pool status, or wait until Phase 5 and see if test jobs can successfully claim a
cluster.

### 4.2 Debug Installation Failures

If `READY` stays at 0, Hive is failing to install clusters. Inspect the install logs:

```bash
pool_name=opendatahub-ocp-4-19-amd64-aws

# Find the ClusterDeployment namespaces
oc --context hosted-mgmt get namespace \
  --selector hive.openshift.io/cluster-pool-name=${pool_name}

# Pick a namespace and look at the provision pod
namespace=<namespace-from-above>
oc --context hosted-mgmt get pod -n ${namespace}

# Get the install logs
pod=$(oc --context hosted-mgmt get pod -n ${namespace} \
  -l hive.openshift.io/cluster-deployment-name=${namespace} \
  --sort-by=.metadata.creationTimestamp \
  -o jsonpath='{.items[-1].metadata.name}')

oc --context hosted-mgmt logs -n ${namespace} ${pod} -c hive
```

Common issues:
- **"credentials not found"**: Vault secret hasn't synced yet. Wait 30 min and re-check.
- **"hosted zone not found"**: Route 53 zone doesn't exist or `baseDomain` is wrong.
- **"quota exceeded"**: EC2 vCPU or other quota limit hit. Request increase.
- **"imageSet not found"**: The `imageSetRef.name` in the ClusterPool doesn't match an
  existing `ClusterImageSet`. Check `oc get clusterimageset` on `hosted-mgmt`.

> `installAttemptsLimit: 1` is set in our manifests, which means Hive won't retry indefinitely
> on failure. It will delete the failed ClusterDeployment and create a new one. This prevents
> quota exhaustion from perma-failing installs, but it also means install logs disappear with
> the failed ClusterDeployment. Monitor quickly after the PR merges.

---

## Phase 5 -- Switch CI Jobs to Cluster Pool

Once the pool has at least one `READY` cluster, you can update the ci-operator config.

### 5.1 ci-operator Config Changes

The changes needed per E2E job are:

| What | Before (IPI) | After (Cluster Pool) |
|------|-------------|---------------------|
| Cluster source | `cluster_profile: gcp-opendatahub` | `cluster_claim:` stanza (matches pool labels) |
| Workflow | `optional-operators-ci-operator-sdk-gcp` | `generic-claim` |
| Operator install | Built into the workflow | Explicit `pre` step: `optional-operators-operator-sdk` |
| Instance type | `env.COMPUTE_NODE_TYPE: n2-standard-8` | Controlled by pool install-config (removed from job) |
| `cli-operator-sdk` base_image | Still required | Still required (used by operator-sdk step) |

The `generic-claim` workflow provides:
- **Pre**: `ipi-install-rbac` (build-farm RBAC) + `openshift-configure-cincinnati` (update service config)
- **Post**: `gather` chain (must-gather, audit logs, extra artifacts)
- **No test steps** (you provide your own)

Since `generic-claim` does NOT install the operator, we add `optional-operators-operator-sdk`
as an explicit pre-step. This step:
- Runs in the `cli-operator-sdk` image (which must remain in `base_images`)
- Uses the `OO_BUNDLE` dependency to install the operator via `operator-sdk run bundle`
- Respects `OO_INSTALL_NAMESPACE` and `OO_INSTALL_MODE` env vars

> **Important**: When overriding `pre` steps in a workflow, your list **replaces** the
> workflow's pre steps entirely. So we must include `ipi-install-rbac` explicitly.

### 5.2 Full Diff: main Branch

File: `ci-operator/config/opendatahub-io/opendatahub-operator/opendatahub-io-opendatahub-operator-main.yaml`

**ODH E2E job -- before:**
```yaml
- as: opendatahub-operator-e2e
  skip_if_only_changed: ^\.github/|^docs/|\.[mM][dD]$|^.gitignore$|^golangci|^crd-ref-docs\.config|^OWNERS$|^PROJECT$|^LICENSE$|^OWNERS_ALIASES$|^Makefile$|^cmd/[^/]+/|^[^/]+\.(yaml|yml)$|^.gitleaksignore$|^.yamllint$
  steps:
    allow_best_effort_post_steps: true
    allow_skip_on_success: true
    cluster_profile: gcp-opendatahub
    dependencies:
      OO_BUNDLE: opendatahub-operator-bundle
    env:
      COMPUTE_NODE_TYPE: n2-standard-8
      OO_INSTALL_NAMESPACE: openshift-operators
    test:
    - as: e2e
      cli: latest
      commands: |
        unset GOFLAGS
        make e2e-test -e E2E_TEST_FLAGS="-timeout 60m" -e OPERATOR_NAMESPACE=openshift-operators
      from: src
      resources:
        requests:
          cpu: 2000m
          memory: 3Gi
    workflow: optional-operators-ci-operator-sdk-gcp
```

**ODH E2E job -- after:**
```yaml
- as: opendatahub-operator-e2e
  cluster_claim:
    architecture: amd64
    cloud: aws
    owner: opendatahub
    product: ocp
    timeout: 1h0m0s
    version: "4.19"
  skip_if_only_changed: ^\.github/|^docs/|\.[mM][dD]$|^.gitignore$|^golangci|^crd-ref-docs\.config|^OWNERS$|^PROJECT$|^LICENSE$|^OWNERS_ALIASES$|^Makefile$|^cmd/[^/]+/|^[^/]+\.(yaml|yml)$|^.gitleaksignore$|^.yamllint$
  steps:
    allow_best_effort_post_steps: true
    allow_skip_on_success: true
    dependencies:
      OO_BUNDLE: opendatahub-operator-bundle
    env:
      OO_INSTALL_NAMESPACE: openshift-operators
    pre:
    - ref: ipi-install-rbac
    - ref: optional-operators-operator-sdk
    test:
    - as: e2e
      cli: latest
      commands: |
        unset GOFLAGS
        make e2e-test -e E2E_TEST_FLAGS="-timeout 60m" -e OPERATOR_NAMESPACE=openshift-operators
      from: src
      resources:
        requests:
          cpu: 2000m
          memory: 3Gi
    workflow: generic-claim
```

**RHOAI E2E job -- before:**
```yaml
- as: opendatahub-operator-rhoai-e2e
  skip_if_only_changed: ^\.github/|^docs/|\.[mM][dD]$|^.gitignore$|^golangci|^crd-ref-docs\.config|^OWNERS$|^PROJECT$|^LICENSE$|^OWNERS_ALIASES$|^Makefile$|^cmd/[^/]+/|^[^/]+\.(yaml|yml)$|^.gitleaksignore$|^.yamllint$
  steps:
    allow_best_effort_post_steps: true
    allow_skip_on_success: true
    cluster_profile: gcp-opendatahub
    dependencies:
      OO_BUNDLE: opendatahub-operator-rhoai-bundle
    env:
      COMPUTE_NODE_TYPE: n2-standard-8
      OO_INSTALL_NAMESPACE: redhat-ods-operator
    test:
    - as: e2e
      cli: latest
      commands: |
        unset GOFLAGS
        E2E_TEST_DSC_MONITORING_NAMESPACE=redhat-ods-monitoring make e2e-test -e ODH_PLATFORM_TYPE=rhoai -e E2E_TEST_FLAGS="-timeout 60m" -e OPERATOR_NAMESPACE=redhat-ods-operator
      from: src
      resources:
        requests:
          cpu: 2000m
          memory: 3Gi
    workflow: optional-operators-ci-operator-sdk-gcp
```

**RHOAI E2E job -- after:**
```yaml
- as: opendatahub-operator-rhoai-e2e
  cluster_claim:
    architecture: amd64
    cloud: aws
    owner: opendatahub
    product: ocp
    timeout: 1h0m0s
    version: "4.19"
  skip_if_only_changed: ^\.github/|^docs/|\.[mM][dD]$|^.gitignore$|^golangci|^crd-ref-docs\.config|^OWNERS$|^PROJECT$|^LICENSE$|^OWNERS_ALIASES$|^Makefile$|^cmd/[^/]+/|^[^/]+\.(yaml|yml)$|^.gitleaksignore$|^.yamllint$
  steps:
    allow_best_effort_post_steps: true
    allow_skip_on_success: true
    dependencies:
      OO_BUNDLE: opendatahub-operator-rhoai-bundle
    env:
      OO_INSTALL_NAMESPACE: redhat-ods-operator
    pre:
    - ref: ipi-install-rbac
    - ref: optional-operators-operator-sdk
    test:
    - as: e2e
      cli: latest
      commands: |
        unset GOFLAGS
        E2E_TEST_DSC_MONITORING_NAMESPACE=redhat-ods-monitoring make e2e-test -e ODH_PLATFORM_TYPE=rhoai -e E2E_TEST_FLAGS="-timeout 60m" -e OPERATOR_NAMESPACE=redhat-ods-operator
      from: src
      resources:
        requests:
          cpu: 2000m
          memory: 3Gi
    workflow: generic-claim
```

> **Note**: The `opendatahub-operator-e2e-hypershift` job is **out of scope** and should be
> left unchanged. It uses a separate `aws-opendatahub` cluster profile for a different purpose.

### 5.3 Full Diff: stable-2.x Branch

File: `ci-operator/config/opendatahub-io/opendatahub-operator/opendatahub-io-opendatahub-operator-stable-2.x.yaml`

Same pattern as main, with `version: "4.18"` in the `cluster_claim` stanza.

**Before:**
```yaml
- as: opendatahub-operator-e2e
  skip_if_only_changed: ^\.github/|^docs/|\.[mM][dD]$|^.gitignore$|^golangci|^crd-ref-docs\.config|^OWNERS$|^PROJECT$|^LICENSE$|^OWNERS_ALIASES$|^Makefile$|^cmd/[^/]+/|^[^/]+\.(yaml|yml)$|^.gitleaksignore$|^.yamllint$
  steps:
    allow_best_effort_post_steps: true
    allow_skip_on_success: true
    cluster_profile: gcp-opendatahub
    dependencies:
      OO_BUNDLE: opendatahub-operator-bundle
    env:
      COMPUTE_NODE_TYPE: n2-standard-8
      OO_INSTALL_NAMESPACE: openshift-operators
    test:
    - as: e2e
      cli: latest
      commands: |
        unset GOFLAGS
        make e2e-test -e E2E_TEST_FLAGS="-timeout 60m" -e OPERATOR_NAMESPACE=openshift-operators
      from: src
      resources:
        requests:
          cpu: 2000m
          memory: 3Gi
    workflow: optional-operators-ci-operator-sdk-gcp
```

**After:**
```yaml
- as: opendatahub-operator-e2e
  cluster_claim:
    architecture: amd64
    cloud: aws
    owner: opendatahub
    product: ocp
    timeout: 1h0m0s
    version: "4.18"
  skip_if_only_changed: ^\.github/|^docs/|\.[mM][dD]$|^.gitignore$|^golangci|^crd-ref-docs\.config|^OWNERS$|^PROJECT$|^LICENSE$|^OWNERS_ALIASES$|^Makefile$|^cmd/[^/]+/|^[^/]+\.(yaml|yml)$|^.gitleaksignore$|^.yamllint$
  steps:
    allow_best_effort_post_steps: true
    allow_skip_on_success: true
    dependencies:
      OO_BUNDLE: opendatahub-operator-bundle
    env:
      OO_INSTALL_NAMESPACE: openshift-operators
    pre:
    - ref: ipi-install-rbac
    - ref: optional-operators-operator-sdk
    test:
    - as: e2e
      cli: latest
      commands: |
        unset GOFLAGS
        make e2e-test -e E2E_TEST_FLAGS="-timeout 60m" -e OPERATOR_NAMESPACE=openshift-operators
      from: src
      resources:
        requests:
          cpu: 2000m
          memory: 3Gi
    workflow: generic-claim
```

### 5.4 Submit and Validate

```bash
# In your openshift/release checkout
git checkout -b opendatahub-cluster-claim upstream/main

# Edit the ci-operator config files as shown above
# Then commit and push
git add ci-operator/config/opendatahub-io/opendatahub-operator/
git commit -m "Switch opendatahub-operator E2E to cluster pools

Migrate E2E CI jobs from per-run GCP IPI provisioning to
pre-provisioned AWS Hive cluster pools, removing 25-40 min
of cluster installation from the test critical path.

Changes per job:
- cluster_profile: gcp-opendatahub -> cluster_claim (AWS pool)
- workflow: optional-operators-ci-operator-sdk-gcp -> generic-claim
- Added optional-operators-operator-sdk as explicit pre-step
- Removed COMPUTE_NODE_TYPE (now in pool install-config)

JIRA: RHOAIENG-46694"

git push origin opendatahub-cluster-claim
gh pr create --repo openshift/release \
  --title "Switch opendatahub-operator E2E to cluster pools [RHOAIENG-46694]"
```

The PR will trigger a **pj-rehearse** that actually runs a rehearsal of your changed jobs.
This is the real validation -- if the rehearsal job succeeds, the migration is working.

> **Tip**: You can also submit the pool manifests PR and ci-operator config PR at the same
> time, but the config PR will fail rehearsals until the pool PR merges and clusters bootstrap.
> It's safer to do them sequentially.

---

## Pool Sizing Rationale

| Pool | OCP Version | Branch | `size` | `maxSize` | Rationale |
|------|-------------|--------|--------|-----------|-----------|
| `opendatahub-ocp-4-19-amd64-aws` | 4.19 | main | 1 | 4 | 2 E2E jobs per PR (ODH + RHOAI), need burst for concurrent PRs |
| `opendatahub-ocp-4-18-amd64-aws` | 4.18 | stable-2.x | 1 | 2 | 1 E2E job per PR, lower volume |

**What the numbers mean:**
- `size`: Number of clusters Hive keeps pre-provisioned at all times. These are either running
  or hibernating. Setting to 1 means the first job claiming from this pool gets a cluster fast.
- `maxSize`: Maximum total clusters that can exist simultaneously (claimed + pre-provisioned).
  If all `size` clusters are claimed, Hive provisions new ones up to `maxSize`.
- Beyond `maxSize`, additional claims queue until a cluster is released/destroyed.

**Expected claim latency:**
- If a warm cluster is available: **~instant** (no waiting)
- If a hibernating cluster exists: **3-6 minutes** (wake from hibernation)
- If no cluster available (all claimed): **40-60 minutes** (new install required)

**Cost implications:**
- A `size: 1` pool costs one hibernating cluster continuously (~EBS storage only, minimal)
- Active clusters cost full EC2 instance + EBS pricing
- Clusters are destroyed 4 hours after being claimed

---

## Operational Runbook

### Scaling Pool Up/Down

If you're seeing queue times because all clusters are claimed:

```bash
# In openshift/release, edit the ClusterPool manifest
# Increase size (more warm clusters) or maxSize (higher burst)

# Example: scale up main pool to size=2, maxSize=6
# Edit opendatahub-ocp-4-19-amd64-aws_clusterpool.yaml:
#   spec.size: 2
#   spec.maxSize: 6
# Submit PR to openshift/release
```

To temporarily scale down (save cost):
```bash
# Set size: 0 to stop keeping warm clusters
# Existing hibernating clusters will be deprovisioned
```

### Rotating AWS Credentials

If AWS access keys need to be rotated:

1. **Scale down** all pools to `size: 0` in a PR to openshift/release
2. Wait for all existing ClusterDeployments to be deprovisioned
3. **Rotate** the access key in AWS IAM:
   ```bash
   aws iam create-access-key --user-name opendatahub-ci-hive
   # Note the new key ID and secret
   aws iam delete-access-key --user-name opendatahub-ci-hive \
     --access-key-id <OLD_KEY_ID>
   ```
4. **Update** the Vault secret at vault.ci.openshift.org with the new credentials
5. Wait 30 min for sync
6. **Scale up** the pools to their original sizes

> **Warning**: If you update credentials while clusters provisioned with the old credentials
> still exist, Hive cannot deprovision them (they'll be orphaned). Always scale down first.

### Adding a New OCP Version Pool

When a new OCP version is needed (e.g. main moves to 4.20):

1. Check that a suitable `ClusterImageSet` exists:
   ```
   clusters/hosted-mgmt/hive/pools/ocp-release-4.20.*_clusterimageset.yaml
   ```
   If not, DPTP typically creates these automatically for released OCP versions.

2. Create a new ClusterPool manifest following the existing pattern:
   ```yaml
   # opendatahub-ocp-4-20-amd64-aws_clusterpool.yaml
   metadata:
     labels:
       version: "4.20"
       version_lower: 4.20.0-0
       version_upper: 4.21.0-0
     name: opendatahub-ocp-4-20-amd64-aws
   spec:
     imageSetRef:
       name: ocp-release-4.20.X-x86-64-for-4.20.0-0-to-4.21.0-0
   ```

3. Update the ci-operator config to use `version: "4.20"` in the `cluster_claim`.

4. Optionally decommission the old version pool.

### Decommissioning a Pool

Hive pool deletion is NOT handled by GitOps automation (it only creates/updates, never
deletes). To remove a pool:

1. Scale the pool to `size: 0` in a PR
2. Wait for all ClusterDeployments to be removed
3. Contact `#forum-ocp-testplatform` to delete the ClusterPool resource from `hosted-mgmt`
4. Remove the manifest file from openshift/release in a follow-up PR

---

## Troubleshooting

### Job fails with "no clusters available" / claim timeout

The pool is exhausted (all clusters claimed, `maxSize` reached). Options:
- Increase `maxSize` if quota allows
- Wait for running claims to finish (clusters destroyed after 4h)
- Check if a stuck/leaked cluster is consuming pool capacity

### Job hangs at "waiting for cluster claim"

Hive might be failing to provision replacement clusters. Check install logs (see 4.2).

### Tests fail with cloud-provider-specific errors

Reminder: the migration changes cloud provider from GCP to AWS. If any test code has
GCP-specific assumptions, it will break. Check for:
- Hardcoded GCP instance metadata URLs
- GCP-specific storage class names
- Cloud provider checks in test assertions

Our test code does NOT reference `CLUSTER_PROFILE_DIR` or cloud-specific APIs -- verified
during analysis. But always double-check after the first successful run.

### operator-sdk installation fails

The `optional-operators-operator-sdk` step requires:
- `cli-operator-sdk` in `base_images` (already present in our config)
- `OO_BUNDLE` dependency (already configured)
- `OO_INSTALL_NAMESPACE` env var (already configured)

If it fails, check the step logs for operator-sdk-specific errors (bundle pull failures,
OLM issues, etc.).

### Pull secret not available

With `cluster_claim`, `${CLUSTER_PROFILE_DIR}/pull-secret` does **not** exist. If any step
needs a pull secret, use the `ci-pull-credentials` secret from `test-credentials` namespace
instead:
```yaml
credentials:
- mount_path: /var/run/secrets/ci-pull-credentials
  name: ci-pull-credentials
  namespace: ci
```

Our tests do not reference pull secrets directly, so this should not be an issue.

---

## References

- [Creating a Cluster Pool](https://docs.ci.openshift.org/docs/how-tos/cluster-claim/) -- full setup guide
- [Testing with a Cluster from a Cluster Pool](https://docs.ci.openshift.org/docs/architecture/ci-operator/#testing-with-a-cluster-from-a-cluster-pool) -- ci-operator docs
- [Adding a New Secret to CI](https://docs.ci.openshift.org/docs/how-tos/adding-a-new-secret-to-ci/) -- Vault secret management
- [generic-claim workflow](https://steps.ci.openshift.org/workflow/generic-claim) -- workflow for pool-claimed clusters
- [optional-operators-operator-sdk step](https://steps.ci.openshift.org/reference/optional-operators-operator-sdk) -- operator installation step
- [Existing cluster pools (openshift/release)](https://github.com/openshift/release/tree/main/clusters/hosted-mgmt/hive/pools) -- reference implementations
- [Hive ClusterPool docs](https://github.com/openshift/hive/blob/master/docs/clusterpools.md) -- upstream Hive documentation
- [Hive troubleshooting](https://github.com/openshift/hive/blob/master/docs/troubleshooting.md#clusterpools) -- debugging cluster pools
- [OpenShift AWS IAM permissions](https://docs.openshift.com/container-platform/4.19/installing/installing_aws/installing-aws-account.html#installation-aws-permissions_installing-aws-account) -- required IAM policies
- [Vault self-service](https://selfservice.vault.ci.openshift.org/) -- create secret collections
- [Vault UI](https://vault.ci.openshift.org/) -- manage secrets
