package monitoring

import (
	"context"
	"embed"
	"errors"

	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

//go:embed resources
var resourcesFS embed.FS

const (
	MonitoringStackTemplate = "resources/monitoring-stack.tmpl.yaml"
	ManagedStackName        = "rhoai-monitoringstack"
	OpenDataHubStackName    = "odh-monitoringstack"
)

func getTemplateData(ctx context.Context, rr *odhtypes.ReconciliationRequest) (map[string]any, error) {
	monitoring, ok := rr.Instance.(*serviceApi.Monitoring)
	if !ok {
		return nil, errors.New("instance is not of type *services.Monitoring")
	}

	if monitoring.Spec.Metrics == nil {
		return nil, nil
	}

	var monitoringStackName string
	switch rr.Release.Name {
	case cluster.ManagedRhoai:
		monitoringStackName = ManagedStackName
	case cluster.SelfManagedRhoai:
		monitoringStackName = ManagedStackName
	default:
		monitoringStackName = OpenDataHubStackName
	}

	defaultIfEmpty := func(value, defaultVal string) string {
		if value == "" {
			return defaultVal
		}
		return value
	}

	var cpuLimit, memoryLimit, cpuRequest, memoryRequest string
	metrics := monitoring.Spec.Metrics
	if metrics.Resources != nil {
		cpuLimit = defaultIfEmpty(metrics.Resources.CPULimit, "500m")
		memoryLimit = defaultIfEmpty(metrics.Resources.MemoryLimit, "512Mi")
		cpuRequest = defaultIfEmpty(metrics.Resources.CPURequest, "100m")
		memoryRequest = defaultIfEmpty(metrics.Resources.MemoryRequest, "256Mi")
	} else {
		// No resources configured, use all defaults
		cpuLimit = "500m"
		memoryLimit = "512Mi"
		cpuRequest = "100m"
		memoryRequest = "256Mi"
	}

	var storageSize, storageRetention string
	if metrics.Storage != nil {
		storageSize = defaultIfEmpty(metrics.Storage.Size, "5Gi")
		storageRetention = defaultIfEmpty(metrics.Storage.Retention, "1d")
	} else {
		// No storage configured, use all defaults
		storageSize = "5Gi"
		storageRetention = "1d"
	}

	return map[string]any{
		"CPULimit":            cpuLimit,
		"MemoryLimit":         memoryLimit,
		"CPURequest":          cpuRequest,
		"MemoryRequest":       memoryRequest,
		"StorageSize":         storageSize,
		"StorageRetention":    storageRetention,
		"MonitoringStackName": monitoringStackName,
		"Namespace":           monitoring.Spec.Namespace,
	}, nil
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
