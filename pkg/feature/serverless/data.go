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

const (
	servingKey                   = "Serving"
	DefaultCertificateSecretName = "knative-serving-cert"
)

// FeatureData is a convention to simplify how the data for the Serverless features is Defined and accessed.
var FeatureData = struct {
	Serving feature.DataDefinition[*infrav1.ServingSpec, Serving]
}{
	Serving: feature.DataDefinition[*infrav1.ServingSpec, Serving]{
		Create:  CreateServingConfig,
		Extract: feature.ExtractEntry[Serving](servingKey),
	},
}

type Serving struct {
	*infrav1.ServingSpec
	KnativeCertificateSecret,
	KnativeIngressDomain string
}

func CreateServingConfig(ctx context.Context, cli client.Client, source *infrav1.ServingSpec) (Serving, error) {
	certificateName := provider.ValueOf(source.IngressGateway.Certificate.SecretName).OrElse(DefaultCertificateSecretName)
	domain, errGet := provider.ValueOf(source.IngressGateway.Domain).OrGet(func() (string, error) {
		return KnativeDomain(ctx, cli)
	}).Get()
	if errGet != nil {
		return Serving{}, fmt.Errorf("failed to get domain for Knative: %w", errGet)
	}

	config := Serving{
		ServingSpec:              source,
		KnativeCertificateSecret: certificateName,
		KnativeIngressDomain:     domain,
	}

	return config, nil
}

var _ feature.Entry = &Serving{}

func (s Serving) AddTo(f *feature.Feature) error {
	return f.Set(servingKey, s)
}

func KnativeDomain(ctx context.Context, c client.Client) (string, error) {
	var errDomain error
	domain, errDomain := cluster.GetDomain(ctx, c)
	if errDomain != nil {
		return "", fmt.Errorf("failed to fetch OpenShift domain to generate certificate for Serverless: %w", errDomain)
	}

	domain = "*." + domain
	return domain, nil
}
