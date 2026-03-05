package common

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
)

// NewScheme creates a new runtime.Scheme with the common types registered
// (core Kubernetes types and apiextensions), plus any additional schemes
// provided by the caller.
func NewScheme(addToSchemes ...func(*runtime.Scheme) error) *runtime.Scheme {
	scheme := runtime.NewScheme()

	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))

	for _, addToScheme := range addToSchemes {
		utilruntime.Must(addToScheme(scheme))
	}

	return scheme
}
