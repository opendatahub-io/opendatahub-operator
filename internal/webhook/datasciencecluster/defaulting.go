//go:build !nowebhook

// Package datasciencecluster provides admission webhook logic for defaulting DataScienceCluster resources.
// It ensures required fields are set to their default values when not specified by the user.
package datasciencecluster

import (
	"context"
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	modelregistryctrl "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/modelregistry"
	webhookutils "github.com/opendatahub-io/opendatahub-operator/v2/pkg/webhook"
)

//+kubebuilder:webhook:path=/mutate-datasciencecluster,mutating=true,failurePolicy=fail,sideEffects=None,groups=datasciencecluster.opendatahub.io,resources=datascienceclusters,verbs=create;update,versions=v1,name=datasciencecluster-defaulter.opendatahub.io,admissionReviewVersions=v1
//nolint:lll

// Defaulter implements webhook.CustomDefaulter for DataScienceCluster resources.
// It sets default values for fields in the DataScienceCluster CR, such as ModelRegistry.RegistriesNamespace.
type Defaulter struct {
	// Name is used for logging and webhook identification.
	Name string
}

// just assert that Defaulter implements webhook.CustomDefaulter.
var _ webhook.CustomDefaulter = &Defaulter{}

// SetupWithManager registers the defaulting webhook with the provided controller-runtime manager.
//
// Parameters:
//   - mgr: The controller-runtime manager to register the webhook with.
//
// Returns:
//   - error: Always nil (for future extensibility).
func (d *Defaulter) SetupWithManager(mgr ctrl.Manager) error {
	mutateWebhook := admission.WithCustomDefaulter(mgr.GetScheme(), &dscv1.DataScienceCluster{}, d)
	mutateWebhook.LogConstructor = webhookutils.NewWebhookLogConstructor(d.Name)
	mgr.GetWebhookServer().Register("/mutate-datasciencecluster", mutateWebhook)
	// No error to return currently, but return nil for future extensibility
	return nil
}

// Default sets default values on the provided DataScienceCluster object.
//
// Parameters:
//   - ctx: Context for the admission request (logger is extracted from here).
//   - obj: The runtime.Object to default (should be a *DataScienceCluster).
//
// Returns:
//   - error: If the object is not a DataScienceCluster, or if defaulting fails.
func (d *Defaulter) Default(ctx context.Context, obj runtime.Object) error {
	dsc, isDSC := obj.(*dscv1.DataScienceCluster)
	if !isDSC {
		log := logf.FromContext(ctx)
		err := fmt.Errorf("expected DataScienceCluster but got a different type: %T", obj)
		log.Error(err, "Got wrong type")
		return err
	}

	// Set default values
	d.applyDefaults(ctx, dsc)
	return nil
}

// applyDefaults applies default values to the DataScienceCluster resource in-place.
// Logger is extracted from the context.
//
// Parameters:
//   - ctx: Context for the admission request (logger is extracted from here).
//   - dsc: The DataScienceCluster object to mutate.
func (d *Defaulter) applyDefaults(ctx context.Context, dsc *dscv1.DataScienceCluster) {
	log := logf.FromContext(ctx)
	// If ModelRegistry is enabled and RegistriesNamespace is empty, it sets it to the default value.
	modelRegistry := &dsc.Spec.Components.ModelRegistry
	if modelRegistry.ManagementState == operatorv1.Managed {
		if modelRegistry.RegistriesNamespace == "" {
			log.V(1).Info("Setting default RegistriesNamespace for ModelRegistry", "default", modelregistryctrl.DefaultModelRegistriesNamespace)
			modelRegistry.RegistriesNamespace = modelregistryctrl.DefaultModelRegistriesNamespace
		}
	}
}
