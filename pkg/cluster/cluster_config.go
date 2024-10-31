package cluster

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/blang/semver/v4"
	"github.com/go-logr/logr"
	"github.com/operator-framework/api/pkg/lib/version"
	ofapiv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

type Platform string

// Release includes information on operator version and platform
// +kubebuilder:object:generate=true
type Release struct {
	Name    Platform                `json:"name,omitempty"`
	Version version.OperatorVersion `json:"version,omitempty"`
}

var clusterConfig struct {
	Namespace string
	Release   Release
}

// Init initializes cluster configuration variables on startup
// init() won't work since it is needed to check the error.
func Init(ctx context.Context, cli client.Client) error {
	var err error
	log := logf.FromContext(ctx)

	clusterConfig.Namespace, err = getOperatorNamespace()
	if err != nil {
		log.Error(err, "unable to find operator namespace")
		// not fatal, fallback to ""
	}

	clusterConfig.Release, err = getRelease(ctx, cli)
	if err != nil {
		return err
	}

	printClusterConfig(log)

	return nil
}

func printClusterConfig(log logr.Logger) {
	log.Info("Cluster config",
		"Namespace", clusterConfig.Namespace,
		"Release", clusterConfig.Release)
}

func GetOperatorNamespace() (string, error) {
	if clusterConfig.Namespace == "" {
		return "", errors.New("unable to find operator namespace")
	}
	return clusterConfig.Namespace, nil
}

func GetRelease() Release {
	return clusterConfig.Release
}

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

func getOperatorNamespace() (string, error) {
	operatorNS, exist := os.LookupEnv("OPERATOR_NAMESPACE")
	if exist && operatorNS != "" {
		return operatorNS, nil
	}
	data, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	return string(data), err
}

func IsNotReservedNamespace(ns *corev1.Namespace) bool {
	return !strings.HasPrefix(ns.GetName(), "openshift-") && !strings.HasPrefix(ns.GetName(), "kube-") &&
		ns.GetName() != "default" && ns.GetName() != "openshift"
}

// GetClusterServiceVersion retries CSV only from the defined namespace.
func GetClusterServiceVersion(ctx context.Context, c client.Client, namespace string) (*ofapiv1alpha1.ClusterServiceVersion, error) {
	clusterServiceVersionList := &ofapiv1alpha1.ClusterServiceVersionList{}
	paginateListOption := &client.ListOptions{
		Limit:     100,
		Namespace: namespace,
	}
	for { // for the case we have very big size of CSV even just in one namespace
		if err := c.List(ctx, clusterServiceVersionList, paginateListOption); err != nil {
			return nil, fmt.Errorf("failed listing cluster service versions for %s: %w", namespace, err)
		}
		for _, csv := range clusterServiceVersionList.Items {
			for _, operatorCR := range csv.Spec.CustomResourceDefinitions.Owned {
				if operatorCR.Kind == "DataScienceCluster" {
					return &csv, nil
				}
			}
		}
		if paginateListOption.Continue = clusterServiceVersionList.GetContinue(); paginateListOption.Continue == "" {
			break
		}
	}

	return nil, k8serr.NewNotFound(
		schema.GroupResource{Group: gvk.ClusterServiceVersion.Group},
		gvk.ClusterServiceVersion.Kind)
}

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

// detectManagedRHODS checks if catsrc CR add-on exists ManagedRhods.
func detectManagedRHODS(ctx context.Context, cli client.Client) (Platform, error) {
	catalogSource := &ofapiv1alpha1.CatalogSource{}
	err := cli.Get(ctx, client.ObjectKey{Name: "addon-managed-odh-catalog", Namespace: "redhat-ods-operator"}, catalogSource)
	if err != nil {
		return Unknown, client.IgnoreNotFound(err)
	}
	return ManagedRhods, nil
}

func getPlatform(ctx context.Context, cli client.Client) (Platform, error) {
	switch os.Getenv("ODH_PLATFORM_TYPE") {
	case "OpenDataHub", "":
		return OpenDataHub, nil
	case "ManagedRHOAI":
		return ManagedRhods, nil
	case "SelfManagedRHOAI":
		return SelfManagedRhods, nil
	default: // fall back to detect platform if ODH_PLATFORM_TYPE env is not provided
		if platform, err := detectManagedRHODS(ctx, cli); err != nil {
			return Unknown, err
		} else if platform == ManagedRhods {
			return ManagedRhods, nil
		}
		return detectSelfManaged(ctx, cli)
	}
}

func getRelease(ctx context.Context, cli client.Client) (Release, error) {
	initRelease := Release{
		// dummy version set to name "", version 0.0.0
		Version: version.OperatorVersion{
			Version: semver.Version{},
		},
	}
	// Set platform
	platform, err := getPlatform(ctx, cli)
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
		// unit test does not have k8s file or env var set, return default
		return initRelease, err
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
