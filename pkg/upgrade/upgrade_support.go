package upgrade

import (
	"context"
	"fmt"
	"reflect"

	"github.com/hashicorp/go-multierror"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

// TODO: to be removed: https://issues.redhat.com/browse/RHOAIENG-21080
var (
	NotebookSizesData = []any{
		map[string]any{
			"name": "Small",
			"resources": map[string]any{
				"requests": map[string]any{
					"cpu":    "1",
					"memory": "8Gi",
				},
				"limits": map[string]any{
					"cpu":    "2",
					"memory": "8Gi",
				},
			},
		},
		map[string]any{
			"name": "Medium",
			"resources": map[string]any{
				"requests": map[string]any{
					"cpu":    "3",
					"memory": "24Gi",
				},
				"limits": map[string]any{
					"cpu":    "6",
					"memory": "24Gi",
				},
			},
		},
		map[string]any{
			"name": "Large",
			"resources": map[string]any{
				"requests": map[string]any{
					"cpu":    "7",
					"memory": "56Gi",
				},
				"limits": map[string]any{
					"cpu":    "14",
					"memory": "56Gi",
				},
			},
		},
		map[string]any{
			"name": "X Large",
			"resources": map[string]any{
				"requests": map[string]any{
					"cpu":    "15",
					"memory": "120Gi",
				},
				"limits": map[string]any{
					"cpu":    "30",
					"memory": "120Gi",
				},
			},
		},
	}
	ModelServerSizeData = []any{
		map[string]any{
			"name": "Small",
			"resources": map[string]any{
				"requests": map[string]any{
					"cpu":    "1",
					"memory": "4Gi",
				},
				"limits": map[string]any{
					"cpu":    "2",
					"memory": "8Gi",
				},
			},
		},
		map[string]any{
			"name": "Medium",
			"resources": map[string]any{
				"requests": map[string]any{
					"cpu":    "4",
					"memory": "8Gi",
				},
				"limits": map[string]any{
					"cpu":    "8",
					"memory": "10Gi",
				},
			},
		},
		map[string]any{
			"name": "Large",
			"resources": map[string]any{
				"requests": map[string]any{
					"cpu":    "6",
					"memory": "16Gi",
				},
				"limits": map[string]any{
					"cpu":    "10",
					"memory": "20Gi",
				},
			},
		},
		map[string]any{
			"name": "Custom",
			"resources": map[string]any{
				"requests": map[string]any{},
				"limits":   map[string]any{},
			},
		},
	}
)

// TODO: to be removed: https://issues.redhat.com/browse/RHOAIENG-21080
func updateSpecFields(obj *unstructured.Unstructured, updates map[string][]any) (bool, error) {
	updated := false

	for field, newData := range updates {
		existingField, exists, err := unstructured.NestedSlice(obj.Object, "spec", field)
		if err != nil {
			return false, fmt.Errorf("failed to get field '%s': %w", field, err)
		}

		if !exists || len(existingField) == 0 {
			if err := unstructured.SetNestedSlice(obj.Object, newData, "spec", field); err != nil {
				return false, fmt.Errorf("failed to set field '%s': %w", field, err)
			}
			updated = true
		}
	}

	return updated, nil
}

func deleteDeprecatedResources(ctx context.Context, cli client.Client, namespace string, resourceList []string, resourceType client.ObjectList) error {
	log := logf.FromContext(ctx)
	var multiErr *multierror.Error
	listOpts := &client.ListOptions{Namespace: namespace}
	if err := cli.List(ctx, resourceType, listOpts); err != nil {
		multiErr = multierror.Append(multiErr, err)
	}
	items := reflect.ValueOf(resourceType).Elem().FieldByName("Items")
	for i := range items.Len() {
		item := items.Index(i).Addr().Interface().(client.Object) //nolint:errcheck,forcetypeassert
		for _, name := range resourceList {
			if name == item.GetName() {
				log.Info("Attempting to delete " + item.GetName() + " in namespace " + namespace)
				err := cli.Delete(ctx, item)
				if err != nil {
					if k8serr.IsNotFound(err) {
						log.Info("Could not find " + item.GetName() + " in namespace " + namespace)
					} else {
						multiErr = multierror.Append(multiErr, err)
					}
				}
				log.Info("Successfully deleted " + item.GetName())
			}
		}
	}
	return multiErr.ErrorOrNil()
}

// upgradODCCR handles different cases related to upgrading ODC CRs.
func upgradeODCCR(ctx context.Context, cli client.Client, instanceName string, applicationNS string) error {
	crd := &apiextv1.CustomResourceDefinition{}
	if err := cli.Get(ctx, client.ObjectKey{Name: "odhdashboardconfigs.opendatahub.io"}, crd); err != nil {
		return client.IgnoreNotFound(err)
	}
	odhObject := &unstructured.Unstructured{}
	odhObject.SetGroupVersionKind(gvk.OdhDashboardConfig)
	if err := cli.Get(ctx, client.ObjectKey{
		Namespace: applicationNS,
		Name:      instanceName,
	}, odhObject); err != nil {
		return client.IgnoreNotFound(err)
	}

	// 1. unset ownerreference for CR odh-dashboard-config
	err := unsetOwnerReference(ctx, cli, instanceName, odhObject)
	return err
}

func unsetOwnerReference(ctx context.Context, cli client.Client, instanceName string, odhObject *unstructured.Unstructured) error {
	if odhObject.GetOwnerReferences() != nil {
		// set to nil as updates
		odhObject.SetOwnerReferences(nil)
		if err := cli.Update(ctx, odhObject); err != nil {
			return fmt.Errorf("error unset ownerreference for CR %s : %w", instanceName, err)
		}
	}
	return nil
}
