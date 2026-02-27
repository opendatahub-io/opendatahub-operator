package clusterhealth

import (
	"context"
	"errors"
	"time"
)

// Run runs all health checks and returns a Report. Config.Client and namespace
// configuration must be set by the caller; no globals are used.
//
// Run always returns a non-nil Report when err is nil. It returns an error only
// when the report cannot be built at all (e.g. nil client). Partial failures are
// recorded per section in the Report (SectionResult.Error and optional Data).
func Run(ctx context.Context, cfg Config) (*Report, error) {
	if cfg.Client == nil {
		return nil, errors.New("clusterhealth: client is required")
	}

	collectedAt := time.Now()
	report := &Report{CollectedAt: collectedAt}
	run := cfg.sectionsToRun()

	// Run each section independently when selected; failures in one do not block others.
	if run[SectionNodes] {
		report.Nodes = runNodesSection(ctx, cfg.Client, cfg.Namespaces)
	}
	if run[SectionDeployments] {
		report.Deployments = runDeploymentsSection(ctx, cfg.Client, cfg.Namespaces)
	}
	if run[SectionPods] {
		report.Pods = runPodsSection(ctx, cfg.Client, cfg.Namespaces)
	}
	if run[SectionEvents] {
		report.Events = runEventsSection(ctx, cfg.Client, cfg.Namespaces, collectedAt)
	}
	if run[SectionQuotas] {
		report.Quotas = runQuotasSection(ctx, cfg.Client, cfg.Namespaces)
	}
	if run[SectionOperator] {
		report.Operator = runOperatorSection(ctx, cfg.Client, cfg.Operator)
	}
	if run[SectionDSCI] {
		report.DSCI = runDSCISection(ctx, cfg.Client, cfg.DSCI)
	}
	if run[SectionDSC] {
		report.DSC = runDSCSection(ctx, cfg.Client, cfg.DSC)
	}

	return report, nil
}
