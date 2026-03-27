package clusterhealth

import (
	"strings"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Config holds the client and namespace needed to run health checks.
// All inputs are passed explicitly; no package-level globals.
type Config struct {
	Client     client.Client
	Operator   OperatorConfig
	Namespaces NamespaceConfig
	DSCI       types.NamespacedName
	DSC        types.NamespacedName
	// OnlySections limits which sections to run. Empty or nil = run all.
	// Use section constants (SectionNodes, SectionDeployments, etc.) or layer
	// constants (LayerInfrastructure, LayerWorkload, LayerOperator) to run a subset.
	OnlySections []string
	// Layers limits which sections to run. Empty or nil = run all.
	Layers []string
}

// OperatorConfig configures which operator deployment and namespace to check.
// The deployment name is supplied by the caller (e.g. from platform: ODH vs RHODS operator).
type OperatorConfig struct {
	Namespace string
	Name      string
}

// NamespaceConfig configures which namespaces to scan for deployments, pods, events, quotas.
// None are required; empty or zero values are valid. If List() returns no namespaces,
// those sections return empty data with no error. The Nodes section does not use namespaces.
type NamespaceConfig struct {
	Apps       string
	Monitoring string
	Extra      []string
}

// List returns the deduplicated list of namespaces to scan, skipping empty ones.
// When Apps and Monitoring point to the same namespace (common in ODH), the
// namespace appears only once to avoid duplicate API calls and metric lines.
func (n NamespaceConfig) List() []string {
	seen := make(map[string]bool)
	var out []string
	add := func(ns string) {
		ns = strings.TrimSpace(ns)
		if ns != "" && !seen[ns] {
			seen[ns] = true
			out = append(out, ns)
		}
	}
	add(n.Apps)
	add(n.Monitoring)
	for _, s := range n.Extra {
		add(s)
	}
	return out
}
