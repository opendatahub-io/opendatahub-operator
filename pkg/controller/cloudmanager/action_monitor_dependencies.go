package cloudmanager

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	ccmcommon "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	ccmcharts "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/cloudmanager/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/status/deployments"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/monitor"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

const dependencyDegradedReason = "DependencyDegraded"

func defaultDegradedConditionFilter(condType, condStatus string) bool {
	if condType == "Degraded" && condStatus == string(metav1.ConditionTrue) {
		return true
	}
	if (condType == "Available" || condType == "Ready") && condStatus == string(metav1.ConditionFalse) {
		return true
	}

	return false
}

func monitorDependencies(ctx context.Context, rr *types.ReconciliationRequest, resourceID string, configs []ccmcharts.DependencyMonitorConfig) error {
	for _, cfg := range configs {
		if cfg.Policy == ccmcommon.Unmanaged {
			rr.Conditions.MarkTrue(
				cfg.ConditionType,
				conditions.WithReason(status.UnmanagedReason),
			)

			continue
		}

		// Tier 1: operator deployment health
		if cfg.HasDeployments {
			depAction := deployments.NewAction(
				deployments.WithConditionType(cfg.ConditionType),
				deployments.InNamespace(cfg.Namespace),
				deployments.WithPartOfLabel(labels.InfrastructurePartOf),
				deployments.WithSelectorLabel(labels.InfrastructurePartOf, resourceID),
			)

			if err := depAction(ctx, rr); err != nil {
				return fmt.Errorf("deployment check for %s failed: %w", cfg.ReleaseName, err)
			}
		}

		// Tier 2: operator CR health (skip if Tier 1 already marked unhealthy)
		cond := rr.Conditions.GetCondition(cfg.ConditionType)
		if cfg.OperatorCR != nil && (cond == nil || cond.Status != metav1.ConditionFalse) {
			result, err := monitor.CheckOperatorHealth(ctx, rr.Client, monitor.OperatorConfig{
				OperatorGVK: cfg.OperatorCR.GVK,
				CRName:      cfg.OperatorCR.Name,
				CRNamespace: cfg.OperatorCR.Namespace,
				Filter:      defaultDegradedConditionFilter,
			})
			if err != nil {
				return fmt.Errorf("operator CR check for %s failed: %w", cfg.ReleaseName, err)
			}

			if !result.Pass {
				rr.Conditions.MarkFalse(
					cfg.ConditionType,
					conditions.WithReason(dependencyDegradedReason),
					conditions.WithMessage("%s", result.Message),
				)
			}
		}

		// No deployments and no CR (e.g. GatewayAPI): mark available after successful deploy
		if !cfg.HasDeployments && cfg.OperatorCR == nil {
			rr.Conditions.MarkTrue(cfg.ConditionType)
		}
	}

	return nil
}

func summarizeDependencyStatus(rr *types.ReconciliationRequest, configs []ccmcharts.DependencyMonitorConfig) {
	var notReady []string

	for _, cfg := range configs {
		c := rr.Conditions.GetCondition(cfg.ConditionType)
		if c == nil || c.Status != metav1.ConditionTrue {
			notReady = append(notReady, cfg.ReleaseName)
		}
	}

	if len(notReady) > 0 {
		rr.Conditions.MarkFalse(
			status.ConditionDependenciesReady,
			conditions.WithReason(status.NotReadyReason),
			conditions.WithMessage("Dependencies not ready: %s", strings.Join(notReady, ", ")),
			conditions.WithSeverity(common.ConditionSeverityError),
		)

		return
	}

	rr.Conditions.MarkTrue(status.ConditionDependenciesReady)
}
