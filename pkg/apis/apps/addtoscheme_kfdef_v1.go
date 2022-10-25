package apps

import (
	v1 "github.com/kubeflow/kfctl/v3/pkg/apis/apps/kfdef/v1"
	ocv1 "github.com/openshift/api/oauth/v1"
	routev1 "github.com/openshift/api/route/v1"
	operatorsv1alpha1 "github.com/operator-framework/operator-lifecycle-manager/pkg/api/apis/operators/v1alpha1"
	apiserv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
)

func init() {
	// Register the types with the Scheme so the components can map objects to GroupVersionKinds and back
	AddToSchemes = append(AddToSchemes,
		v1.SchemeBuilder.AddToScheme,
		operatorsv1alpha1.AddToScheme,
		apiserv1.AddToScheme,
		ocv1.AddToScheme,
		routev1.AddToScheme)
}
