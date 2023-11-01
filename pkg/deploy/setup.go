package deploy

import (
	"context"
	"strings"

	ofapi "github.com/operator-framework/api/pkg/operators/v1alpha1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// ManagedRhods defines expected addon catalogsource.
	ManagedRhods Platform = "addon-managed-odh-catalog"
	// SelfManagedRhods defines display name in csv.
	SelfManagedRhods Platform = "Red Hat OpenShift Data Science"
	// OpenDataHub defines display name in csv.
	OpenDataHub Platform = "Open Data Hub Operator"
	// Unknown indicates that operator is not deployed using OLM
	Unknown Platform = ""
)

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
	return Unknown, nil
}

// isManagedRHODS checks if CRD add-on exists and contains string ManagedRhods.
func isManagedRHODS(cli client.Client) (Platform, error) {
	catalogSourceCRD := &apiextv1.CustomResourceDefinition{}

	err := cli.Get(context.TODO(), client.ObjectKey{Name: "catalogsources.operators.coreos.com"}, catalogSourceCRD)
	if err != nil {
		if apierrs.IsNotFound(err) {
			return "", nil
		}
		return "", err
	} else {
		expectedCatlogSource := &ofapi.CatalogSourceList{}
		err := cli.List(context.TODO(), expectedCatlogSource)
		if err != nil {
			if apierrs.IsNotFound(err) {
				return Unknown, nil
			} else {
				return Unknown, err
			}
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
