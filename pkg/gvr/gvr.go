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

	ResourceTracker = schema.GroupVersionResource{
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

	JupyterhubApp = schema.GroupVersionResource{
		Group:    "dashboard.opendatahub.io",
		Version:  "v1",
		Resource: "odhapplications",
	}

	Deployment = schema.GroupVersionResource{
		Group:    "apps",
		Version:  "v1",
		Resource: "Deployment",
	}

	Prometheus = schema.GroupVersionResource{
		Group:    "monitoring.coreos.com",
		Version:  "v1",
		Resource: "Prometheus",
	}

	Route = schema.GroupVersionResource{
		Group:    "route.openshift.io",
		Version:  "v1",
		Resource: "Route",
	}

	Secret = schema.GroupVersionResource{
		Group:    " ",
		Version:  "v1",
		Resource: "Secret",
	}

	Service = schema.GroupVersionResource{
		Group:    " ",
		Version:  "v1",
		Resource: "Service",
	}

	ServiceAccount = schema.GroupVersionResource{
		Group:    " ",
		Version:  "v1",
		Resource: "ServiceAccount",
	}

	ServiceMonitor = schema.GroupVersionResource{
		Group:    "monitoring.coreos.com",
		Version:  "v1",
		Resource: "ServiceMonitor",
	}

	ClusterRole = schema.GroupVersionResource{
		Group:    "rbac.authorization.k8s.io",
		Version:  "v1",
		Resource: "ClusterRole",
	}

	ClusterRoleBinding = schema.GroupVersionResource{
		Group:    "rbac.authorization.k8s.io",
		Version:  "v1",
		Resource: "ClusterRoleBinding",
	}
)
