package monitoring

import (
	"context"
	"embed"
	"errors"
	"strconv"

	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

//go:embed resources
var resourcesFS embed.FS

const (
	MonitoringStackTemplate        = "resources/monitoring-stack.tmpl.yaml"
	TempoMonolithicTemplate        = "resources/tempo-monolithic.tmpl.yaml"
	TempoStackTemplate             = "resources/tempo-stack.tmpl.yaml"
	ManagedStackName               = "rhoai-monitoringstack"
	OpenDataHubStackName           = "odh-monitoringstack"
	ManagedTempoMonolithicName     = "rhoai-tempomonolithic"
	OpenDataHubTempoMonolithicName = "odh-tempomonolithic"
	ManagedTempoStackName          = "rhoai-tempostack"
	OpenDataHubTempoStackName      = "odh-tempostack"
)

func getTemplateData(ctx context.Context, rr *odhtypes.ReconciliationRequest) (map[string]any, error) {
	monitoring, ok := rr.Instance.(*serviceApi.Monitoring)
	if !ok {
		return nil, errors.New("instance is not of type *services.Monitoring")
	}

	templateData := map[string]any{
		"Namespace": monitoring.Spec.Namespace,
	}

	// Add metrics data if metrics are configured
	if monitoring.Spec.Metrics != nil {
		var monitoringStackName string
		switch rr.Release.Name {
		case cluster.ManagedRhoai:
			monitoringStackName = ManagedStackName
		case cluster.SelfManagedRhoai:
			monitoringStackName = ManagedStackName
		default:
			monitoringStackName = OpenDataHubStackName
		}
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
		templateData["MonitoringStackName"] = monitoringStackName
		templateData["Replicas"] = strconv.Itoa(int(replicas))
	}

	// Add traces data if traces are configured
	if monitoring.Spec.Traces != nil {
		var tempoName string
		switch rr.Release.Name {
		case cluster.ManagedRhoai:
			if monitoring.Spec.Traces.Storage.Backend == "pv" {
				tempoName = ManagedTempoMonolithicName
			} else {
				tempoName = ManagedTempoStackName
			}
		case cluster.SelfManagedRhoai:
			if monitoring.Spec.Traces.Storage.Backend == "pv" {
				tempoName = ManagedTempoMonolithicName
			} else {
				tempoName = ManagedTempoStackName
			}
		default:
			if monitoring.Spec.Traces.Storage.Backend == "pv" {
				tempoName = OpenDataHubTempoMonolithicName
			} else {
				tempoName = OpenDataHubTempoStackName
			}
		}

		traces := monitoring.Spec.Traces.Storage
		templateData["Backend"] = traces.Backend
		templateData["Secret"] = traces.Secret
		templateData["Size"] = traces.Size
		templateData["TempoName"] = tempoName
	}

	return templateData, nil
}

func ifGVKInstalled(kvg schema.GroupVersionKind) func(context.Context, *odhtypes.ReconciliationRequest) bool {
	return func(ctx context.Context, rr *odhtypes.ReconciliationRequest) bool {
		hasCRD, err := cluster.HasCRD(ctx, rr.Client, kvg)
		if err != nil {
			ctrl.Log.Error(err, "error checking if CRD installed", "GVK", kvg)
			return false
		}
		return hasCRD
	}
}
