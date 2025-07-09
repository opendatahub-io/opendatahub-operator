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

	replicas := metrics.Replicas // take whatever value user set in DSCI, including 0, or if not set then default to 2

	return map[string]any{
		"CPULimit":            cpuLimit,
		"MemoryLimit":         memoryLimit,
		"CPURequest":          cpuRequest,
		"MemoryRequest":       memoryRequest,
		"StorageSize":         storageSize,
		"StorageRetention":    storageRetention,
		"MonitoringStackName": monitoringStackName,
		"Namespace":           monitoring.Spec.Namespace,
		"Replicas":            strconv.Itoa(int(replicas)),
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
