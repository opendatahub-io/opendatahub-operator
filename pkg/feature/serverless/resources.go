package serverless

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
)

func ServingCertificateResource(f *feature.Feature) error {
	return f.CreateSelfSignedCertificate(f.Spec.KnativeCertificateSecret, f.Spec.Serving.IngressGateway.Certificate.Type, f.Spec.KnativeIngressDomain, f.Spec.ControlPlane.Namespace)
}

func GetDomain(c client.Client) (string, error) {
	ingress := &unstructured.Unstructured{}
	ingress.SetGroupVersionKind(cluster.OpenshiftIngressGVK)

	if err := c.Get(context.TODO(), client.ObjectKey{
		Namespace: "",
		Name:      "cluster",
	}, ingress); err != nil {
		return "", fmt.Errorf("failed fetching cluster's ingress details: %w", err)
	}

	domain, found, err := unstructured.NestedString(ingress.Object, "spec", "domain")
	if !found {
		return "", errors.New("spec.domain not found")
	}

	return domain, err
}
