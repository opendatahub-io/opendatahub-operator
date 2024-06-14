package cluster

const (
	// ManagedRhods defines expected addon catalogsource.
	ManagedRhods Platform = "Managed Red Hat OpenShift AI"
	// SelfManagedRhods defines display name in csv.
	SelfManagedRhods Platform = "Self Managed Red Hat OpenShift AI"
	// OpenDataHub defines display name in csv.
	OpenDataHub Platform = "Open Data Hub Operator"
	// Unknown indicates that operator is not deployed using OLM.
	Unknown Platform = ""
)
