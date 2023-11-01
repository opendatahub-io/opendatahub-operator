package gvr

import "k8s.io/apimachinery/pkg/runtime/schema"

var (
	ResourceTracker = schema.GroupVersionResource{
		Group:    "dscinitialization.opendatahub.io",
		Version:  "v1",
		Resource: "featuretrackers",
	}
)
