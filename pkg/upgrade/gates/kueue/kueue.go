package kueue

import (
	"context"
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

const (
	kueueCRName                   = "cluster"
	kueueOperatorSubscriptionName = "kueue-operator"
)

var workloadFamilies = []struct {
	name  string
	kinds []schema.GroupVersionKind
}{
	{name: "Notebooks", kinds: []schema.GroupVersionKind{gvk.Notebook}},
	{name: "InferenceServices", kinds: []schema.GroupVersionKind{gvk.InferenceServices}},
	{name: "LLMInferenceServices", kinds: []schema.GroupVersionKind{gvk.LLMInferenceServiceV1Alpha2, gvk.LLMInferenceServiceV1Alpha1}},
	{name: "RayClusters", kinds: []schema.GroupVersionKind{gvk.RayClusterV1, gvk.RayClusterV1Alpha1}},
	{name: "RayJobs", kinds: []schema.GroupVersionKind{gvk.RayJobV1, gvk.RayJobV1Alpha1}},
	{name: "PyTorchJobs", kinds: []schema.GroupVersionKind{gvk.PyTorchJob}},
}

func Check(ctx context.Context, reader client.Reader, _, _ string) error {
	state, err := getKueueManagementState(ctx, reader)
	if err != nil {
		return err
	}

	blocking := &UpgradeBlockedError{}

	switch state {
	case "":
		return nil
	case string(operatorv1.Managed):
		blocking.ManagedStateUnsupported = true
		return blocking
	case string(operatorv1.Unmanaged):
		installed, err := hasKueueOperatorSubscription(ctx, reader)
		if err != nil {
			return err
		}
		blocking.MissingKueueOperatorSubscription = !installed
	default:
		return nil
	}

	blocking.WorkloadsWithoutKueueNamespaceLabel, err = collectWorkloadsWithoutKueueNamespaceLabel(ctx, reader)
	if err != nil {
		return err
	}
	if !blocking.MissingKueueOperatorSubscription &&
		blocking.WorkloadsWithoutKueueNamespaceLabel == 0 {
		return nil
	}

	return blocking
}

func getKueueManagementState(ctx context.Context, reader client.Reader) (string, error) {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk.KueueConfigV1)

	err := reader.Get(ctx, client.ObjectKey{Name: kueueCRName}, obj)
	switch {
	case k8serr.IsNotFound(err), meta.IsNoMatchError(err):
		return "", nil
	case err != nil:
		return "", fmt.Errorf("getting Kueue CR: %w", err)
	}

	state, found, err := unstructured.NestedString(obj.Object, "spec", "managementState")
	if err != nil {
		return "", fmt.Errorf("reading Kueue managementState: %w", err)
	}
	if !found {
		return "", nil
	}

	return state, nil
}

func hasKueueOperatorSubscription(ctx context.Context, reader client.Reader) (bool, error) {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(gvk.Subscription)

	err := reader.List(ctx, list)
	switch {
	case meta.IsNoMatchError(err):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("listing Subscriptions: %w", err)
	}

	for i := range list.Items {
		if list.Items[i].GetName() == kueueOperatorSubscriptionName {
			return true, nil
		}
	}

	return false, nil
}

func collectWorkloadsWithoutKueueNamespaceLabel(ctx context.Context, reader client.Reader) (int, error) {
	namespaceManaged := make(map[string]bool)
	blocking := 0

	for _, family := range workloadFamilies {
		list, err := listFirstSupportedFamily(ctx, reader, family.name, family.kinds)
		if err != nil {
			return 0, err
		}

		for i := range list.Items {
			if !resources.HasLabel(&list.Items[i], cluster.KueueQueueNameLabel) {
				continue
			}

			ns := list.Items[i].GetNamespace()
			managed, ok := namespaceManaged[ns]
			if !ok {
				managed, err = isNamespaceManagedByKueue(ctx, reader, ns)
				if err != nil {
					return 0, err
				}
				namespaceManaged[ns] = managed
			}

			if !managed {
				blocking++
			}
		}
	}

	return blocking, nil
}

func listFirstSupportedFamily(
	ctx context.Context,
	reader client.Reader,
	name string,
	kinds []schema.GroupVersionKind,
) (*unstructured.UnstructuredList, error) {
	for _, kind := range kinds {
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(kind)

		err := reader.List(ctx, list)
		switch {
		case meta.IsNoMatchError(err):
			continue
		case err != nil:
			return nil, fmt.Errorf("listing %s: %w", name, err)
		default:
			return list, nil
		}
	}

	return &unstructured.UnstructuredList{}, nil
}

func isNamespaceManagedByKueue(ctx context.Context, reader client.Reader, namespaceName string) (bool, error) {
	if namespaceName == "" {
		return false, nil
	}

	ns := &corev1.Namespace{}
	err := reader.Get(ctx, client.ObjectKey{Name: namespaceName}, ns)
	switch {
	case k8serr.IsNotFound(err):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("getting namespace %s: %w", namespaceName, err)
	default:
		return resources.HasLabel(ns, cluster.KueueManagedLabelKey, "true"), nil
	}
}
