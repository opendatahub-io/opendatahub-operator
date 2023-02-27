package e2e

import (
	"context"
	"fmt"
	kfdefappskubefloworgv1 "github.com/opendatahub-io/opendatahub-operator/apis/kfdef.apps.kubeflow.org/v1"
	"github.com/stretchr/testify/require"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"log"
	"testing"
	"time"
)

func creationTestSuite(t *testing.T) {
	testCtx, err := NewTestContext()
	require.NoError(t, err)
	for _, kfContext := range testCtx.testKfDefs {
		t.Run(kfContext.kfObjectMeta.Name, func(t *testing.T) {
			t.Run("Creation of KfDef instance", func(t *testing.T) {
				err = testCtx.testKfDefCreation(kfContext)
				require.NoError(t, err, "error creating KFDef object ")
			})
			t.Run("Validate deployed applications", func(t *testing.T) {
				for i, _ := range kfContext.kfSpec.Applications {
					t.Run("Validate deployments for application "+kfContext.kfSpec.Applications[i].Name, func(t *testing.T) {
						err = testCtx.testKfDefApplications(kfContext.kfObjectMeta, kfContext.kfSpec.Applications[i].Name)
						require.NoError(t, err, "error testing deployments for application %v", kfContext.kfSpec.Applications[i].Name)
					})
				}
			})
			t.Run("Validate KfDef Annotation is added", func(t *testing.T) {
				err = testCtx.testKfDefAnnotation(kfContext)
				require.NoError(t, err, "error testing KfDef annotation ")
			})
			t.Run("Validate Controller Reverts Updates", func(t *testing.T) {
				err = testCtx.testUpdateManagedResource(kfContext)
				require.NoError(t, err, "error testing updates for Kfdef resource ")
			})
		})
	}

}

func (tc *testContext) testKfDefCreation(kfContext kfDefContext) error {

	testKfDef := &kfdefappskubefloworgv1.KfDef{
		ObjectMeta: *kfContext.kfObjectMeta,
		Spec:       *kfContext.kfSpec,
	}

	// Create KfDef resource if not already created
	kfDefLookupKey := types.NamespacedName{Name: testKfDef.Name, Namespace: testKfDef.Namespace}
	createdKfDef := &kfdefappskubefloworgv1.KfDef{}

	err := tc.customClient.Get(tc.ctx, kfDefLookupKey, createdKfDef)
	if err != nil {
		if errors.IsNotFound(err) {
			nberr := wait.Poll(tc.resourceRetryInterval, tc.resourceCreationTimeout, func() (done bool, err error) {
				creationErr := tc.customClient.Create(tc.ctx, testKfDef)
				if creationErr != nil {
					log.Printf("Error creating KfDef resource %v: %v, trying again",
						testKfDef.Name, creationErr)
					return false, nil
				} else {
					return true, nil
				}
			})
			if nberr != nil {
				return fmt.Errorf("error creating test KfDef %s: %v", testKfDef.Name, nberr)
			}
		} else {
			return fmt.Errorf("error getting test KfDef %s: %v", testKfDef.Name, err)
		}
	}
	return nil
}

// This tests verifies given an application names, corresponding deployments are running with label `app: <app-name>`
func (tc *testContext) testKfDefApplications(kfmeta *metav1.ObjectMeta, appName string) error {

	err := wait.Poll(tc.resourceRetryInterval, tc.resourceCreationTimeout, func() (done bool, err error) {
		appList, err := tc.kubeClient.AppsV1().Deployments(kfmeta.Namespace).List(context.TODO(), metav1.ListOptions{
			LabelSelector: "app=" + appName,
		})
		if err != nil {
			log.Printf("error listing application deployments :%v. Trying again...", err)
			return false, err
		}
		if len(appList.Items) != 0 {
			allAppDeploymentsReady := true
			for _, deployment := range appList.Items {
				if deployment.Status.ReadyReplicas < 1 {
					allAppDeploymentsReady = false
				}
			}
			if allAppDeploymentsReady {
				return true, nil
			} else {
				log.Printf("waiting for application deployments to be in Ready state.")
				return false, nil
			}
		}
		return false, nil
	})
	if err != nil {
		return err
	}
	return err
}

// This test verifies all resources of an application have the kfdef annotation added by the operator
func (tc *testContext) testKfDefAnnotation(kfContext kfDefContext) error {
	testapp := kfContext.kfSpec.Applications[0].Name

	appDeployments, err := tc.kubeClient.AppsV1().Deployments(kfContext.kfObjectMeta.Namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: "app=" + testapp,
	})
	if err != nil {
		return fmt.Errorf("error list application deployments %v", err)
	} else {
		// test any one deployment for annotation
		if len(appDeployments.Items) != 0 && appDeployments.Items[0].Annotations["kfctl.kubeflow.io/kfdef-instance"] !=
			kfContext.kfObjectMeta.Name+"."+kfContext.kfObjectMeta.Namespace {

			return fmt.Errorf("expected annotation not found. Got annotation: %v",
				appDeployments.Items[0].Annotations["kfctl.kubeflow.io/kfdef-instance"])
		}
	}

	appSecrets, err := tc.kubeClient.CoreV1().Secrets(kfContext.kfObjectMeta.Namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: "app=" + testapp,
	})
	if err != nil {
		return fmt.Errorf("error list application secrets %v", err)
	} else {
		// test any one secret for annotation
		if len(appSecrets.Items) != 0 && appSecrets.Items[0].Annotations["kfctl.kubeflow.io/kfdef-instance"] !=
			kfContext.kfObjectMeta.Name+"."+kfContext.kfObjectMeta.Namespace {

			return fmt.Errorf("expected annotation not found. Got annotation: %v",
				appSecrets.Items[0].Annotations["kfctl.kubeflow.io/kfdef-instance"])
		}
	}

	appConfigMaps, err := tc.kubeClient.CoreV1().ConfigMaps(kfContext.kfObjectMeta.Namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: "app=" + testapp,
	})
	if err != nil {
		return fmt.Errorf("error list application secrets %v", err)
	} else {
		// test any one configmap for annotation
		if len(appConfigMaps.Items) != 0 && appConfigMaps.Items[0].Annotations["kfctl.kubeflow.io/kfdef-instance"] !=
			kfContext.kfObjectMeta.Name+"."+kfContext.kfObjectMeta.Namespace {

			return fmt.Errorf("expected annotation not found. Got annotation: %v",
				appConfigMaps.Items[0].Annotations["kfctl.kubeflow.io/kfdef-instance"])
		}
	}

	return nil
}

// this test verifies that any updates to resources managed by KfDef are reverted
func (tc *testContext) testUpdateManagedResource(kfContext kfDefContext) error {
	appDeployments, err := tc.kubeClient.AppsV1().Deployments(kfContext.kfObjectMeta.Namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: "app=" + kfContext.kfSpec.Applications[0].Name,
	})

	if err != nil {
		return err
	}
	if len(appDeployments.Items) != 0 {
		testDeployment := appDeployments.Items[0]
		expectedReplica := testDeployment.Spec.Replicas
		patchedReplica := &autoscalingv1.Scale{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testDeployment.Name,
				Namespace: testDeployment.Namespace,
			},
			Spec: autoscalingv1.ScaleSpec{
				Replicas: 3,
			},
			Status: autoscalingv1.ScaleStatus{},
		}

		retrievedDep, err := tc.kubeClient.AppsV1().Deployments(kfContext.kfObjectMeta.Namespace).UpdateScale(context.TODO(), testDeployment.Name, patchedReplica, metav1.UpdateOptions{})
		if err != nil {

			return fmt.Errorf("error patching application resources : %v", err)
		}
		if retrievedDep.Spec.Replicas != patchedReplica.Spec.Replicas {
			return fmt.Errorf("failed to patch replicas")

		}
		// Sleep for 20 seconds to allow the operator to reconcile
		time.Sleep(2 * tc.resourceRetryInterval)
		revertedDep, err := tc.kubeClient.AppsV1().Deployments(kfContext.kfObjectMeta.Namespace).Get(context.TODO(), testDeployment.Name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("error getting application resource : %v", err)
		}

		if *revertedDep.Spec.Replicas != *expectedReplica {
			return fmt.Errorf("failed to revert updated resource")
		}

	}

	return nil

}
