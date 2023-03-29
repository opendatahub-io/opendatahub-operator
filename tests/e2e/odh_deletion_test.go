package e2e

import (
	"context"
	"fmt"
	"log"
	"testing"

	kfdefappskubefloworgv1 "github.com/opendatahub-io/opendatahub-operator/apis/kfdef.apps.kubeflow.org/v1"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func deletionTestSuite(t *testing.T) {
	testCtx, err := NewTestContext()
	require.NoError(t, err)
	for _, kfContext := range testCtx.testKfDefs {
		t.Run(kfContext.kfObjectMeta.Name, func(t *testing.T) {
			t.Run("KfDef Deletion", func(t *testing.T) {
				err = testCtx.testKfDefDeletion(kfContext.kfObjectMeta)
				require.NoError(t, err, "error deleting KfDef object ")
			})
			t.Run("Application Resource Deletion", func(t *testing.T) {
				for i, _ := range kfContext.kfSpec.Applications {
					t.Run("Validate deletion for application "+kfContext.kfSpec.Applications[i].Name, func(t *testing.T) {
						err = testCtx.testKfDefResourcesDeletion(kfContext.kfObjectMeta, kfContext.kfSpec.Applications[i].Name)
						require.NoError(t, err, "error testing deletion for application %v", kfContext.kfSpec.Applications[i].Name)
					})
				}
			})
		})
	}
}

func (tc *testContext) testKfDefDeletion(kfMeta *metav1.ObjectMeta) error {
	// Delete test KfDef resource if found
	kfDefLookupKey := types.NamespacedName{Name: kfMeta.Name, Namespace: kfMeta.Namespace}
	createdKfDef := &kfdefappskubefloworgv1.KfDef{}

	err := tc.customClient.Get(tc.ctx, kfDefLookupKey, createdKfDef)
	if err == nil {
		kferr := tc.customClient.Delete(tc.ctx, createdKfDef, &client.DeleteOptions{})
		if kferr != nil {
			return fmt.Errorf("error deleting test KfDef %s: %v", kfMeta.Name, kferr)
		}
	} else if !errors.IsNotFound(err) {
		if err != nil {
			return fmt.Errorf("error getting test KfDef instance :%v", err)
		}
	}
	return nil
}

func (tc *testContext) testKfDefResourcesDeletion(kfmeta *metav1.ObjectMeta, appName string) error {

	// Deletion of Deployments
	if err := wait.Poll(tc.resourceRetryInterval, tc.resourceCreationTimeout, func() (done bool, err error) {
		appList, err := tc.kubeClient.AppsV1().Deployments(kfmeta.Namespace).List(context.TODO(), metav1.ListOptions{
			LabelSelector: "app=" + appName,
		})
		if err != nil {
			log.Printf("error listing application deployments :%v. Trying again...", err)
			return false, err
		}
		if len(appList.Items) != 0 {
			return false, nil
		} else {
			return true, nil
		}
	}); err != nil {
		return err
	}

	// Deletion of Secrets
	if err := wait.Poll(tc.resourceRetryInterval, tc.resourceCreationTimeout, func() (done bool, err error) {
		appList, err := tc.kubeClient.CoreV1().Secrets(kfmeta.Namespace).List(context.TODO(), metav1.ListOptions{
			LabelSelector: "app=" + appName,
		})
		if err != nil {
			log.Printf("error listing application secrets :%v. Trying again...", err)
			return false, err
		}
		if len(appList.Items) != 0 {
			return false, nil
		} else {
			return true, nil
		}
	}); err != nil {
		return err
	}

	// Deletion of ConfigMaps
	if err := wait.Poll(tc.resourceRetryInterval, tc.resourceCreationTimeout, func() (done bool, err error) {
		appList, err := tc.kubeClient.CoreV1().ConfigMaps(kfmeta.Namespace).List(context.TODO(), metav1.ListOptions{
			LabelSelector: "app=" + appName,
		})
		if err != nil {
			log.Printf("error listing application configmaps :%v. Trying again...", err)
			return false, err
		}
		if len(appList.Items) != 0 {
			return false, nil
		} else {
			return true, nil
		}
	}); err != nil {
		return err
	}

	return nil
}
