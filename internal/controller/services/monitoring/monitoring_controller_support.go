package monitoring

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	cond "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

//go:embed resources
var resourcesFS embed.FS

const (
	MonitoringStackTemplate          = "resources/monitoring-stack.tmpl.yaml"
	TempoMonolithicTemplate          = "resources/tempo-monolithic.tmpl.yaml"
	TempoStackTemplate               = "resources/tempo-stack.tmpl.yaml"
	OpenTelemetryCollectorTemplate   = "resources/opentelemetry-collector.tmpl.yaml"
	CollectorServiceMonitorsTemplate = "resources/collector-servicemonitors.tmpl.yaml"
	CollectorRBACTemplate            = "resources/collector-rbac.tmpl.yaml"
	PrometheusRouteTemplate          = "resources/prometheus-route.tmpl.yaml"
	PrometheusPipelineName           = "odh-prometheus-collector"
	opentelemetryOperator            = "opentelemetry-product"
	clusterObservabilityOperator     = "cluster-observability-operator"
	tempoOperator                    = "tempo-product"
)

func getTemplateData(ctx context.Context, rr *odhtypes.ReconciliationRequest) (map[string]any, error) {
	monitoring, ok := rr.Instance.(*serviceApi.Monitoring)
	if !ok {
		return nil, errors.New("instance is not of type services.Monitoring")
	}

	templateData := map[string]any{
		"Namespace": monitoring.Spec.Namespace,
	}

	templateData["Traces"] = monitoring.Spec.Traces != nil
	templateData["Metrics"] = monitoring.Spec.Metrics != nil

	// Add metrics data if metrics are configured
	if monitoring.Spec.Metrics != nil {
		metrics := monitoring.Spec.Metrics

		var cpuLimit, memoryLimit, cpuRequest, memoryRequest string
		if metrics.Resources != nil {
			cpuLimit = metrics.Resources.CPULimit.String()
			memoryLimit = metrics.Resources.MemoryLimit.String()
			cpuRequest = metrics.Resources.CPURequest.String()
			memoryRequest = metrics.Resources.MemoryRequest.String()
		} else { // here need to match default value set in API
			cpuLimit = "500m"
			memoryLimit = "512Mi"
			cpuRequest = "100m"
			memoryRequest = "256Mi"
		}

		var storageSize, storageRetention string
		if metrics.Storage != nil {
			storageSize = metrics.Storage.Size.String()
			storageRetention = metrics.Storage.Retention
		} else { // here need to match default value set in API
			storageSize = "5Gi"
			storageRetention = "1d"
		}

		// only when either storage or resources is set, we take replicas into account
		// - if user did not set it / zero-value "0", we use default value of 2
		// - if user set it to Y, we pass Y to template
		var replicas int32 = 2 // default value to match monitoringstack CRD's default
		if (metrics.Storage != nil || metrics.Resources != nil) && metrics.Replicas != 0 {
			replicas = metrics.Replicas
		}

		templateData["CPULimit"] = cpuLimit
		templateData["MemoryLimit"] = memoryLimit
		templateData["CPURequest"] = cpuRequest
		templateData["MemoryRequest"] = memoryRequest
		templateData["StorageSize"] = storageSize
		templateData["StorageRetention"] = storageRetention
		templateData["Replicas"] = strconv.Itoa(int(replicas))
		templateData["PromPipelineName"] = PrometheusPipelineName
	}

	// Add traces data if traces are configured
	if monitoring.Spec.Traces != nil {
		traces := monitoring.Spec.Traces.Storage
		templateData["Backend"] = traces.Backend
		templateData["Secret"] = traces.Secret
		templateData["Size"] = traces.Size
	}

	return templateData, nil
}

func addMonitoringCapability(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	log := logf.FromContext(ctx)

	// Set initial condition state
	rr.Conditions.MarkUnknown("MonitoringConfigured")

	if err := checkMonitoringPreconditions(ctx, rr); err != nil {
		log.Error(err, "Monitoring preconditions failed")

		rr.Conditions.MarkFalse(
			"MonitoringConfigured",
			cond.WithReason(status.MissingOperatorReason),
			cond.WithMessage("Monitoring preconditions failed: %s", err.Error()),
		)

		return err
	}

	rr.Conditions.MarkTrue("MonitoringConfigured")

	return nil
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

func checkMonitoringPreconditions(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	var allErrors []error

	// Check for opentelemetry-product operator if either metrics or traces are enabled
	if rr.DSCI.Spec.Monitoring.Metrics != nil || rr.DSCI.Spec.Monitoring.Traces != nil {
		err := checkOperatorSubscription(ctx, rr.Client, opentelemetryOperator)
		if err != nil {
			allErrors = append(allErrors, err)
		}
	}

	// Check for cluster-observability-operator if metrics are enabled
	if rr.DSCI.Spec.Monitoring.Metrics != nil {
		err := checkOperatorSubscription(ctx, rr.Client, clusterObservabilityOperator)
		if err != nil {
			allErrors = append(allErrors, err)
		}
	}

	// Check for tempo-product operator if traces are enabled
	if rr.DSCI.Spec.Monitoring.Traces != nil {
		err := checkOperatorSubscription(ctx, rr.Client, tempoOperator)
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
