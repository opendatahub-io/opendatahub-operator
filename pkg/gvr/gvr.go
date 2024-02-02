package gvr

import "k8s.io/apimachinery/pkg/runtime/schema"

var (
	KnativeServing = schema.GroupVersionResource{
		Group:    "operator.knative.dev",
		Version:  "v1beta1",
		Resource: "knativeservings",
	}

	OpenshiftIngress = schema.GroupVersionResource{
		Group:    "config.openshift.io",
		Version:  "v1",
		Resource: "ingresses",
	}

	FeatureTracker = schema.GroupVersionResource{
		Group:    "features.opendatahub.io",
		Version:  "v1",
		Resource: "featuretrackers",
	}

	SMCP = schema.GroupVersionResource{
		Group:    "maistra.io",
		Version:  "v2",
		Resource: "servicemeshcontrolplanes",
	}

	NetworkPolicies = schema.GroupVersionResource{
		Group:    "networking.k8s.io",
		Version:  "v1",
		Resource: "networkpolicies",
	}
)
