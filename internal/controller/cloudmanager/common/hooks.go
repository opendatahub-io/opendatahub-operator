package common

import (
	"context"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

const (
	sailOperatorIgnoreAnnotation = "sailoperator.io/ignore"

	istioSidecarInjectorWebhook = "istio-sidecar-injector"
	istioValidatorWebhook       = "istio-validator-istio-system"
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
