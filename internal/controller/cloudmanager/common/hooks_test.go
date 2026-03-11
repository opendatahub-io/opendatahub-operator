//nolint:testpackage // testing unexported methods
package common

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
)

func TestCreateNamespaceHook(t *testing.T) {
	t.Run("creates namespace", func(t *testing.T) {
		cli, err := fakeclient.New()
		require.NoError(t, err)

		rr := &odhtypes.ReconciliationRequest{
			Client: cli,
		}

		hook := CreateNamespaceHook("test-namespace")
		err = hook(context.Background(), rr)
		require.NoError(t, err)

		var ns corev1.Namespace
		err = cli.Get(context.Background(), types.NamespacedName{Name: "test-namespace"}, &ns)
		require.NoError(t, err)
		assert.Equal(t, "test-namespace", ns.Name)
	})

	t.Run("is idempotent when namespace already exists", func(t *testing.T) {
		ns := &corev1.Namespace{}
		ns.Name = "existing-namespace"

		cli, err := fakeclient.New(fakeclient.WithObjects(ns))
		require.NoError(t, err)

		rr := &odhtypes.ReconciliationRequest{
			Client: cli,
		}

		hook := CreateNamespaceHook("existing-namespace")
		err = hook(context.Background(), rr)
		require.NoError(t, err)

		var result corev1.Namespace
		err = cli.Get(context.Background(), types.NamespacedName{Name: "existing-namespace"}, &result)
		require.NoError(t, err)
		assert.Equal(t, "existing-namespace", result.Name)
	})
}

// TODO(OSSM-12397): Remove this test once the sail-operator ships a fix.
func TestAnnotateIstioWebhooksHook(t *testing.T) {
	t.Run("should annotate webhooks when they exist without annotation", func(t *testing.T) {
		mutatingWH := &admissionregistrationv1.MutatingWebhookConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Name: istioSidecarInjectorWebhook,
			},
		}
		validatingWH := &admissionregistrationv1.ValidatingWebhookConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Name: istioValidatorWebhook,
			},
		}

		cli, err := fakeclient.New(fakeclient.WithObjects(mutatingWH, validatingWH))
		require.NoError(t, err)

		rr := &odhtypes.ReconciliationRequest{Client: cli}

		hook := AnnotateIstioWebhooksHook()
		err = hook(context.Background(), rr)
		require.NoError(t, err)

		var updatedMutating admissionregistrationv1.MutatingWebhookConfiguration
		err = cli.Get(context.Background(), types.NamespacedName{Name: istioSidecarInjectorWebhook}, &updatedMutating)
		require.NoError(t, err)
		assert.Equal(t, "true", updatedMutating.Annotations[sailOperatorIgnoreAnnotation])

		var updatedValidating admissionregistrationv1.ValidatingWebhookConfiguration
		err = cli.Get(context.Background(), types.NamespacedName{Name: istioValidatorWebhook}, &updatedValidating)
		require.NoError(t, err)
		assert.Equal(t, "true", updatedValidating.Annotations[sailOperatorIgnoreAnnotation])
	})

	t.Run("should be a no-op when webhooks already have the annotation", func(t *testing.T) {
		mutatingWH := &admissionregistrationv1.MutatingWebhookConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Name: istioSidecarInjectorWebhook,
				Annotations: map[string]string{
					sailOperatorIgnoreAnnotation: "true",
				},
			},
		}
		validatingWH := &admissionregistrationv1.ValidatingWebhookConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Name: istioValidatorWebhook,
				Annotations: map[string]string{
					sailOperatorIgnoreAnnotation: "true",
				},
			},
		}

		cli, err := fakeclient.New(fakeclient.WithObjects(mutatingWH, validatingWH))
		require.NoError(t, err)

		rr := &odhtypes.ReconciliationRequest{Client: cli}

		hook := AnnotateIstioWebhooksHook()
		err = hook(context.Background(), rr)
		require.NoError(t, err)

		var updatedMutating admissionregistrationv1.MutatingWebhookConfiguration
		err = cli.Get(context.Background(), types.NamespacedName{Name: istioSidecarInjectorWebhook}, &updatedMutating)
		require.NoError(t, err)
		assert.Equal(t, "true", updatedMutating.Annotations[sailOperatorIgnoreAnnotation])
	})

	t.Run("should be a no-op when webhooks do not exist", func(t *testing.T) {
		cli, err := fakeclient.New()
		require.NoError(t, err)

		rr := &odhtypes.ReconciliationRequest{Client: cli}

		hook := AnnotateIstioWebhooksHook()
		err = hook(context.Background(), rr)
		require.NoError(t, err)
	})

	t.Run("should preserve existing annotations when adding the ignore annotation", func(t *testing.T) {
		mutatingWH := &admissionregistrationv1.MutatingWebhookConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Name: istioSidecarInjectorWebhook,
				Annotations: map[string]string{
					"existing-annotation": "existing-value",
				},
			},
		}

		cli, err := fakeclient.New(fakeclient.WithObjects(mutatingWH))
		require.NoError(t, err)

		rr := &odhtypes.ReconciliationRequest{Client: cli}

		hook := AnnotateIstioWebhooksHook()
		err = hook(context.Background(), rr)
		require.NoError(t, err)

		var updatedMutating admissionregistrationv1.MutatingWebhookConfiguration
		err = cli.Get(context.Background(), types.NamespacedName{Name: istioSidecarInjectorWebhook}, &updatedMutating)
		require.NoError(t, err)
		assert.Equal(t, "true", updatedMutating.Annotations[sailOperatorIgnoreAnnotation])
		assert.Equal(t, "existing-value", updatedMutating.Annotations["existing-annotation"])
	})
}
