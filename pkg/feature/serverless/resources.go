package serverless

import (
	"context"

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
)

func ServingCertificateResource(ctx context.Context, f *feature.Feature) error {
	switch certType := f.Spec.Serving.IngressGateway.Certificate.Type; certType {
	case infrav1.SelfSigned:
		return cluster.CreateSelfSignedCertificate(ctx, f.Client,
			f.Spec.KnativeCertificateSecret,
			f.Spec.KnativeIngressDomain,
			f.Spec.ControlPlane.Namespace,
			feature.OwnedBy(f))
	case infrav1.Provided:
		return nil
	default:
		return cluster.PropagateDefaultIngressCertificate(ctx, f.Client, f.Spec.KnativeCertificateSecret, f.Spec.ControlPlane.Namespace)
	}
}
