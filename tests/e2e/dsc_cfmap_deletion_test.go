package e2e_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade"
)

func cfgMapDeletionTestSuite(t *testing.T) {
	testCtx, err := NewTestContext()
	require.NoError(t, err)

	defer removeDeletionConfigMap(testCtx)

	t.Run(testCtx.testDsc.Name, func(t *testing.T) {
		t.Run("create configmap but set to disable deletion", func(t *testing.T) {
			err = testCtx.testDSCDeletionUsingConfigMap("false")
			require.NoError(t, err, "Configmap should not delete DSC instance")
		})

		t.Run("owned namespaces should be not deleted", func(t *testing.T) {
			err = testCtx.testOwnedNamespacesAllExist()
			require.NoError(t, err, "Error while deleting owned namespaces")
		})
	})
}

func (tc *testContext) testDSCDeletionUsingConfigMap(enableDeletion string) error {
	dscLookupKey := types.NamespacedName{Name: tc.testDsc.Name}
	expectedDSC := &dscv1.DataScienceCluster{}

	if err := createDeletionConfigMap(tc, enableDeletion); err != nil {
		return err
	}

	err := tc.customClient.Get(tc.ctx, dscLookupKey, expectedDSC)
	// should have DSC instance
	if err != nil {
		if k8serr.IsNotFound(err) {
			return fmt.Errorf("should have DSC instance in cluster:%w", err)
		}
		return fmt.Errorf("error getting DSC instance :%w", err)
	}

	return nil
}
func (tc *testContext) testOwnedNamespacesAllExist() error {
	namespaces, err := tc.kubeClient.CoreV1().Namespaces().List(tc.ctx, metav1.ListOptions{
		LabelSelector: labels.ODH.OwnedNamespace,
	})

	if err != nil {
		return fmt.Errorf("failed getting owned namespaces %w", err)
	}
	if len(namespaces.Items) == 0 {
		return errors.New("all namespaces are gone")
	}

	return nil
}

func removeDeletionConfigMap(tc *testContext) {
	_ = tc.kubeClient.CoreV1().ConfigMaps(tc.operatorNamespace).Delete(context.TODO(), "delete-self-managed", metav1.DeleteOptions{})
}

func createDeletionConfigMap(tc *testContext, enableDeletion string) error {
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "delete-self-managed",
			Namespace: tc.operatorNamespace,
			Labels: map[string]string{
				upgrade.DeleteConfigMapLabel: enableDeletion,
			},
		},
	}

	configMaps := tc.kubeClient.CoreV1().ConfigMaps(configMap.Namespace)
	if _, err := configMaps.Get(context.TODO(), configMap.Name, metav1.GetOptions{}); err != nil {
		switch {
		case k8serr.IsNotFound(err):
			if _, err = configMaps.Create(context.TODO(), configMap, metav1.CreateOptions{}); err != nil {
				return err
			}
		case k8serr.IsAlreadyExists(err):
			if _, err = configMaps.Update(context.TODO(), configMap, metav1.UpdateOptions{}); err != nil {
				return err
			}
		default:
			return err
		}
	}

	return nil
}
