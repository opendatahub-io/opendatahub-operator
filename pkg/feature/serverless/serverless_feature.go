package serverless

import (
	"context"
	"errors"
	"path"
	"path/filepath"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	ctrlLog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/servicemesh"
)

const templatesDir = "templates/serverless"

var log = ctrlLog.Log.WithName("features")

func ConfigureServerlessFeatures(s *feature.FeaturesInitializer) error {
	var rootDir = filepath.Join(feature.BaseOutputDir, s.DSCInitializationSpec.ApplicationsNamespace)
	if err := feature.CopyEmbeddedFiles(templatesDir, rootDir); err != nil {
		return err
	}

	serverlessSpec := s.Serverless

	servingDeployment, err := feature.CreateFeature("serverless-serving-deployment").
		For(s.DSCInitializationSpec).
		Manifests(
			path.Join(rootDir, templatesDir, "serving-install"),
		).
		PreConditions(
			EnsureServerlessOperatorInstalled,
			EnsureServerlessAbsent,
			servicemesh.EnsureServiceMeshInstalled,
			feature.CreateNamespace(serverlessSpec.Serving.Namespace),
		).
		PostConditions(
			feature.WaitForPodsToBeReady(serverlessSpec.Serving.Namespace),
		).
		Load()
	if err != nil {
		return err
	}
	s.Features = append(s.Features, servingDeployment)

	servingIstioGateways, err := feature.CreateFeature("serverless-serving-gateways").
		For(s.DSCInitializationSpec).
		PreConditions(
			// Check serverless is installed
			feature.WaitForResourceToBeCreated(serverlessSpec.Serving.Namespace, schema.GroupVersionResource{
				Group:    "operator.knative.dev",
				Version:  "v1beta1",
				Resource: "knativeservings",
			}),
		).
		WithResources(ServingCertificateResource).
		Manifests(
			path.Join(rootDir, templatesDir, "serving-istio-gateways"),
		).
		Load()
	if err != nil {
		return err
	}
	s.Features = append(s.Features, servingIstioGateways)

	return nil
}

func GetDomain(dynamicClient dynamic.Interface) (string, error) {
	gvrIngress := schema.GroupVersionResource{
		Group:    "config.openshift.io",
		Version:  "v1",
		Resource: "ingresses",
	}

	cluster, err := dynamicClient.Resource(gvrIngress).Get(context.TODO(), "cluster", metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	domain, found, err := unstructured.NestedString(cluster.Object, "spec", "domain")
	if !found {
		return "", errors.New("spec.domain not found")
	}
	return domain, err
}
