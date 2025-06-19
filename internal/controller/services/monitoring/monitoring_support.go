package monitoring

import (
	"context"
	"errors"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	cond "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
)

func addMonitoringCapability(ctx context.Context, rr *types.ReconciliationRequest, monitoring *serviceApi.Monitoring) error {
	log := logf.FromContext(ctx)
	initialCondition := &common.Condition{
		Type:   "MonitoringConfigured",
		Status: metav1.ConditionTrue,
		Reason: "MonitoringConfigured",
	}

	capability := monitoringCapability(rr, monitoring, initialCondition)

	// Retry logic in case of Feature tracker update error
	var lastErr error
	retryIntervals := []int{1, 2, 4, 8, 16}

	for i, interval := range retryIntervals {
		// Check if context is cancelled
		if ctx.Err() != nil {
			return ctx.Err()
		}

		err := capability.Apply(ctx, rr.Client)
		if err == nil {
			return nil
		}
		lastErr = err

		latestMonitoring := &serviceApi.Monitoring{}
		if err := rr.Client.Get(ctx, client.ObjectKey{Name: monitoring.Name, Namespace: monitoring.Namespace}, latestMonitoring); err != nil {
			log.Error(err, "failed to get latest monitoring resource")
			return err
		}
		monitoring = latestMonitoring
		capability = monitoringCapability(rr, monitoring, initialCondition)

		// Add exponential backoff delay (except for last iteration)
		if i < len(retryIntervals)-1 {
			select {
			case <-time.After(time.Duration(interval) * time.Second):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	return fmt.Errorf("failed to add monitoring capability after 5 retries, last error: %w", lastErr)
}

func monitoringCapability(rr *types.ReconciliationRequest, monitoring *serviceApi.Monitoring, initialCondition *common.Condition) *feature.HandlerWithReporter[*serviceApi.Monitoring] { //nolint:lll // Reason: generics are long
	return feature.NewHandlerWithReporter(
		feature.ClusterFeaturesHandler(rr.DSCI, addMonitoringPreconditions(rr)),
		createCapabilityReporter(rr.Client, monitoring, initialCondition),
	)
}

func createCapabilityReporter(cli client.Client, object *serviceApi.Monitoring, successfulCondition *common.Condition) *status.Reporter[*serviceApi.Monitoring] {
	return status.NewStatusReporter(
		cli,
		object,
		func(err error) status.SaveStatusFunc[*serviceApi.Monitoring] {
			return func(saved *serviceApi.Monitoring) {
				actualCondition := successfulCondition.DeepCopy()
				if err != nil {
					actualCondition.Status = metav1.ConditionFalse
					actualCondition.Message = err.Error()
					actualCondition.Reason = status.CapabilityFailed
					var missingOperatorErr *feature.MissingOperatorError
					if errors.As(err, &missingOperatorErr) {
						actualCondition.Reason = status.MissingOperatorReason
					}
				}
				cond.SetStatusCondition(&saved.Status, *actualCondition)
			}
		},
	)
}

func addMonitoringPreconditions(rr *types.ReconciliationRequest) feature.FeaturesProvider {
	return func(registry feature.FeaturesRegistry) error {
		metrics := rr.DSCI.Spec.Monitoring.Metrics
		traces := rr.DSCI.Spec.Monitoring.Traces
		monitoringFeature := feature.Define("observability").PreConditions(
			feature.EnsureOperatorIsInstalled("opentelemetry-operator"),
		)

		if metrics != (serviceApi.MetricsSpec{}) {
			monitoringFeature = monitoringFeature.PreConditions(
				feature.EnsureOperatorIsInstalled("cluster-observability-operator"),
			)
		}
		if traces != (serviceApi.TracesSpec{}) {
			monitoringFeature = monitoringFeature.PreConditions(
				feature.EnsureOperatorIsInstalled("tempo-product"),
			)
		}

		// Register the feature with the preconditions
		return registry.Add(
			monitoringFeature,
		)
	}
}
