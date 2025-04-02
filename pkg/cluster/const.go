package cluster

import "github.com/opendatahub-io/opendatahub-operator/v2/api/common"

const (
	// ManagedRhoai defines expected addon catalogsource.
	ManagedRhoai common.Platform = "OpenShift AI Cloud Service"
	// SelfManagedRhoai defines display name in csv.
	SelfManagedRhoai common.Platform = "OpenShift AI Self-Managed"
	// OpenDataHub defines display name in csv.
	OpenDataHub common.Platform = "Open Data Hub"
	// Unknown indicates that operator is not deployed using OLM.
	Unknown common.Platform = ""

	// DefaultNotebooksNamespace defines default namespace for notebooks.
	DefaultNotebooksNamespace = "rhods-notebooks"

	// Default cluster-scope Authentication CR name.
	ClusterAuthenticationObj = "cluster"

	// Default OpenShift version CR name.
	OpenShiftVersionObj = "version"

	// Managed cluster required route.
	NameConsoleLink      = "console"
	NamespaceConsoleLink = "openshift-console"
)
