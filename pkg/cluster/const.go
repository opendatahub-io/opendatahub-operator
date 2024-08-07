package cluster

const (
	// ManagedRhods defines expected addon catalogsource.
	ManagedRhods Platform = "OpenShift AI Cloud Service"
	// SelfManagedRhods defines display name in csv.
	SelfManagedRhods Platform = "OpenShift AI Self-Managed"
	// OpenDataHub defines display name in csv.
	OpenDataHub Platform = "Open Data Hub"
	// Unknown indicates that operator is not deployed using OLM.
	Unknown Platform = ""

	// Opendatahub default applicatiaon namespace.
	// Even we have the capability to config to use different namespace, but for now, it is hardcoded to only use this namespace.
	ODHApplicationNamespace = "opendatahub"
	// RHOAI default applicatiaon namespace.
	// RHOAI does not officially support using a different application namespace, as not being verified.
	RHOAIApplicationNamespace = "redhat-ods-applications"
)
