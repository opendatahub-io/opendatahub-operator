package cluster

const (
	// ManagedRhoai defines expected addon catalogsource.
	ManagedRhoai Platform = "OpenShift AI Cloud Service"
	// SelfManagedRhoai defines display name in csv.
	SelfManagedRhoai Platform = "OpenShift AI Self-Managed"
	// OpenDataHub defines display name in csv.
	OpenDataHub Platform = "Open Data Hub"
	// Unknown indicates that operator is not deployed using OLM.
	Unknown Platform = ""

	// DefaultNotebooksNamespace defines default namespace for notebooks.
	DefaultNotebooksNamespace = "rhods-notebooks"

	// Default cluster-scope Authentication CR name.
	ClusterAuthenticationObj = "cluster"

	// Default OpenShift version CR name.
	OpenShiftVersionObj = "version"
)
