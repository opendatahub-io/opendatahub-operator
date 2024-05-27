package serverless

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
)

const (
	KnativeServingNamespace = "knative-serving"
)

func EnsureServerlessAbsent(f *feature.Feature) error {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(gvk.KnativeServing)

	if err := f.Client.List(context.TODO(), list, client.InNamespace("")); err != nil {
		return fmt.Errorf("failed to list KnativeServings: %w", err)
	}

	if len(list.Items) == 0 {
		return nil
	}

	if len(list.Items) > 1 {
		return fmt.Errorf("multiple KNativeServing resources found, which is an unsupported state")
	}

	servingOwners := list.Items[0].GetOwnerReferences()
	featureOwner := f.AsOwnerReference()
	for _, owner := range servingOwners {
		if owner.APIVersion == featureOwner.APIVersion &&
			owner.Kind == featureOwner.Kind &&
			owner.Name == featureOwner.Name &&
			owner.UID == featureOwner.UID {
			return nil
		}
	}

	return fmt.Errorf("existing KNativeServing resource was found; integrating to an existing installation is not supported")
}

func EnsureServerlessOperatorInstalled(f *feature.Feature) error {
	if err := feature.EnsureOperatorIsInstalled("serverless-operator")(f); err != nil {
		return fmt.Errorf("failed to find the pre-requisite KNative Serving Operator subscription, please ensure Serverless Operator is installed. %w", err)
	}

	return nil
}

var EnsureServerlessServingDeployed = feature.WaitForResourceToBeCreated(KnativeServingNamespace, gvk.KnativeServing)
