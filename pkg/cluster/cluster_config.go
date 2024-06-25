package cluster

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/blang/semver/v4"
	"github.com/operator-framework/api/pkg/lib/version"
	ofapi "github.com/operator-framework/api/pkg/operators/v1alpha1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

// +kubebuilder:rbac:groups="config.openshift.io",resources=ingresses,verbs=get

func GetDomain(ctx context.Context, c client.Client) (string, error) {
	ingress := &unstructured.Unstructured{}
	ingress.SetGroupVersionKind(gvk.OpenshiftIngress)

	if err := c.Get(ctx, client.ObjectKey{
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

func GetOperatorNamespace() (string, error) {
	data, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	return string(data), err
}

// GetClusterServiceVersion retries the clusterserviceversions available in the operator namespace.
func GetClusterServiceVersion(ctx context.Context, c client.Client, watchNameSpace string) (*ofapi.ClusterServiceVersion, error) {
	clusterServiceVersionList := &ofapi.ClusterServiceVersionList{}
	if err := c.List(ctx, clusterServiceVersionList, client.InNamespace(watchNameSpace)); err != nil {
		return nil, fmt.Errorf("failed listign cluster service versions: %w", err)
	}

	for _, csv := range clusterServiceVersionList.Items {
		for _, operatorCR := range csv.Spec.CustomResourceDefinitions.Owned {
			if operatorCR.Kind == "DataScienceCluster" {
				return &csv, nil
			}
		}
	}

	return nil, k8serr.NewNotFound(
		schema.GroupResource{Group: gvk.ClusterServiceVersion.Group},
		gvk.ClusterServiceVersion.Kind)
}

type Platform string

// detectSelfManaged detects if it is Self Managed Rhods or OpenDataHub.
func detectSelfManaged(ctx context.Context, cli client.Client) (Platform, error) {
	variants := map[string]Platform{
		"opendatahub-operator": OpenDataHub,
		"rhods-operator":       SelfManagedRhods,
	}

	for k, v := range variants {
		exists, err := OperatorExists(ctx, cli, k)
		if err != nil {
			return Unknown, err
		}
		if exists {
			return v, nil
		}
	}

	return Unknown, nil
}

// detectManagedRHODS checks if CRD add-on exists and contains string ManagedRhods.
func detectManagedRHODS(ctx context.Context, cli client.Client) (Platform, error) {
	catalogSourceCRD := &apiextv1.CustomResourceDefinition{}

	err := cli.Get(ctx, client.ObjectKey{Name: "catalogsources.operators.coreos.com"}, catalogSourceCRD)
	if err != nil {
		return "", client.IgnoreNotFound(err)
	}
	expectedCatlogSource := &ofapi.CatalogSourceList{}
	err = cli.List(ctx, expectedCatlogSource)
	if err != nil {
		return Unknown, err
	}
	if len(expectedCatlogSource.Items) > 0 {
		for _, cs := range expectedCatlogSource.Items {
			if cs.Name == "addon-managed-odh-catalog" {
				return ManagedRhods, nil
			}
		}
	}

	return "", nil
}

func GetPlatform(ctx context.Context, cli client.Client) (Platform, error) {
	// First check if its addon installation to return 'ManagedRhods, nil'
	if platform, err := detectManagedRHODS(ctx, cli); err != nil {
		return Unknown, err
	} else if platform == ManagedRhods {
		return ManagedRhods, nil
	}

	// check and return whether ODH or self-managed platform
	return detectSelfManaged(ctx, cli)
}

// Release includes information on operator version and platform
// +kubebuilder:object:generate=true
type Release struct {
	Name    Platform                `json:"name,omitempty"`
	Version version.OperatorVersion `json:"version,omitempty"`
}

func GetRelease(ctx context.Context, cli client.Client) (Release, error) {
	initRelease := Release{
		// dummy version set to name "", version 0.0.0
		Version: version.OperatorVersion{
			Version: semver.Version{},
		},
	}
	// Set platform
	platform, err := GetPlatform(ctx, cli)
	if err != nil {
		return initRelease, err
	}
	initRelease.Name = platform

	// For unit-tests
	if os.Getenv("CI") == "true" {
		return initRelease, nil
	}
	// Set Version
	// Get watchNamespace
	operatorNamespace, err := GetOperatorNamespace()
	if err != nil {
		// unit test does not have k8s file
		fmt.Printf("Falling back to dummy version: %v\n", err)
		return initRelease, nil
	}
	csv, err := GetClusterServiceVersion(ctx, cli, operatorNamespace)
	if k8serr.IsNotFound(err) {
		// hide not found, return default
		return initRelease, nil
	}
	if err != nil {
		return initRelease, err
	}
	initRelease.Version = csv.Spec.Version
	return initRelease, nil
}
