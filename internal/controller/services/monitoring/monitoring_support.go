package monitoring

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	cond "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

const (
	opentelemetryOperator        = "opentelemetry-product"
	clusterObservabilityOperator = "cluster-observability-operator"
	tempoOperator                = "tempo-product"
)

func addMonitoringCapability(ctx context.Context, rr *types.ReconciliationRequest, monitoring *serviceApi.Monitoring) error {
	log := logf.FromContext(ctx)

	if err := checkMonitoringPreconditions(ctx, rr); err != nil {
		log.Error(err, "Monitoring preconditions failed")

		return updateMonitoringStatus(ctx, rr.Client, monitoring, err)
	}

	return updateMonitoringStatus(ctx, rr.Client, monitoring, nil)
}

func checkOperatorSubscription(ctx context.Context, cli client.Client, operatorName string) error {
	if found, err := cluster.SubscriptionExists(ctx, cli, operatorName); !found || err != nil {
		if err != nil {
			return fmt.Errorf("failed to find the pre-requisite operator subscription %q,"+
				" please ensure operator is installed: %w", operatorName, err)
		}
		return fmt.Errorf("failed to find the pre-requisite operator subscription %q,"+
			" please ensure operator is installed", operatorName)
	}
	return nil
}

func checkMonitoringPreconditions(ctx context.Context, rr *types.ReconciliationRequest) error {
	var allErrors []error

	err := checkOperatorSubscription(ctx, rr.Client, opentelemetryOperator)
	if err != nil {
		allErrors = append(allErrors, err)
	}

	// Check for cluster-observability-operator if metrics are enabled
	if rr.DSCI.Spec.Monitoring.Metrics != nil {
		err = checkOperatorSubscription(ctx, rr.Client, clusterObservabilityOperator)
		if err != nil {
			allErrors = append(allErrors, err)
		}
	}

	// Check for tempo-product operator if traces are enabled
	if rr.DSCI.Spec.Monitoring.Traces != nil {
		err = checkOperatorSubscription(ctx, rr.Client, tempoOperator)
		if err != nil {
			allErrors = append(allErrors, err)
		}
	}

	if len(allErrors) > 0 {
		var errorMessages []string
		for _, err := range allErrors {
			errorMessages = append(errorMessages, err.Error())
		}
		return fmt.Errorf("monitoring preconditions failed: %s", strings.Join(errorMessages, "; "))
	}

	return nil
}

func updateMonitoringStatus(ctx context.Context, cli client.Client, monitoring *serviceApi.Monitoring, err error) error {
	// Fetch the latest monitoring object to avoid conflicts
	latestMonitoring := &serviceApi.Monitoring{}
	if getErr := cli.Get(ctx, client.ObjectKey{Name: monitoring.Name, Namespace: monitoring.Namespace}, latestMonitoring); getErr != nil {
		return fmt.Errorf("failed to get latest monitoring resource: %w", getErr)
	}

	condition := &common.Condition{
		Type:   "MonitoringConfigured",
		Status: metav1.ConditionTrue,
		Reason: "MonitoringConfigured",
	}

	if err != nil {
		condition.Status = metav1.ConditionFalse
		condition.Message = err.Error()
		condition.Reason = status.CapabilityFailed

		if isMissingOperatorError(err) {
			condition.Reason = status.MissingOperatorReason
		}
	}

	cond.SetStatusCondition(&latestMonitoring.Status, *condition)

	if updateErr := cli.Status().Update(ctx, latestMonitoring); updateErr != nil {
		return fmt.Errorf("failed to update monitoring status: %w", updateErr)
	}

	return err
}

func isMissingOperatorError(err error) bool {
	// Check if the error contains text indicating missing operator subscription
	if err != nil {
		errStr := err.Error()
		return strings.Contains(errStr, "failed to find the pre-requisite operator subscription")
	}
	return false
}
