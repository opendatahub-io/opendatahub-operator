package clusterhealth

import (
	"strings"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
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
	// Clientset enables pod log capture for unhealthy containers. When nil, log
	// capture is skipped and ContainerInfo.Logs stays empty. The controller-runtime
	// client.Client does not support log streaming, so a separate clientset is needed.
	Clientset *kubernetes.Clientset
	// LogTailLines is the number of log lines to capture per problematic container.
	// Zero means use the default (50). Negative means skip log capture entirely.
	LogTailLines int64
}

const defaultLogTailLines int64 = 50

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

// List returns the list of namespaces to scan, skipping empty ones.
func (n NamespaceConfig) List() []string {
	var out []string
	if n.Apps != "" {
		out = append(out, n.Apps)
	}
	if n.Monitoring != "" {
		out = append(out, n.Monitoring)
	}
	for _, s := range n.Extra {
		if trimmed := strings.TrimSpace(s); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
