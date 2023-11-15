package serverless

import (
	"context"

	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/gvr"
)

func ServingCertificateResource(f *feature.Feature) error {
	return f.CreateSelfSignedCertificate(f.Spec.KnativeCertificateSecret, f.Spec.Serving.IngressGateway.Certificate.Type, f.Spec.KnativeIngressDomain, f.Spec.ControlPlane.Namespace)
}

func GetDomain(dynamicClient dynamic.Interface) (string, error) {
	cluster, err := dynamicClient.Resource(gvr.OpenshiftIngress).Get(context.TODO(), "cluster", metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	domain, found, err := unstructured.NestedString(cluster.Object, "spec", "domain")
	if !found {
		return "", errors.New("spec.domain not found")
	}
	return domain, err
}
