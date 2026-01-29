# CLAUDE.md - Prow CI Testing & Fail-Fast Evaluation

This file provides guidance to Claude Code when working in this opendatahub-operator test repository.

## Project Context

This is a **test clone** of the opendatahub-io/opendatahub-operator repository for evaluating CI/Prow behavior and testing fail-fast recommendations.

**Git Setup**:
- **origin**: `git@github.com:jctanner-opendatahub-io/opendatahub-operator` (your fork)
- **upstream**: `https://github.com/opendatahub-io/opendatahub-operator` (main repo)
- **Branch**: `CI_PROW_EVALUATION` (already created)

**CI System**: OpenShift Prow (prow.ci.openshift.org)
- Prow automatically runs e2e tests on PRs to the upstream repository
- Test results available at: `https://prow.ci.openshift.org/`
- Artifacts stored in GCS: `gs://test-platform-results/pr-logs/pull/opendatahub-io_opendatahub-operator/`

## Mission

You have two primary objectives:

### 1. Trigger Prow Test Jobs via Benign Commits

Make small, harmless changes to trigger Prow CI and observe infrastructure failure patterns.

**Approach**:
- Make trivial changes (add comment to docstring, fix typo, add whitespace to docs)
- Commit and push to your fork's CI_PROW_EVALUATION branch
- Open PR to upstream opendatahub-io/opendatahub-operator
- Monitor Prow job execution
- Observe failure patterns, durations, and infrastructure issues

**Commands**:
```bash
# Make a benign change (example)
echo "# CI evaluation test $(date +%Y%m%d_%H%M%S)" >> docs/TESTING.md

# Commit and push
git add docs/TESTING.md
git commit -m "docs: CI evaluation - trigger test jobs"
git push origin CI_PROW_EVALUATION

# Create PR using gh CLI
gh pr create --base main --head jctanner-opendatahub-io:CI_PROW_EVALUATION \
  --title "CI Evaluation: Infrastructure Failure Pattern Testing" \
  --body "Testing Prow CI behavior to evaluate fail-fast recommendations. This PR contains only benign documentation changes."

# Monitor PR and Prow jobs
gh pr view --web  # Opens PR in browser
# Also check: https://prow.ci.openshift.org/
```

**What to Observe**:
- Job duration (especially failures - do they run for 90+ minutes?)
- Infrastructure vs code failures
- Timeout patterns
- Clarity of error messages
- Time-of-day correlation (peak hours: 1-4 PM UTC / 8-11 AM EST / 9 AM-12 PM EDT)

### 2. Evaluate Fail-Fast Recommendations

Based on CI audit findings, evaluate whether fail-fast patterns would improve the developer experience.

**Background from CI Audit** (6-month analysis):
- **87.6% of failures are infrastructure-related** (not code bugs)
- **Average failed test duration**: 92 minutes (wasted waiting for timeout)
- **75.4% of PRs require manual retries** (average 4.8 retries)
- **Peak hour success rate**: 52-58% vs off-peak 70%+
- **Wasted CI time**: 4,319 hours over 6 months due to infrastructure timeouts

**Fail-Fast Principle**:
Detect infrastructure issues in < 5 minutes instead of running for 90+ minutes before timeout.

## Fail-Fast Recommendations (from CI Audit)

### Pattern 1: Pre-Flight Infrastructure Check

Add quick health checks BEFORE running expensive e2e tests.

**Implementation** (tests/e2e/):

```go
// InfrastructureHealthCheck runs quick checks to verify cluster is ready
// Returns error if infrastructure is degraded, allowing test to fail fast
func InfrastructureHealthCheck(ctx context.Context) error {
    timeout := 30 * time.Second

    // Check 1: Cluster nodes are ready (5 seconds)
    GinkgoWriter.Printf("Pre-flight check: Verifying nodes are ready...\n")
    nodes, err := kubeClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
    if err != nil {
        return fmt.Errorf("[INFRASTRUCTURE] failed to list nodes: %w", err)
    }

    readyNodes := 0
    for _, node := range nodes.Items {
        for _, condition := range node.Status.Conditions {
            if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
                readyNodes++
                break
            }
        }
    }

    if readyNodes < 3 {
        return fmt.Errorf("[INFRASTRUCTURE] insufficient ready nodes: %d/3", readyNodes)
    }

    // Check 2: Sufficient cluster resources available
    // Check 3: Image registry is accessible
    // Check 4: Required operators are running

    return nil
}

// Add to test suites (BeforeSuite):
var _ = BeforeSuite(func() {
    ctx := context.Background()

    By("Running infrastructure health check")
    err := InfrastructureHealthCheck(ctx)
    Expect(err).NotTo(HaveOccurred(), "Infrastructure not ready - failing fast to save CI time")
})
```

**Benefits**:
- Fail in < 5 minutes if infrastructure is degraded
- Clear error message: "[INFRASTRUCTURE] insufficient ready nodes: 1/3"
- Save ~87 minutes of wasted CI time per failure
- Enable auto-retry to distinguish infrastructure vs code failures

### Pattern 2: Fail-Fast Timeouts

Replace long timeouts with short circuit breakers.

**Current Problem**:
```go
// CURRENT: Waits 10+ minutes even when cluster is clearly broken
Eventually(func() error {
    return waitForDeployment(ctx, "odh-dashboard")
}, 10*time.Minute, 1*time.Second).Should(Succeed())
```

**Fail-Fast Alternative**:
```go
// IMPROVED: Detect persistent failures quickly
Eventually(func() error {
    return waitForDeployment(ctx, "odh-dashboard")
}, 5*time.Minute, 1*time.Second).Should(Succeed())

// OR even better - circuit breaker pattern:
consecutiveFailures := 0
Eventually(func() error {
    err := waitForDeployment(ctx, "odh-dashboard")
    if err != nil {
        consecutiveFailures++
        if consecutiveFailures >= 10 {
            // 10 consecutive failures = infrastructure issue, fail fast
            return fmt.Errorf("[INFRASTRUCTURE] deployment failing consistently: %w", err)
        }
    } else {
        consecutiveFailures = 0
    }
    return err
}, 3*time.Minute, 1*time.Second).Should(Succeed())
```

### Pattern 3: Infrastructure Tagging

Tag infrastructure failures clearly for auto-retry and analysis.

**Convention**: Prefix error messages with `[INFRASTRUCTURE]` for issues outside developer control.

```go
// Good error messages for fail-fast
"[INFRASTRUCTURE] pod scheduling timeout - no available nodes"
"[INFRASTRUCTURE] image pull failed - registry unavailable"
"[INFRASTRUCTURE] cluster API timeout - control plane degraded"

// Code issues (developer should fix)
"assertion failed: expected 3 replicas, got 1"
"panic: nil pointer dereference in controller"
```

## Evaluation Criteria

After triggering test jobs and observing Prow behavior, evaluate:

### Questions to Answer:

1. **Do tests run for 90+ minutes before failing?**
   - Yes/No
   - Actual average duration for failed runs?

2. **Are infrastructure failures clearly identified?**
   - Current error message clarity?
   - Would "[INFRASTRUCTURE]" tagging help?

3. **Could pre-flight checks prevent wasted time?**
   - What % of failures could be caught in first 5 minutes?
   - Examples of failures that could be detected early?

4. **What is the developer experience?**
   - How many retries needed per PR?
   - How long waiting for results?
   - Is it clear when to retry vs fix code?

5. **Time-of-day correlation?**
   - Do tests fail more during peak hours (9 AM - 12 PM EDT)?
   - Should we recommend off-peak scheduling for some jobs?

### Data to Collect:

For each Prow job run:
- Start time (UTC)
- End time (UTC)
- Duration
- Result (SUCCESS/FAILURE/ABORTED)
- Error message (if failed)
- Whether failure appears to be infrastructure vs code
- GCS artifact path for detailed logs

### Expected Findings (from CI Audit):

Based on 6 months of data, you should observe:

- **Infrastructure failure rate**: ~50-90% (varies by time of day)
- **Average failed duration**: 90-115 minutes (hitting timeout)
- **Average success duration**: 30-60 minutes
- **Retry necessity**: 70-80% of infrastructure failures pass on retry
- **Peak hour correlation**: Lower success rate 1-4 PM UTC / 8-11 AM EST / 9 AM-12 PM EDT

## Test Job Types

Prow runs multiple job types per PR:

| Job Type | Description | Typical Duration | Common Failures |
|----------|-------------|------------------|-----------------|
| **e2e** | Main e2e test suite | 92-115 min (fail), 30-60 min (pass) | Timeouts, pod startup, image pulls |
| **e2e-hypershift** | Hypershift e2e tests | 120-180 min | Cluster provisioning, networking |
| **rhoai-e2e** | RHOAI integration tests | 60-90 min | Infrastructure timeouts |
| **bundle** | Bundle validation | 10-20 min | Fast, usually reliable |
| **images** | Image builds | 10-15 min | Registry issues |

**Focus on**: The main `e2e` job - highest volume, most infrastructure failures.

## Monitoring Commands

```bash
# View PR status
gh pr view <PR_NUMBER>

# View PR checks
gh pr checks <PR_NUMBER>

# View Prow job logs (requires web browser)
# https://prow.ci.openshift.org/

# Download GCS artifacts for analysis
# gs://test-platform-results/pr-logs/pull/opendatahub-io_opendatahub-operator/<PR>/pull-ci-opendatahub-io-opendatahub-operator-main-opendatahub-operator-e2e/<BUILD_ID>/
```

## Test Files to Explore

E2E test structure (tests/e2e/):
```
tests/e2e/
├── authcontroller_test.go
├── dashboard_test.go
├── datasciencepipelines_test.go
├── kserve_test.go
├── modelregistry_test.go
├── kueue_test.go
├── gateway_test.go
└── helper_test.go
```

**Key file**: `helper_test.go` - Contains test utilities, setup/teardown

**Common pattern**: Each test file uses Ginkgo BDD framework with BeforeSuite/AfterSuite

## Implementation Strategy

If fail-fast patterns show promise:

### Phase 1: Prove the Concept (This Evaluation)
1. Trigger multiple test runs via benign commits
2. Document current failure patterns
3. Measure wasted time (how long tests run before timeout)
4. Identify infrastructure failures that could be caught early

### Phase 2: Prototype (If Promising)
1. Add InfrastructureHealthCheck to helper_test.go
2. Implement in BeforeSuite for one test (e.g., dashboard_test.go)
3. Run side-by-side comparison
4. Measure improvement in time-to-failure

### Phase 3: Rollout (If Successful)
1. Expand to all test suites
2. Add circuit breaker timeouts
3. Implement infrastructure error tagging
4. Update Prow configuration for auto-retry based on tags

## Related Documentation

Full CI audit analysis available in parent directory:
- **CI Audit Docs**: `../ci_audit/docs/`
- **Fail-Fast Guide**: `../ci_audit/docs/recommendations/fail-fast-patterns.md`
- **Infrastructure Issues**: `../ci_audit/docs/findings/infrastructure.md`
- **Time Cost Analysis**: `../ci_audit/docs/findings/time-cost.md`
- **Test Improvements**: `../ci_audit/docs/findings/test-improvements.md`

## Success Metrics

This evaluation is successful if you can answer:

1. ✅ Current average time-to-failure for infrastructure issues
2. ✅ Percentage of failures detectable in < 5 minutes
3. ✅ Developer clarity on infrastructure vs code failures
4. ✅ ROI estimate: hours saved per month with fail-fast patterns

**Target**: If fail-fast can save 50%+ of wasted CI time (>2,000 hours/6 months), it's worth implementing.

## Important Notes

- **Do NOT** merge the test PR - it's for evaluation only
- Make only benign changes (documentation, comments, whitespace)
- DO NOT modify production code or tests in this evaluation phase
- Document all observations for later analysis
- Close PR after collecting sufficient data

## Getting Started

```bash
# Verify you're on the right branch
git branch --show-current  # Should show: CI_PROW_EVALUATION

# Make a benign change to trigger CI
echo "# CI test $(date)" >> docs/TESTING.md
git add docs/TESTING.md
git commit -m "docs: trigger CI evaluation"
git push origin CI_PROW_EVALUATION

# Create PR
gh pr create --base main --head jctanner-opendatahub-io:CI_PROW_EVALUATION \
  --title "CI Evaluation: Infrastructure Failure Testing" \
  --body "Testing infrastructure failure patterns. Benign documentation changes only."

# Monitor the PR
gh pr view --web
```

Good luck with the evaluation!
