//go:build !nowebhook

// Package v3 provides admission webhook logic for defaulting DataScienceCluster v3 resources.
// It ensures required fields are set to their default values when not specified by the user.
package v3

import (
	"context"
	"encoding/json"
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv3 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	webhookutils "github.com/opendatahub-io/opendatahub-operator/v2/pkg/webhook"
)

//+kubebuilder:webhook:path=/mutate-datasciencecluster-v3,matchPolicy=Exact,mutating=true,failurePolicy=fail,sideEffects=None,groups=datasciencecluster.opendatahub.io,resources=datascienceclusters,verbs=create;update,versions=v3,name=datasciencecluster-v3-defaulter.opendatahub.io,admissionReviewVersions=v1
//nolint:lll

// Defaulter implements admission.Defaulter for DataScienceCluster v3 resources.
// It sets default values for fields in the DataScienceCluster CR, such as AIHub.ApplicationNamespace.
type Defaulter struct {
	// Name is used for logging and webhook identification.
	Name string
}

// just assert that Defaulter implements admission.Defaulter.
var _ admission.Defaulter[runtime.Object] = &Defaulter{}

// SetupWithManager registers the defaulting webhook with the provided controller-runtime manager.
//
// Parameters:
//   - mgr: The controller-runtime manager to register the webhook with.
//
// Returns:
//   - error: Always nil (for future extensibility).
func (d *Defaulter) SetupWithManager(mgr ctrl.Manager) error {
	mutateWebhook := admission.WithCustomDefaulter(mgr.GetScheme(), &dscv3.DataScienceCluster{}, d)
	mutateWebhook.LogConstructor = webhookutils.NewWebhookLogConstructor(d.Name)
	mgr.GetWebhookServer().Register("/mutate-datasciencecluster-v3", mutateWebhook)
	// No error to return currently, but return nil for future extensibility
	return nil
}

// Default sets default values on the provided DataScienceCluster v3 object.
//
// Parameters:
//   - ctx: Context for the admission request (logger is extracted from here).
//   - obj: The runtime.Object to default (should be a *DataScienceCluster).
//
// Returns:
//   - error: If the object is not a DataScienceCluster, or if defaulting fails.
func (d *Defaulter) Default(ctx context.Context, obj runtime.Object) error {
	dsc, isDSC := obj.(*dscv3.DataScienceCluster)
	if !isDSC {
		log := logf.FromContext(ctx)
		err := fmt.Errorf("expected DataScienceCluster v3 but got a different type: %T", obj)
		log.Error(err, "Got wrong type")
		return err
	}

	// Set default values
	d.applyDefaults(ctx, dsc)

	request, err := admission.RequestFromContext(ctx)
	if err != nil {
		// Direct defaulting calls have no old object and cannot introduce history.
		return dsc.AdmitMaaSV2State(nil)
	}

	var old *dscv3.DataScienceCluster
	if request.Operation == admissionv1.Update {
		old = &dscv3.DataScienceCluster{}
		if err := json.Unmarshal(request.OldObject.Raw, old); err != nil {
			return fmt.Errorf("decode old DSC for MaaS compatibility: %w", err)
		}
	}

	if request.RequestKind != nil && request.RequestKind.Version == "v2" {
		if err := dsc.AdmitMaaSV2StateFromV2(); err != nil {
			return fmt.Errorf("admit DSC MaaS compatibility: %w", err)
		}

		return nil
	}

	if err := dsc.AdmitMaaSV2State(old); err != nil {
		return fmt.Errorf("admit DSC MaaS compatibility: %w", err)
	}

	return nil
}

// applyDefaults applies default values to the DataScienceCluster v3 resource in-place.
// Logger is extracted from the context.
//
// Parameters:
//   - ctx: Context for the admission request (logger is extracted from here).
//   - dsc: The DataScienceCluster object to mutate.
func (d *Defaulter) applyDefaults(ctx context.Context, dsc *dscv3.DataScienceCluster) {
	log := logf.FromContext(ctx)
	// If AI Hub is enabled and ApplicationNamespace is empty, set it to the default value.
	aiHub := &dsc.Spec.Components.AIHub
	if aiHub.ManagementState == operatorv1.Managed {
		if aiHub.ApplicationNamespace == "" {
			log.V(1).Info("Setting default ApplicationNamespace for AI Hub", "default", componentApi.DefaultModelRegistriesNamespace)
			aiHub.ApplicationNamespace = componentApi.DefaultModelRegistriesNamespace
		}
	}

	// If Kserve is enabled and NIM ManagementState is empty, set it to the default value (Managed).
	kserve := &dsc.Spec.Components.Kserve
	if kserve.ManagementState == operatorv1.Managed {
		if kserve.NIM.ManagementState == "" {
			log.V(1).Info("Setting default ManagementState for Kserve NIM", "default", operatorv1.Managed)
			kserve.NIM.ManagementState = operatorv1.Managed
		}
	}
}
