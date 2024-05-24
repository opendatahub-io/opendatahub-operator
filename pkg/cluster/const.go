package cluster

const (
	// ManagedRhods defines expected addon catalogsource.
	ManagedRhods Platform = "addon-managed-odh-catalog"
	// SelfManagedRhods defines display name in csv.
	SelfManagedRhods Platform = "Red Hat OpenShift AI"
	// OpenDataHub defines display name in csv.
	OpenDataHub Platform = "Open Data Hub Operator"
	// Unknown indicates that operator is not deployed using OLM.
	Unknown Platform = ""
)
