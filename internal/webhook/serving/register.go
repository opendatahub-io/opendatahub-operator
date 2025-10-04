//go:build !nowebhook

package serving

import (
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	webhookutils "github.com/opendatahub-io/opendatahub-operator/v2/pkg/webhook"
)

// RegisterWebhooks registers the combined connection webhook that handles both validation and mutation.
func RegisterWebhooks(mgr ctrl.Manager) error {
	if err := (&ISVCConnectionWebhook{
		Webhook: webhookutils.BaseServingConnectionWebhook{
			APIReader: mgr.GetAPIReader(),
			Client:    mgr.GetClient(),
			Decoder:   admission.NewDecoder(mgr.GetScheme()),
			Name:      "connection-isvc",
		},
	}).SetupWithManager(mgr); err != nil {
		return err
	}
	if err := (&LLMISVCConnectionWebhook{
		Webhook: webhookutils.BaseServingConnectionWebhook{
			APIReader: mgr.GetAPIReader(),
			Client:    mgr.GetClient(),
			Decoder:   admission.NewDecoder(mgr.GetScheme()),
			Name:      "connection-llmisvc",
		},
	}).SetupWithManager(mgr); err != nil {
		return err
	}

	return nil
}
