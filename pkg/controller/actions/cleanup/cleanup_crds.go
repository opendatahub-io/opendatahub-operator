package cleanup

import (
	"context"
	"fmt"

	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	odhTypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// NewCRDInstanceCleanupFinalizer returns a finalizer action that removes
// finalizers from all CR instances of CRDs labeled for this component.
// When a component is removed, its operator is shut down and can no longer
// process finalizers on CRs. Stripping finalizers ensures CRDs can be
// cleanly deleted without getting stuck in Terminating state.
func NewCRDInstanceCleanupFinalizer(labelKey, labelValue string) actions.Fn {
	return func(ctx context.Context, rr *odhTypes.ReconciliationRequest) error {
		crdList := &extv1.CustomResourceDefinitionList{}
		if err := rr.Client.List(ctx, crdList,
			client.MatchingLabels{labelKey: labelValue},
		); err != nil {
			return fmt.Errorf("failed to list CRDs for component cleanup: %w", err)
		}

		if len(crdList.Items) == 0 {
			return nil
		}

		for i := range crdList.Items {
			crd := &crdList.Items[i]

			var storageVersion string
			for _, v := range crd.Spec.Versions {
				if v.Storage {
					storageVersion = v.Name
					break
				}
			}

			if storageVersion == "" {
				continue
			}

			if err := removeCRFinalizers(ctx, rr.Client, crd, storageVersion); err != nil {
				return err
			}
		}

		return nil
	}
}

func removeCRFinalizers(
	ctx context.Context,
	cli client.Client,
	crd *extv1.CustomResourceDefinition,
	storageVersion string,
) error {
	l := logf.FromContext(ctx)
	crList := &unstructured.UnstructuredList{}
	crList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   crd.Spec.Group,
		Version: storageVersion,
		Kind:    crd.Spec.Names.Kind,
	})

	if err := cli.List(ctx, crList); err != nil {
		if meta.IsNoMatchError(err) || k8serr.IsNotFound(err) {
			return nil
		}

		return fmt.Errorf("failed to list CRs for CRD %s: %w", crd.Name, err)
	}

	for j := range crList.Items {
		cr := &crList.Items[j]
		if len(cr.GetFinalizers()) == 0 {
			continue
		}

		l.Info("removing finalizers from CR during component cleanup",
			"crd", crd.Name,
			"name", cr.GetName(),
			"namespace", cr.GetNamespace(),
		)

		patch := client.MergeFrom(cr.DeepCopy())
		cr.SetFinalizers(nil)

		if err := cli.Patch(ctx, cr, patch); err != nil {
			if k8serr.IsNotFound(err) {
				continue
			}

			return fmt.Errorf("failed to remove finalizers from %s %s/%s: %w",
				cr.GetKind(), cr.GetNamespace(), cr.GetName(), err)
		}
	}

	return nil
}
