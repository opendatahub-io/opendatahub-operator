package common

import (
	"context"
	"fmt"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

const (
	sailOperatorIgnoreAnnotation = "sailoperator.io/ignore"

	istioSidecarInjectorWebhook = "istio-sidecar-injector"
	istioValidatorWebhook       = "istio-validator-istio-system"

	// ServiceMonitorCRDName is the fully qualified name of the ServiceMonitor CRD.
	ServiceMonitorCRDName = "servicemonitors.monitoring.coreos.com"
)

// AnnotateIstioWebhooksHook returns a PostApply hook that annotates Istio
// webhooks with sailoperator.io/ignore=true to work around a sail-operator
// bug (OSSM-12397) where webhook configuration updates trigger an infinite
// Helm reinstall loop on vanilla Kubernetes.
//
// TODO(OSSM-12397): Remove this workaround once the sail-operator ships a fix.
// Tracking: https://issues.redhat.com/browse/RHOAIENG-52246
func AnnotateIstioWebhooksHook() types.HookFn {
	return func(ctx context.Context, rr *types.ReconciliationRequest) error {
		logger := logf.FromContext(ctx)

		hookErr := ensureSailOperatorIgnoreAnnotation(
			ctx, rr.Client, istioSidecarInjectorWebhook, &admissionregistrationv1.MutatingWebhookConfiguration{},
		)
		if hookErr != nil {
			logger.Error(hookErr, "Failed to annotate webhook", "name", istioSidecarInjectorWebhook)
		}

		if err := ensureSailOperatorIgnoreAnnotation(
			ctx, rr.Client, istioValidatorWebhook, &admissionregistrationv1.ValidatingWebhookConfiguration{},
		); err != nil {
			logger.Error(err, "Failed to annotate webhook", "name", istioValidatorWebhook)
			if hookErr == nil {
				hookErr = err
			}
		}

		return hookErr
	}
}

// SkipCRDIfPresent returns a PreApply hook that removes a CRD from the
// rendered resources if it already exists in the cluster and is not managed
// by this controller. This avoids SSA ForceOwnership conflicts with other
// operators that may own the CRD (e.g., Prometheus Operator for ServiceMonitor).
// If the CRD carries the infrastructure label it is kept in resources so it can
// be updated via SSA. On clusters without the CRD, it is kept in resources and
// deployed normally.
func SkipCRDIfPresent(crdName string) types.HookFn {
	return func(ctx context.Context, rr *types.ReconciliationRequest) error {
		crd, err := cluster.GetCRD(ctx, rr.Client, crdName)
		if err != nil {
			if k8serr.IsNotFound(err) {
				return nil
			}

			return fmt.Errorf("failed to check CRD %s: %w", crdName, err)
		}

		// If the CRD is managed by this controller, keep it in resources
		// so it gets updated via SSA.
		if _, ok := crd.GetLabels()[labels.InfrastructurePartOf]; ok {
			return nil
		}

		logger := logf.FromContext(ctx)
		logger.Info("CRD already exists, skipping installation", "crd", crdName)

		return rr.RemoveResources(func(obj *unstructured.Unstructured) bool {
			return obj.GetKind() == gvk.CustomResourceDefinition.Kind &&
				obj.GetName() == crdName
		})
	}
}

func ensureSailOperatorIgnoreAnnotation(ctx context.Context, c client.Client, name string, obj client.Object) error {
	logger := logf.FromContext(ctx)

	if err := c.Get(ctx, k8stypes.NamespacedName{Name: name}, obj); err != nil {
		if k8serr.IsNotFound(err) {
			return nil
		}
		return err
	}

	annotations := obj.GetAnnotations()
	if annotations[sailOperatorIgnoreAnnotation] == "true" {
		return nil
	}

	// Use a MergePatch to only update the annotation without taking ownership
	// of other fields on this resource we do not own.
	annotationPatch := client.RawPatch(k8stypes.MergePatchType,
		[]byte(`{"metadata":{"annotations":{"`+sailOperatorIgnoreAnnotation+`":"true"}}}`))

	if err := c.Patch(ctx, obj, annotationPatch); err != nil {
		return err
	}

	logger.Info("Annotated webhook with sailoperator.io/ignore=true",
		"kind", obj.GetObjectKind().GroupVersionKind().Kind,
		"name", name,
	)

	return nil
}
