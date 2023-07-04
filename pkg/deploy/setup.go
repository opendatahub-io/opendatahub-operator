package deploy

import (
	"context"

	addonv1alpha1 "github.com/openshift/addon-operator/apis/addons/v1alpha1"
	ofapi "github.com/operator-framework/api/pkg/operators/v1alpha1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// ManagedRhods defines expected addon catalogsource
	ManagedRhods Platform = "managed-odh"
	// SelfManagedRhods defines display name in csv
	SelfManagedRhods Platform = "Red Hat OpenShift Data Science"
	// OpenDataHub defines display name in csv
	OpenDataHub Platform = "Open Data Hub Operator"
)

type Platform string

func isSelfManaged(cli client.Client) (Platform, error) {
	clusterCsvs := &ofapi.ClusterServiceVersionList{}
	err := cli.List(context.TODO(), clusterCsvs)
	if err != nil {
		if apierrs.IsNotFound(err) {
			return "", nil
		} else {
			return "", err
		}
	} else {
		for _, csv := range clusterCsvs.Items {
			if csv.Spec.DisplayName == string(OpenDataHub) {
				return OpenDataHub, nil
			}
			if csv.Spec.DisplayName == string(SelfManagedRhods) {
				return SelfManagedRhods, nil

			}
		}

	}
	return "", err
}
func isManagedRHODS(cli client.Client) (Platform, error) {
	addonCRD := &apiextv1.CustomResourceDefinition{}

	err := cli.Get(context.TODO(), client.ObjectKey{Name: "addons.managed.openshift.io"}, addonCRD)
	if err != nil {
		if apierrs.IsNotFound(err) {
			// self managed service
			return "", nil
		} else {
			return "", err
		}
	} else {
		expectedAddon := &addonv1alpha1.Addon{}
		err := cli.Get(context.TODO(), client.ObjectKey{Name: string(ManagedRhods)}, expectedAddon)
		if err != nil {
			if apierrs.IsNotFound(err) {
				return "", nil
			} else {
				return "", err
			}
		}
		return ManagedRhods, nil
	}
}

func GetPlatform(cli client.Client) (Platform, error) {
	// First check if its addon installation
	if platform, err := isManagedRHODS(cli); err == nil {
		return platform, nil
	}

	// return self-managed platform
	return isSelfManaged(cli)
}
