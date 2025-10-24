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
	HardwareProfileValidatePath = "/validate-dashboard-hardwareprofile"
	HardwareProfileValidateName = "dashboard-hardwareprofile-validating"
)

//nolint:lll
//+kubebuilder:webhook:path=/validate-dashboard-hardwareprofile,mutating=false,failurePolicy=fail,sideEffects=None,groups=dashboard.opendatahub.io,resources=hardwareprofiles,verbs=create;update,versions=v1alpha1,name=dashboard-hardwareprofile-validator.opendatahub.io,admissionReviewVersions=v1

func NewHardwareProfileWebhook(s *runtime.Scheme) *deprecation.TypeValidator {
	return &deprecation.TypeValidator{
		Decoder:     admission.NewDecoder(s),
		Name:        HardwareProfileValidateName,
		WebhookPath: HardwareProfileValidatePath,
		TypeMap: deprecation.TypeMap{
			DeprecatedGVK:  gvk.DashboardHardwareProfile,
			ReplacementGVK: gvk.HardwareProfile,
		},
	}
}
func RegisterHardwareProfileWebhook(mgr ctrl.Manager) error {
	validator := NewHardwareProfileWebhook(mgr.GetScheme())

	mgr.GetWebhookServer().Register(
		validator.WebhookPath,
		&webhook.Admission{
			Handler:        validator,
			LogConstructor: webhookutils.NewWebhookLogConstructor(validator.Name),
		},
	)

	return nil
}
