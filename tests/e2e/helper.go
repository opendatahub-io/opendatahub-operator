package e2e

import (
	"context"
	"log"

	corev1 "k8s.io/api/core/v1"

	kfdefappskubefloworgv1 "github.com/opendatahub-io/opendatahub-operator/apis/kfdef.apps.kubeflow.org/v1"
	appsv1 "k8s.io/api/apps/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (tc *testContext) waitForControllerDeployment(name string, replicas int32) error {
	err := wait.Poll(tc.resourceRetryInterval, tc.resourceCreationTimeout, func() (done bool, err error) {

		controllerDeployment, err := tc.kubeClient.AppsV1().Deployments(tc.testNamespace).Get(tc.ctx, name, metav1.GetOptions{})

		if err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			log.Printf("Failed to get %s controller deployment", name)
			return false, err
		}

		for _, condition := range controllerDeployment.Status.Conditions {
			if condition.Type == appsv1.DeploymentAvailable {
				if condition.Status == corev1.ConditionTrue && controllerDeployment.Status.ReadyReplicas == replicas {
					return true, nil
				}
			}
		}

		log.Printf("Error in %s deployment", name)
		return false, nil

	})
	return err
}

func setupCoreKfdef() kfDefContext {
	kfDefRes := &kfdefappskubefloworgv1.KfDef{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "e2e-test",
			Namespace: kfdefTestNamespace,
		},
		Spec: kfdefappskubefloworgv1.KfDefSpec{
			Applications: []kfdefappskubefloworgv1.Application{
				kfdefappskubefloworgv1.Application{
					Name: "odh-dashboard",
					KustomizeConfig: &kfdefappskubefloworgv1.KustomizeConfig{
						RepoRef: &kfdefappskubefloworgv1.RepoRef{
							Name: "manifests",
							Path: "odh-dashboard",
						},
					},
				},
				{
					Name: "sample-app",
					KustomizeConfig: &kfdefappskubefloworgv1.KustomizeConfig{
						RepoRef: &kfdefappskubefloworgv1.RepoRef{
							Name: "sample-manifests",
							Path: "/data/manifests/sample-app",
						},
					},
				},
			},
			Repos: []kfdefappskubefloworgv1.Repo{
				{
					Name: "manifests",
					URI:  "https://github.com/opendatahub-io/odh-manifests/tarball/master",
				}, {
					//Any update to manifests should be reflected in the tar.gz file by doing
					//`make update-test-data`
					Name: "sample-manifests",
					URI:  "file:///opt/test-data/test-data.tar.gz",
				},
			},
		},
	}

	return kfDefContext{
		kfObjectMeta: &kfDefRes.ObjectMeta,
		kfSpec:       &kfDefRes.Spec,
	}

}

func (tc *testContext) validateCRD(crdName string) error {
	crd := &apiextv1.CustomResourceDefinition{}
	obj := client.ObjectKey{
		Name: crdName,
	}
	err := wait.Poll(tc.resourceRetryInterval, tc.resourceCreationTimeout, func() (done bool, err error) {
		err = tc.customClient.Get(context.TODO(), obj, crd)
		if err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			log.Printf("Failed to get %s crd", crdName)
			return false, err
		}

		for _, condition := range crd.Status.Conditions {
			if condition.Type == apiextv1.Established {
				if condition.Status == apiextv1.ConditionTrue {
					return true, nil
				}
			}
		}
		log.Printf("Error in getting %s crd ", crdName)
		return false, nil

	})
	return err
}
