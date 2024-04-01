package e2e_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsc "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	dsci "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade"
)

func cfgMapDeletionTestSuite(t *testing.T) {
	testCtx, err := NewTestContext()
	require.NoError(t, err)

	defer removeDeletionConfigMap(testCtx)

	t.Run(testCtx.testDsc.Name, func(t *testing.T) {
		t.Run("create data science cluster", func(t *testing.T) {
			err = testCtx.testDSCCreation()
			require.NoError(t, err, "Error to create DSC instance")
		})

		t.Run("ensure all components created", func(t *testing.T) {
			err = testCtx.testAllApplicationCreation(t)
			require.NoError(t, err, "Error to create DSC instance")
		})

		t.Run("trigger deletion using labeled config map", func(t *testing.T) {
			err = testCtx.testDSCDeletionUsingConfigMap()
			require.NoError(t, err, "Error to delete DSC instance")
		})

		t.Run("owned namespaces should be deleted", func(t *testing.T) {
			err = testCtx.testOwnedNamespacesDeletion()
			require.NoError(t, err, "Error while deleting owned namespaces")
		})

		t.Run("dsci should be deleted", func(t *testing.T) {
			err = testCtx.testDSCIDeletion()
			require.NoError(t, err, "failed deleting DSCI")
		})

		t.Run("applications resources should be deleted", func(t *testing.T) {
			err = testCtx.testAllApplicationDeletion()
			require.NoError(t, err, "Error to delete component")
		})
	})
}

func (tc *testContext) testDSCIDeletion() error {
	dsciInstances := &dsci.DSCInitializationList{}
	if err := tc.customClient.List(context.TODO(), dsciInstances); err != nil {
		return fmt.Errorf("failed while listing DSCIs: %w", err)
	}

	if len(dsciInstances.Items) != 0 {
		return fmt.Errorf("expected DSCI removal, but got %v", dsciInstances)
	}

	return nil
}

func (tc *testContext) testDSCDeletionUsingConfigMap() error {
	dscLookupKey := types.NamespacedName{Name: tc.testDsc.Name}
	expectedDSC := &dsc.DataScienceCluster{}

	if err := createDeletionConfigMap(tc); err != nil {
		return err
	}

	err := tc.customClient.Get(tc.ctx, dscLookupKey, expectedDSC)
	if err == nil {
		dscerr := tc.customClient.Delete(tc.ctx, expectedDSC, &client.DeleteOptions{})
		if dscerr != nil {
			return fmt.Errorf("error deleting DSC instance %s: %w", expectedDSC.Name, dscerr)
		}
	} else if !k8serrors.IsNotFound(err) {
		if err != nil {
			return fmt.Errorf("error getting DSC instance :%w", err)
		}
	}

	return nil
}

func (tc *testContext) testOwnedNamespacesDeletion() error {
	if err := wait.PollUntilContextTimeout(tc.ctx, tc.resourceRetryInterval, tc.resourceCreationTimeout, false, func(ctx context.Context) (bool, error) {
		namespaces, err := tc.kubeClient.CoreV1().Namespaces().List(ctx, metav1.ListOptions{
			LabelSelector: labels.ODH.OwnedNamespace,
		})

		return len(namespaces.Items) == 0, err
	}); err != nil {
		return fmt.Errorf("failed waiting for all owned namespaces to be deleted: %w", err)
	}

	return nil
}

func removeDeletionConfigMap(tc *testContext) {
	_ = tc.kubeClient.CoreV1().ConfigMaps(tc.operatorNamespace).Delete(context.TODO(), "delete-self-managed", metav1.DeleteOptions{})
}

func createDeletionConfigMap(tc *testContext) error {
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "delete-self-managed",
			Namespace: tc.operatorNamespace,
			Labels: map[string]string{
				upgrade.DeleteConfigMapLabel: "true",
			},
		},
	}

	configMaps := tc.kubeClient.CoreV1().ConfigMaps(configMap.Namespace)
	if _, err := configMaps.Get(context.TODO(), configMap.Name, metav1.GetOptions{}); err != nil {
		switch {
		case k8serrors.IsNotFound(err):
			if _, err = configMaps.Create(context.TODO(), configMap, metav1.CreateOptions{}); err != nil {
				return err
			}
		case k8serrors.IsAlreadyExists(err):
			if _, err = configMaps.Update(context.TODO(), configMap, metav1.UpdateOptions{}); err != nil {
				return err
			}
		default:
			return err
		}
	}

	return nil
}
