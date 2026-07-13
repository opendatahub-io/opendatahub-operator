package cleanup

import (
	"context"
	"fmt"
	"strings"

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

// operatorFinalizerDomain is the domain used by the opendatahub operator for its
// finalizers. Only finalizers containing this domain will be removed during cleanup.
const operatorFinalizerDomain = "opendatahub.io"

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
		if k8serr.IsNotFound(err) {
			return nil
		}

		if meta.IsNoMatchError(err) {
			l.Info("no REST mapping for CRD, skipping finalizer cleanup this pass", "crd", crd.Name)
			return nil
		}

		return fmt.Errorf("failed to list CRs for CRD %s: %w", crd.Name, err)
	}

	for j := range crList.Items {
		cr := &crList.Items[j]
		finalizers := cr.GetFinalizers()
		if len(finalizers) == 0 {
			continue
		}

		kept, removed := filterOperatorFinalizers(finalizers)
		if len(removed) == 0 {
			continue
		}

		l.Info("removing operator-owned finalizers from CR during component cleanup",
			"crd", crd.Name,
			"name", cr.GetName(),
			"namespace", cr.GetNamespace(),
			"removedFinalizers", removed,
		)

		patch := client.MergeFrom(cr.DeepCopy())
		if len(kept) == 0 {
			cr.SetFinalizers(nil)
		} else {
			cr.SetFinalizers(kept)
		}

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

// filterOperatorFinalizers splits finalizers into those that should be kept
// (not owned by the operator) and those that should be removed (containing
// the operator's domain).
func filterOperatorFinalizers(finalizers []string) (kept, removed []string) {
	for _, f := range finalizers {
		if strings.Contains(f, operatorFinalizerDomain) {
			removed = append(removed, f)
		} else {
			kept = append(kept, f)
		}
	}

	return kept, removed
}
