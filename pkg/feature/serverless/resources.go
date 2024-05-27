package serverless

import (
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
)

func ServingCertificateResource(f *feature.Feature) error {
	return f.CreateSelfSignedCertificate(f.Spec.KnativeCertificateSecret, f.Spec.Serving.IngressGateway.Certificate.Type, f.Spec.KnativeIngressDomain, f.Spec.ControlPlane.Namespace)
}
