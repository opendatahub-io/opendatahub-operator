package serverless

import (
	"context"
	"fmt"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlLog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/gvr"
)

var log = ctrlLog.Log.WithName("features")

func EnsureServerlessAbsent(f *feature.Feature) error {
	list, err := f.DynamicClient.Resource(gvr.KnativeServing).Namespace("").List(context.TODO(), v1.ListOptions{})
	if err != nil {
		return err
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
	if err := feature.EnsureCRDIsInstalled("knativeservings.operator.knative.dev")(f); err != nil {
		log.Info("Failed to find the pre-requisite KNative Serving Operator CRD, please ensure Serverless Operator is installed.", "feature", f.Name)

		return err
	}

	return nil
}
