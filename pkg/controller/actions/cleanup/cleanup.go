// Package cleanup provides a generic mechanism for ensuring dependency CRs are
// deleted and their finalizers processed before the managing operator is removed.
package cleanup

import (
	"context"
	"fmt"
	"strings"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	odhTypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// Target identifies a dependency CR whose finalizers must be processed before
// the operator itself is removed by GC or cascade deletion.
type Target struct {
	GVK             schema.GroupVersionKind
	Name            string
	Namespace       string // empty for cluster-scoped
	FinalizerPrefix string // only wait for finalizers with this prefix
}

// NewFinalizer returns a finalizer action that ensures dependency CRs are
// deleted and their finalizers processed during parent CR deletion.
func NewFinalizer(targets ...Target) actions.Fn {
	return func(ctx context.Context, rr *odhTypes.ReconciliationRequest) error {
		for i := range targets {
			if err := do(ctx, rr, targets[i]); err != nil {
				return err
			}
		}

		return nil
	}
}

// DeleteAndWait deletes the already-fetched dependency CR and waits for the
// operator to process its finalizers. Returns an error to trigger reconciler
// requeue while finalizers matching target.FinalizerPrefix are still pending.
//
// The caller is responsible for fetching obj and verifying it is non-nil.
func DeleteAndWait(ctx context.Context, rr *odhTypes.ReconciliationRequest, target Target, obj *unstructured.Unstructured) error {
	l := logf.FromContext(ctx)

	owned := false
	for _, ref := range obj.GetOwnerReferences() {
		if ref.UID == rr.Instance.GetUID() {
			owned = true

			break
		}
	}

	if !owned {
		l.V(1).Info("dependency CR not owned by this instance, skipping cleanup",
			"gvk", target.GVK, "name", target.Name, "instance", rr.Instance.GetName())

		return nil
	}

	if obj.GetDeletionTimestamp().IsZero() {
		l.Info("deleting dependency CR to allow operator to clean up before GC",
			"gvk", target.GVK, "name", target.Name, "instance", rr.Instance.GetName())

		if err := rr.Client.Delete(ctx, obj); err != nil && !k8serr.IsNotFound(err) {
			return err
		}
	}

	var pending []string
	for _, f := range obj.GetFinalizers() {
		if strings.HasPrefix(f, target.FinalizerPrefix) {
			pending = append(pending, f)
		}
	}

	if len(pending) == 0 {
		l.V(1).Info("dependency CR has no remaining operator finalizers, cleanup complete",
			"gvk", target.GVK, "name", target.Name)

		return nil
	}

	l.Info("waiting for dependency CR to be fully deleted",
		"gvk", target.GVK, "name", target.Name,
		"remainingFinalizers", pending)

	return fmt.Errorf("waiting for %s/%s to be fully deleted (remaining finalizers: %v)",
		target.GVK.Kind, target.Name, pending)
}

func do(ctx context.Context, rr *odhTypes.ReconciliationRequest, target Target) error {
	obj, err := getCR(ctx, rr.Client, target)
	if obj == nil || err != nil {
		return err
	}

	return DeleteAndWait(ctx, rr, target, obj)
}

func getCR(ctx context.Context, cli client.Client, target Target) (*unstructured.Unstructured, error) {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(target.GVK)

	if err := cli.Get(ctx, client.ObjectKey{Name: target.Name, Namespace: target.Namespace}, obj); err != nil {
		if k8serr.IsNotFound(err) || meta.IsNoMatchError(err) {
			return nil, nil
		}

		return nil, err
	}

	return obj, nil
}
