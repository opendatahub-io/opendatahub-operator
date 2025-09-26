//go:build !nowebhook

package serving

import (
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	webhookutils "github.com/opendatahub-io/opendatahub-operator/v2/pkg/webhook"
)

// create new type for connection types.
type ConnectionType string

const (
	// ConnectionTypeURI represents uri connections.
	ConnectionTypeURI ConnectionType = "uri-v1"
	// ConnectionTypeS3 represents s3 connections.
	ConnectionTypeS3 ConnectionType = "s3"
	// ConnectionTypeOCI represents oci connections.
	ConnectionTypeOCI ConnectionType = "oci-v1"
)

func (ct ConnectionType) String() string {
	return string(ct)
}

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
