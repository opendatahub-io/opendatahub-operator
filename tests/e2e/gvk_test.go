package e2e_test

import "k8s.io/apimachinery/pkg/runtime/schema"

var (
	gvkDataScienceCluster = schema.GroupVersionKind{
		Group:   "datasciencecluster.opendatahub.io",
		Version: "v1",
		Kind:    "DataScienceCluster",
	}
	gvkDSCInitializaion = schema.GroupVersionKind{
		Group:   "dscinitialization.opendatahub.io",
		Version: "v1",
		Kind:    "DSCInitialization",
	}
)
