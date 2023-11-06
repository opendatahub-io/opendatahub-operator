package e2e

import (
	"context"
	"log"

	operatorv1 "github.com/openshift/api/operator/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsc "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/codeflare"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/dashboard"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/datasciencepipelines"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/kserve"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/modelmeshserving"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/ray"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/trustyai"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/workbenches"
)

func (tc *testContext) waitForControllerDeployment(name string, replicas int32) error {
	err := wait.PollUntilContextTimeout(tc.ctx, tc.resourceRetryInterval, tc.resourceCreationTimeout, false, func(ctx context.Context) (done bool, err error) {
		controllerDeployment, err := tc.kubeClient.AppsV1().Deployments(tc.operatorNamespace).Get(tc.ctx, name, metav1.GetOptions{})
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

func setupDSCInstance() *dsc.DataScienceCluster {
	dscTest := &dsc.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-test",
		},
		Spec: dsc.DataScienceClusterSpec{
			Components: dsc.Components{
				// keep dashboard as enabled, because other test is rely on this
				Dashboard: dashboard.Dashboard{
					Component: components.Component{
						ManagementState: operatorv1.Managed,
					},
				},
				Workbenches: workbenches.Workbenches{
					Component: components.Component{
						ManagementState: operatorv1.Managed,
					},
				},
				ModelMeshServing: modelmeshserving.ModelMeshServing{
					Component: components.Component{
						ManagementState: operatorv1.Removed,
					},
				},
				DataSciencePipelines: datasciencepipelines.DataSciencePipelines{
					Component: components.Component{
						ManagementState: operatorv1.Managed,
					},
				},
				Kserve: kserve.Kserve{
					Component: components.Component{
						ManagementState: operatorv1.Managed,
					},
				},
				CodeFlare: codeflare.CodeFlare{
					Component: components.Component{
						ManagementState: operatorv1.Removed,
					},
				},
				Ray: ray.Ray{
					Component: components.Component{
						ManagementState: operatorv1.Managed,
					},
				},
				TrustyAI: trustyai.TrustyAI{
					Component: components.Component{
						ManagementState: operatorv1.Managed,
					},
				},
			},
		},
	}
	return dscTest
}

func (tc *testContext) validateCRD(crdName string) error {
	crd := &apiextv1.CustomResourceDefinition{}
	obj := client.ObjectKey{
		Name: crdName,
	}
	err := wait.PollUntilContextTimeout(tc.ctx, tc.resourceRetryInterval, tc.resourceCreationTimeout, false, func(ctx context.Context) (done bool, err error) {
		err = tc.customClient.Get(context.TODO(), obj, crd)
		if err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			log.Printf("Failed to get CRD %s", crdName)
			return false, err
		}

		for _, condition := range crd.Status.Conditions {
			if condition.Type == apiextv1.Established {
				if condition.Status == apiextv1.ConditionTrue {
					return true, nil
				}
			}
		}
		log.Printf("Error to get CRD %s condition's matching", crdName)
		return false, nil
	})
	return err
}
