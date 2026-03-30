package cloudmanager_test

import (
	"k8s.io/apimachinery/pkg/runtime/schema"

	azurev1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/azure/v1alpha1"
	coreweavev1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/coreweave/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

type ProviderConfig struct {
	Name         string
	GVK          schema.GroupVersionKind
	InstanceName string
}

var providers = map[string]ProviderConfig{
	"azure": {
		Name:         "azure",
		GVK:          gvk.AzureKubernetesEngine,
		InstanceName: azurev1alpha1.AzureKubernetesEngineInstanceName,
	},
	"coreweave": {
		Name:         "coreweave",
		GVK:          gvk.CoreWeaveKubernetesEngine,
		InstanceName: coreweavev1alpha1.CoreWeaveKubernetesEngineInstanceName,
	},
}
