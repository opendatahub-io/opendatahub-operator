package serverless

import (
	"context"
	"fmt"

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/servicemesh"
)

func ServingCertificateResource(ctx context.Context, f *feature.Feature) error {
	secretData, err := getSecretParams(f)
	if err != nil {
		return err
	}

	switch secretData.Type {
	case infrav1.SelfSigned:
		return cluster.CreateSelfSignedCertificate(ctx, f.Client,
			secretData.Name,
			secretData.Domain,
			secretData.Namespace,
			feature.OwnedBy(f))
	case infrav1.Provided:
		return nil
	default:
		return cluster.PropagateDefaultIngressCertificate(ctx, f.Client, secretData.Name, secretData.Namespace)
	}
}

type secretParams struct {
	Name      string
	Namespace string
	Domain    string
	Type      infrav1.CertType
}

func getSecretParams(f *feature.Feature) (*secretParams, error) {
	result := &secretParams{}

	serving, err := FeatureData.Serving.Extract(f)
	if err != nil {
		return nil, fmt.Errorf("failed to extract serving data: %w", err)
	}

	result.Name = serving.KnativeCertificateSecret
	result.Domain = serving.KnativeIngressDomain
	result.Type = serving.IngressGateway.Certificate.Type

	if controlPlane, err := servicemesh.FeatureData.ControlPlane.Extract(f); err == nil {
		result.Namespace = controlPlane.Namespace
	} else {
		return nil, fmt.Errorf("failed to extract service mesh data: %w", err)
	}

	return result, nil
}
