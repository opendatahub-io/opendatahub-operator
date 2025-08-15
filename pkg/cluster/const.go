package cluster

import "github.com/opendatahub-io/opendatahub-operator/v2/api/common"

const (
	// ManagedRhoai defines expected addon catalogsource.
	ManagedRhoai common.Platform = "OpenShift AI Cloud Service"
	// SelfManagedRhoai defines display name in csv.
	SelfManagedRhoai common.Platform = "OpenShift AI Self-Managed"
	// OpenDataHub defines display name in csv.
	OpenDataHub common.Platform = "Open Data Hub"

	// DefaultNotebooksNamespaceODH defines default namespace for notebooks.
	DefaultNotebooksNamespaceODH = "opendatahub"
	// DefaultNotebooksNamespaceRHOAI defines default namespace for notebooks.
	DefaultNotebooksNamespaceRHOAI = "rhods-notebooks"

	// DefaultMonitoringNamespaceODH defines default namespace for monitoring.
	DefaultMonitoringNamespaceODH = "opendatahub"
	// DefaultMonitoringNamespaceRHOAI defines default namespace for monitoring.
	DefaultMonitoringNamespaceRHOAI = "redhat-ods-monitoring"

	// Default cluster-scope Authentication CR name.
	ClusterAuthenticationObj = "cluster"

	// Default OpenShift version CR name.
	OpenShiftVersionObj = "version"

	// Managed cluster required route.
	NameConsoleLink      = "console"
	NamespaceConsoleLink = "openshift-console"

	// KueueQueueNameLabel is the label key used to specify the Kueue queue name for workloads.
	KueueQueueNameLabel = "kueue.x-k8s.io/queue-name"

	// KueueManagedLabelKey indicates a namespace is managed by Kueue.
	KueueManagedLabelKey = "kueue.openshift.io/managed"

	// KueueLegacyManagedLabelKey is the legacy label key used to indicate a namespace is managed by Kueue.
	KueueLegacyManagedLabelKey = "kueue-managed"
)
