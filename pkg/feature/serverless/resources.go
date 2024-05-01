package serverless

import (
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
)

func ServingCertificateResource(f *feature.Feature) error {
	switch certType := f.Spec.Serving.IngressGateway.Certificate.Type; certType {
	case infrav1.SelfSigned:
		return f.CreateSelfSignedCertificate(f.Spec.KnativeCertificateSecret, f.Spec.KnativeIngressDomain, f.Spec.ControlPlane.Namespace)
	case infrav1.Provided:
		return nil
	default:
		return f.GetDefaultIngressCertificate(f.Spec.ControlPlane.Namespace)
	}
}
