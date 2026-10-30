package precondition

import (
	"context"
	"errors"
	"fmt"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// CRDConversionPolicy determines when a CRD must use Webhook conversion.
type CRDConversionPolicy int

const (
	// RequireWebhookAlways requires Webhook conversion regardless of the number
	// of served versions, as required by DataScienceCluster.
	RequireWebhookAlways CRDConversionPolicy = iota + 1
	// RequireWebhookForMultipleServedVersions permits CRDs with at most one
	// served version to reconcile without conversion, as with older Platforms.
	RequireWebhookForMultipleServedVersions
)

// CRDConversion checks the live CRD's conversion strategy before reconciliation.
// It always uses BeforeReconciliation so an unsafe strategy or a failed read
// prevents finalizer, action, and status writes. The check uses the controller's
// APIReader to bypass informer-cache staleness; the subsequent writes are not
// atomic with this read.
func CRDConversion(crdName string, policy CRDConversionPolicy) PreCondition {
	return newPreCondition(func(ctx context.Context, rr *types.ReconciliationRequest) (CheckResult, error) {
		if crdName == "" {
			return CheckResult{}, errors.New("CRD conversion precondition requires a CRD name")
		}
		if policy != RequireWebhookAlways && policy != RequireWebhookForMultipleServedVersions {
			return CheckResult{}, fmt.Errorf("CRD %q has invalid conversion policy %d", crdName, policy)
		}
		if rr == nil || rr.Controller == nil {
			return CheckResult{}, fmt.Errorf("CRD %q conversion check requires a controller", crdName)
		}
		reader := rr.Controller.GetAPIReader()
		if reader == nil {
			return CheckResult{}, fmt.Errorf("CRD %q conversion check requires an APIReader", crdName)
		}

		crd := apiextensionsv1.CustomResourceDefinition{}
		if err := reader.Get(ctx, client.ObjectKey{Name: crdName}, &crd); err != nil {
			return CheckResult{}, fmt.Errorf("failed to read CRD %q conversion strategy: %w", crdName, err)
		}

		servedVersions := 0
		for _, version := range crd.Spec.Versions {
			if version.Served {
				servedVersions++
			}
		}
		if policy == RequireWebhookForMultipleServedVersions && servedVersions <= 1 {
			return CheckResult{Pass: true}, nil
		}
		if crd.Spec.Conversion != nil && crd.Spec.Conversion.Strategy == apiextensionsv1.WebhookConverter {
			return CheckResult{Pass: true}, nil
		}

		strategy := "absent"
		if crd.Spec.Conversion != nil {
			strategy = string(crd.Spec.Conversion.Strategy)
		}
		return CheckResult{
			Message: fmt.Sprintf("CRD %q requires Webhook conversion (current strategy %q, served versions %d)", crdName, strategy, servedVersions),
		}, nil
	}, BeforeReconciliation())
}
