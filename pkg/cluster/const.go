package cluster

const (
	// ManagedRhods defines expected addon catalogsource.
	ManagedRhods Platform = "OpenShift AI Cloud Service"
	// SelfManagedRhods defines display name in csv.
	SelfManagedRhods Platform = "OpenShift AI Self-Managed"
	// OpenDataHub defines display name in csv.
	OpenDataHub Platform = "Open Data Hub Operator"
	// Unknown indicates that operator is not deployed using OLM.
	Unknown Platform = ""
)
