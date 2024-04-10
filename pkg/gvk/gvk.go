package gvk

import "k8s.io/apimachinery/pkg/runtime/schema"

var (
	OdhApplication = schema.GroupVersionKind{Group: "dashboard.opendatahub.io", Version: "v1", Kind: "OdhApplication"}
	OdhDocument    = schema.GroupVersionKind{Group: "dashboard.opendatahub.io", Version: "v1", Kind: "OdhDocument"}
	OdhQuickStart  = schema.GroupVersionKind{Group: "console.openshift.io", Version: "v1", Kind: "OdhQuickStart"}
)
