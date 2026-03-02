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

	// DefaultApplicationNamespaceODH defines default application namespace for ODH.
	DefaultApplicationNamespaceODH = "opendatahub"
	// DefaultApplicationNamespaceRHOAI defines default application namespace for RHOAI.
	DefaultApplicationNamespaceRHOAI = "redhat-ods-applications"

	// ODHPlatformTypeEnv is the environment variable used to set platform (OpenDataHub, ManagedRHOAI, SelfManagedRHOAI).
	ODHPlatformTypeEnv = "ODH_PLATFORM_TYPE"
	// PlatformDetectionLogName is the logger name used for platform detection logs.
	PlatformDetectionLogName = "platform-detection"
	// RhodsOperatorPrefix is the operator prefix used to detect Self-Managed RHOAI.
	RhodsOperatorPrefix = "rhods-operator"
	// DefaultOperatorNamespaceCatalog is the default namespace for catalog lookup when operator namespace is unknown.
	DefaultOperatorNamespaceCatalog = "redhat-ods-operator"
	// ManagedRhoaiCatalogName is the CatalogSource name used to detect Managed RHOAI.
	ManagedRhoaiCatalogName = "addon-managed-odh-catalog"
	// ApplicationNamespaceLabelKey is the label key for the application namespace.
	ApplicationNamespaceLabelKey = "opendatahub.io/application-namespace"

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
