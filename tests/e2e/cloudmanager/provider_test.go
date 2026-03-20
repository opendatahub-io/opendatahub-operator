package cloudmanager_test

import (
	"k8s.io/apimachinery/pkg/runtime/schema"

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
		InstanceName: "default-azurekubernetesengine",
	},
	"coreweave": {
		Name:         "coreweave",
		GVK:          gvk.CoreWeaveKubernetesEngine,
		InstanceName: "default-coreweavekubernetesengine",
	},
}
