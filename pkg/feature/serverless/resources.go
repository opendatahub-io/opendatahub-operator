package serverless

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/servicemesh"
)

func ServingCertificateResource(ctx context.Context, cli client.Client, f *feature.Feature) error {
	secretData, err := getSecretParams(f)
	if err != nil {
		return err
	}

	switch secretData.Type {
	case infrav1.SelfSigned:
		return cluster.CreateSelfSignedCertificate(ctx, cli,
			secretData.Name,
			secretData.Domain,
			secretData.Namespace,
			feature.OwnedBy(f))
	case infrav1.Provided:
		return nil
	default:
		return cluster.PropagateDefaultIngressCertificate(ctx, cli, secretData.Name, secretData.Namespace)
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

	if secret, err := FeatureData.CertificateName.Extract(f); err == nil {
		result.Name = secret
	} else {
		return nil, err
	}

	if domain, err := FeatureData.IngressDomain.Extract(f); err == nil {
		result.Domain = domain
	} else {
		return nil, err
	}

	if serving, err := FeatureData.Serving.Extract(f); err == nil {
		result.Type = serving.IngressGateway.Certificate.Type
	} else {
		return nil, err
	}

	if controlPlane, err := servicemesh.FeatureData.ControlPlane.Extract(f); err == nil {
		result.Namespace = controlPlane.Namespace
	} else {
		return nil, err
	}

	return result, nil
}
