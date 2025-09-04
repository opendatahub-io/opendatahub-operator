package scheme

import (
	oauthv1 "github.com/openshift/api/oauth/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	routev1 "github.com/openshift/api/route/v1"
	userv1 "github.com/openshift/api/user/v1"
	ofapi "github.com/operator-framework/api/pkg/operators/v1alpha1"
	ofapiv2 "github.com/operator-framework/api/pkg/operators/v2"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	admissionv1 "k8s.io/api/admission/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	featurev1 "github.com/opendatahub-io/opendatahub-operator/v2/api/features/v1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
)

var (
	DefaultAddToSchemes = []func(*runtime.Scheme) error{
		clientgoscheme.AddToScheme,
		routev1.AddToScheme,
		dsciv2.AddToScheme,
		dscv2.AddToScheme,
		userv1.AddToScheme,
		dsciv1.AddToScheme,
		dscv1.AddToScheme,
		featurev1.AddToScheme,
		monitoringv1.AddToScheme,
		ofapi.AddToScheme,
		operatorv1.AddToScheme,
		componentApi.AddToScheme,
		apiextensionsv1.AddToScheme,
		oauthv1.AddToScheme,
		ofapiv2.AddToScheme,
		coordinationv1.AddToScheme,
		serviceApi.AddToScheme,
		admissionv1.AddToScheme,
		infrav1.AddToScheme,
	}
)

func New() (*runtime.Scheme, error) {
	s := runtime.NewScheme()

	for _, at := range DefaultAddToSchemes {
		if err := at(s); err != nil {
			return nil, err
		}
	}

	return s, nil
}
