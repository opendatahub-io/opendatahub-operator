//go:build nowebhook

package webhook

import ctrl "sigs.k8s.io/controller-runtime"

// RegisterWebhooks is a no-op stub for builds without webhooks.
func RegisterAllWebhooks(mgr ctrl.Manager) error {
	return nil
}
