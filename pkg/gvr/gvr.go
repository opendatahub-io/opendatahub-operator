package gvr

import "k8s.io/apimachinery/pkg/runtime/schema"

var (
	OpenshiftIngress = schema.GroupVersionResource{
		Group:    "config.openshift.io",
		Version:  "v1",
		Resource: "ingresses",
	}

	ResourceTracker = schema.GroupVersionResource{
		Group:    "dscinitialization.opendatahub.io",
		Version:  "v1",
		Resource: "featuretrackers",
	}

	ODHDashboardConfigGVR = schema.GroupVersionResource{
		Group:    "opendatahub.io",
		Version:  "v1alpha",
		Resource: "odhdashboardconfigs",
	}

	SMCP = schema.GroupVersionResource{
		Group:    "maistra.io",
		Version:  "v2",
		Resource: "servicemeshcontrolplanes",
	}

	OAuthClient = schema.GroupVersionResource{
		Group:    "oauth.openshift.io",
		Version:  "v1",
		Resource: "oauthclients",
	}
)
