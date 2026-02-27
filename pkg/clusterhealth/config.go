package clusterhealth

import (
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Config holds the client and namespace/CR names needed to run health checks.
// All inputs are passed explicitly; no package-level globals.
type Config struct {
	Client     client.Client
	Operator   OperatorConfig
	Namespaces NamespaceConfig
	DSCI       types.NamespacedName // DSCI instance to inspect (e.g. default-dsci)
	DSC        types.NamespacedName // DSC instance to inspect (e.g. default-dsc)

	// OnlySections limits which sections to run. Empty or nil = run all.
	// Use section constants (SectionNodes, SectionDeployments, etc.) or layer
	// constants (LayerInfrastructure, LayerWorkload) to run a subset.
	OnlySections []string
	// Layers limits which layers to run when OnlySections is empty. E.g. []string{LayerInfrastructure}
	// runs only nodes and quotas; []string{LayerWorkload} runs deployments, pods, events, operator, DSCI, DSC.
	Layers []string
}

// OperatorConfig configures which operator deployment and namespace to check.
// The deployment name is supplied by the caller (e.g. from platform: ODH vs RHODS).
type OperatorConfig struct {
	Namespace string // operator deployment namespace
	Name      string // deployment name (e.g. opendatahub-operator-controller-manager or rhods-operator)
}

// NamespaceConfig lists namespaces to inspect for deployments, pods, events, quotas.
type NamespaceConfig struct {
	Apps  string   // applications namespace
	Extra []string // e.g. ["kube-system"] for events
}
