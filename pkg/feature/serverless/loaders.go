package serverless

import (
	"context"
	"fmt"
	"strings"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
)

const DefaultCertificateSecretName = "knative-serving-cert"

func ServingDefaultValues(_ context.Context, f *feature.Feature) error {
	certificateSecretName := strings.TrimSpace(f.Spec.Serving.IngressGateway.Certificate.SecretName)
	if len(certificateSecretName) == 0 {
		certificateSecretName = DefaultCertificateSecretName
	}

	f.Spec.KnativeCertificateSecret = certificateSecretName
	return nil
}

func ServingIngressDomain(ctx context.Context, f *feature.Feature) error {
	domain := strings.TrimSpace(f.Spec.Serving.IngressGateway.Domain)
	if len(domain) == 0 {
		var errDomain error
		domain, errDomain = cluster.GetDomain(ctx, f.Client)
		if errDomain != nil {
			return fmt.Errorf("failed to fetch OpenShift domain to generate certificate for Serverless: %w", errDomain)
		}

		domain = "*." + domain
	}

	f.Spec.KnativeIngressDomain = domain
	return nil
}
