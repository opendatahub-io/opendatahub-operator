//nolint:testpackage // testing unexported methods
package common

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
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
