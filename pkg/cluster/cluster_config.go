package cluster

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/blang/semver/v4"
	"github.com/operator-framework/api/pkg/lib/version"
	ofapi "github.com/operator-framework/api/pkg/operators/v1alpha1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

// +kubebuilder:rbac:groups="config.openshift.io",resources=ingresses,verbs=get

func GetDomain(c client.Client) (string, error) {
	ingress := &unstructured.Unstructured{}
	ingress.SetGroupVersionKind(gvk.OpenshiftIngress)

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

	return nil, apierrs.NewNotFound(
		schema.GroupResource{Group: gvk.ClusterServiceVersion.Group},
		gvk.ClusterServiceVersion.Kind)
}

type Platform string

// isSelfManaged checks presence of ClusterServiceVersions:
// when CSV displayname contains OpenDataHub, return 'OpenDataHub,nil' => high priority
// when CSV displayname contains SelfManagedRhods, return 'SelfManagedRhods,nil'
// when in dev mode and  could not find CSV (deploy by olm), return "", nil
// otherwise return "",err.
func isSelfManaged(cli client.Client) (Platform, error) {
	clusterCsvs := &ofapi.ClusterServiceVersionList{}
	err := cli.List(context.TODO(), clusterCsvs)
	if err != nil {
		return "", err
	} else { //nolint:golint,revive // Readability on else
		for _, csv := range clusterCsvs.Items {
			if strings.Contains(csv.Spec.DisplayName, string(OpenDataHub)) {
				return OpenDataHub, nil
			}
			if strings.Contains(csv.Spec.DisplayName, string(SelfManagedRhods)) {
				return SelfManagedRhods, nil
			}
		}
	}

	return Unknown, nil
}

// isManagedRHODS checks if CRD add-on exists and contains string ManagedRhods.
func isManagedRHODS(cli client.Client) (Platform, error) {
	catalogSourceCRD := &apiextv1.CustomResourceDefinition{}

	err := cli.Get(context.TODO(), client.ObjectKey{Name: "catalogsources.operators.coreos.com"}, catalogSourceCRD)
	if err != nil {
		return "", client.IgnoreNotFound(err)
	}
	expectedCatlogSource := &ofapi.CatalogSourceList{}
	err = cli.List(context.TODO(), expectedCatlogSource)
	if err != nil {
		return Unknown, err
	}
	if len(expectedCatlogSource.Items) > 0 {
		for _, cs := range expectedCatlogSource.Items {
			if cs.Name == string(ManagedRhods) {
				return ManagedRhods, nil
			}
		}
	}

	return "", nil
}

func GetPlatform(cli client.Client) (Platform, error) {
	// First check if its addon installation to return 'ManagedRhods, nil'
	if platform, err := isManagedRHODS(cli); err != nil {
		return Unknown, err
	} else if platform == ManagedRhods {
		return ManagedRhods, nil
	}

	// check and return whether ODH or self-managed platform
	return isSelfManaged(cli)
}

// Release includes information on operator version and platform
// +kubebuilder:object:generate=true
type Release struct {
	Name    Platform                `json:"name,omitempty"`
	Version version.OperatorVersion `json:"version,omitempty"`
}

func GetRelease(cli client.Client) (Release, error) {
	initRelease := Release{
		// dummy version set to name "", version 0.0.0
		Version: version.OperatorVersion{
			Version: semver.Version{},
		},
	}
	// Set platform
	platform, err := GetPlatform(cli)
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
	csv, err := GetClusterServiceVersion(context.TODO(), cli, operatorNamespace)
	if apierrs.IsNotFound(err) {
		// hide not found, return default
		return initRelease, nil
	}
	if err != nil {
		return initRelease, err
	}
	initRelease.Version = csv.Spec.Version
	return initRelease, nil
}
