//go:build nowebhook

package webhook

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ReconcileWebhooks is a no-op stub for builds without webhooks.
func ReconcileWebhooks(ctx context.Context, c client.Client, scheme *runtime.Scheme, owner metav1.Object) error {
	return nil
}
