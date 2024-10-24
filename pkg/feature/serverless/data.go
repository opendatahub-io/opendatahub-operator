package serverless

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/provider"
)

const DefaultCertificateSecretName = "knative-serving-cert"

const (
	servingKey              = "Serving"
	certificateKey          = "KnativeCertificateSecret"
	knativeIngressDomainKey = "KnativeIngressDomain"
)

// FeatureData is a convention to simplify how the data for the Serverless features is Defined and accessed.
var FeatureData = struct {
	Serving         feature.DataDefinition[infrav1.ServingSpec, infrav1.ServingSpec]
	CertificateName feature.DataDefinition[infrav1.ServingSpec, string]
	IngressDomain   feature.DataDefinition[infrav1.ServingSpec, string]
}{
	Serving: feature.DataDefinition[infrav1.ServingSpec, infrav1.ServingSpec]{
		Define: func(source *infrav1.ServingSpec) feature.DataEntry[infrav1.ServingSpec] {
			return feature.DataEntry[infrav1.ServingSpec]{
				Key:   servingKey,
				Value: provider.ValueOf(*source).Get,
			}
		},
		Extract: feature.ExtractEntry[infrav1.ServingSpec](servingKey),
	},
	CertificateName: feature.DataDefinition[infrav1.ServingSpec, string]{
		Define: func(source *infrav1.ServingSpec) feature.DataEntry[string] {
			return feature.DataEntry[string]{
				Key:   certificateKey,
				Value: provider.ValueOf(source.IngressGateway.Certificate.SecretName).OrElse(DefaultCertificateSecretName),
			}
		},
		Extract: feature.ExtractEntry[string](certificateKey),
	},
	IngressDomain: feature.DataDefinition[infrav1.ServingSpec, string]{
		Define: func(source *infrav1.ServingSpec) feature.DataEntry[string] {
			return feature.DataEntry[string]{
				Key:   knativeIngressDomainKey,
				Value: provider.ValueOf(source.IngressGateway.Domain).OrGet(knativeDomain),
			}
		},
		Extract: feature.ExtractEntry[string](knativeIngressDomainKey),
	},
}

func knativeDomain(ctx context.Context, c client.Client) (string, error) {
	var errDomain error
	domain, errDomain := cluster.GetDomain(ctx, c)
	if errDomain != nil {
		return "", fmt.Errorf("failed to fetch OpenShift domain to generate certificate for Serverless: %w", errDomain)
	}

	domain = "*." + domain
	return domain, nil
}
