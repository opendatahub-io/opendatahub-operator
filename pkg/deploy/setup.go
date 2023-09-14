package deploy

import (
	"context"
	"strings"

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

// isSelfManaged checks presence of ClusterServiceVersions:
// when CSV displayname contains OpenDataHub, return 'OpenDataHub,nil' => high priority
// when CSV displayname contains SelfManagedRhods, return 'SelfManagedRhods,nil'
// when in dev mode and  could not find CSV (deploy by olm), return "", nil
// otherwise return "",err
func isSelfManaged(cli client.Client) (Platform, error) {
	clusterCsvs := &ofapi.ClusterServiceVersionList{}
	err := cli.List(context.TODO(), clusterCsvs)
	if err != nil {
		return "", err
	} else {
		for _, csv := range clusterCsvs.Items {
			if strings.Contains(csv.Spec.DisplayName, string(OpenDataHub)) {
				return OpenDataHub, nil
			}
			if strings.Contains(csv.Spec.DisplayName, string(SelfManagedRhods)) {
				return SelfManagedRhods, nil

			}
		}
	}
	return "", nil
}

// isManagedRHODS checks if CRD add-on exists and contains string ManagedRhods
func isManagedRHODS(cli client.Client) (Platform, error) {
	addonCRD := &apiextv1.CustomResourceDefinition{}

	err := cli.Get(context.TODO(), client.ObjectKey{Name: "addons.managed.openshift.io"}, addonCRD)
	if err != nil {
		if apierrs.IsNotFound(err) {
			return "", nil
		}
		return "", err
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
	// First check if its addon installation to return 'ManagedRhods, nil'
	if platform, err := isManagedRHODS(cli); err != nil {
		return "", err
	} else if platform == ManagedRhods {
		return ManagedRhods, nil
	}

	// check and return whether ODH or self-managed platform
	return isSelfManaged(cli)
}
