//go:build !nowebhook

package dashboard

import (
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/deprecation"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	webhookutils "github.com/opendatahub-io/opendatahub-operator/v2/pkg/webhook"
)

const (
	AcceleratorProfileValidatePath = "/validate-dashboard-acceleratorprofile"
	AcceleratorProfileValidateName = "dashboard-acceleratorprofile-validating"
)

//nolint:lll
//+kubebuilder:webhook:path=/validate-dashboard-acceleratorprofile,mutating=false,failurePolicy=fail,sideEffects=None,groups=dashboard.opendatahub.io,resources=acceleratorprofiles,verbs=create;update,versions=v1,name=dashboard-acceleratorprofile-validator.opendatahub.io,admissionReviewVersions=v1

func NewAcceleratorProfileWebhook(s *runtime.Scheme) *deprecation.TypeValidator {
	return &deprecation.TypeValidator{
		Decoder:     admission.NewDecoder(s),
		Name:        AcceleratorProfileValidateName,
		WebhookPath: AcceleratorProfileValidatePath,
		TypeMap: deprecation.TypeMap{
			DeprecatedGVK:  gvk.DashboardAcceleratorProfile,
			ReplacementGVK: gvk.HardwareProfile,
		},
	}
}

func RegisterAcceleratorProfileWebhook(mgr ctrl.Manager) error {
	validator := NewAcceleratorProfileWebhook(mgr.GetScheme())

	mgr.GetWebhookServer().Register(
		validator.WebhookPath,
		&webhook.Admission{
			Handler:        validator,
			LogConstructor: webhookutils.NewWebhookLogConstructor(validator.Name),
		},
	)

	return nil
}
