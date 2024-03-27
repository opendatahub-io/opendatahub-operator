package cluster

import (
	"context"
	"errors"
	"fmt"
	"os"

	ofapi "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// +kubebuilder:rbac:groups="config.openshift.io",resources=ingresses,verbs=get

func GetDomain(c client.Client) (string, error) {
	ingress := &unstructured.Unstructured{}
	ingress.SetGroupVersionKind(OpenshiftIngressGVK)

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

	return nil, nil
}
