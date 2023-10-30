package upgrade

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	kfdefv1 "github.com/opendatahub-io/opendatahub-operator/apis/kfdef.apps.kubeflow.org/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	ofapi "github.com/operator-framework/api/pkg/operators/v1alpha1"
	olmclientset "github.com/operator-framework/operator-lifecycle-manager/pkg/api/client/clientset/versioned/typed/operators/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsc "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	dsci "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/codeflare"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/dashboard"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/datasciencepipelines"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/kserve"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/modelmeshserving"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/ray"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/workbenches"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

const (
	// DeleteConfigMapLabel is the label for configMap used to trigger operator uninstall
	// TODO: Label should be updated if addon name changes
	DeleteConfigMapLabel = "api.openshift.com/addon-managed-odh-delete"
	// odhGeneratedNamespaceLabel is the label added to all the namespaces genereated by odh-deployer
	odhGeneratedNamespaceLabel = "opendatahub.io/generated-namespace"
)

// OperatorUninstall deletes all the externally generated resources. This includes monitoring resources and applications
// installed by KfDef.
func OperatorUninstall(cli client.Client, cfg *rest.Config) error {
	platform, err := deploy.GetPlatform(cli)
	if err != nil {
		return err
	}
	// Delete kfdefs if found
	err = RemoveKfDefInstances(cli, platform)
	if err != nil {
		return err
	}

	// Delete DSCInitialization instance
	err = removeDSCInitialization(cli)
	if err != nil {
		return err
	}
	// Delete generated namespaces by the operator
	generatedNamespaces := &corev1.NamespaceList{}
	nsOptions := []client.ListOption{
		client.MatchingLabels{odhGeneratedNamespaceLabel: "true"},
	}
	if err := cli.List(context.TODO(), generatedNamespaces, nsOptions...); err != nil {
		if !apierrs.IsNotFound(err) {
			return fmt.Errorf("error getting generated namespaces : %v", err)
		}
	}

	// Return if any one of the namespaces is Terminating due to resources that are in process of deletion. (e.g CRDs)
	if len(generatedNamespaces.Items) != 0 {
		for _, namespace := range generatedNamespaces.Items {
			if namespace.Status.Phase == corev1.NamespaceTerminating {
				return fmt.Errorf("waiting for namespace %v to be deleted", namespace.Name)
			}
		}
	}

	// Delete all the active namespaces
	for _, namespace := range generatedNamespaces.Items {
		if namespace.Status.Phase == corev1.NamespaceActive {
			if err := cli.Delete(context.TODO(), &namespace, []client.DeleteOption{}...); err != nil {
				return fmt.Errorf("error deleting namespace %v: %v", namespace.Name, err)
			}
			fmt.Printf("Namespace %s deleted as a part of uninstall.", namespace.Name)
		}
	}

	// Wait for all resources to get cleaned up
	time.Sleep(10 * time.Second)
	fmt.Printf("All resources deleted as part of uninstall. Removing the operator csv")
	return removeCsv(cli, cfg)
}

func removeDSCInitialization(cli client.Client) error {
	// Last check if multiple instances of DSCInitialization exist
	instanceList := &dsci.DSCInitializationList{}
	var err error
	err = cli.List(context.TODO(), instanceList)
	if err != nil {
		return err
	}

	if len(instanceList.Items) != 0 {
		for _, dsciInstance := range instanceList.Items {
			err = cli.Delete(context.TODO(), &dsciInstance)
			if apierrs.IsNotFound(err) {
				err = nil
			}
		}
	}
	return err
}

// HasDeleteConfigMap returns true if delete configMap is added to the operator namespace by managed-tenants repo.
// It returns false in all other cases.
func HasDeleteConfigMap(c client.Client) bool {
	// Get watchNamespace
	operatorNamespace, err := GetOperatorNamespace()
	if err != nil {
		return false
	}

	// If delete configMap is added, uninstall the operator and the resources
	deleteConfigMapList := &corev1.ConfigMapList{}
	cmOptions := []client.ListOption{
		client.InNamespace(operatorNamespace),
		client.MatchingLabels{DeleteConfigMapLabel: "true"},
	}

	if err := c.List(context.TODO(), deleteConfigMapList, cmOptions...); err != nil {
		return false
	}
	return len(deleteConfigMapList.Items) != 0
}

// createDefaultDSC creates a default instance of DSC.
// Note: When the platform is not Managed, and a DSC instance already exists, the function doesn't re-create/update the resource.
func CreateDefaultDSC(cli client.Client, platform deploy.Platform) error {
	releaseDataScienceCluster := &dsc.DataScienceCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DataScienceCluster",
			APIVersion: "datasciencecluster.opendatahub.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "default",
		},
		Spec: dsc.DataScienceClusterSpec{
			Components: dsc.Components{
				Dashboard: dashboard.Dashboard{
					Component: components.Component{ManagementState: operatorv1.Managed},
				},
				Workbenches: workbenches.Workbenches{
					Component: components.Component{ManagementState: operatorv1.Managed},
				},
				ModelMeshServing: modelmeshserving.ModelMeshServing{
					Component: components.Component{ManagementState: operatorv1.Managed},
				},
				DataSciencePipelines: datasciencepipelines.DataSciencePipelines{
					Component: components.Component{ManagementState: operatorv1.Managed},
				},
				Kserve: kserve.Kserve{
					Component: components.Component{ManagementState: operatorv1.Removed},
				},
				CodeFlare: codeflare.CodeFlare{
					Component: components.Component{ManagementState: operatorv1.Managed},
				},
				Ray: ray.Ray{
					Component: components.Component{ManagementState: operatorv1.Managed},
				},
			},
		},
	}
	err := cli.Create(context.TODO(), releaseDataScienceCluster)
	switch {
	case err == nil:
		fmt.Printf("created DataScienceCluster resource")
	case apierrs.IsAlreadyExists(err):
		// Update if already exists
		if platform == deploy.ManagedRhods {
			fmt.Printf("DataScienceCluster resource already exists in Managed. Updating it.")
			data, err := json.Marshal(releaseDataScienceCluster)
			if err != nil {
				return fmt.Errorf("failed to get DataScienceCluster custom resource data: %v", err)
			}
			err = cli.Patch(context.TODO(), releaseDataScienceCluster, client.RawPatch(types.ApplyPatchType, data),
				client.ForceOwnership, client.FieldOwner("opendatahub-operator"))
			if err != nil {
				return fmt.Errorf("failed to update DataScienceCluster custom resource:%v", err)
			}
		} else {
			fmt.Printf("DataScienceCluster resource already exists. It will not be updated with default DSC.")
			return nil
		}
	default:
		return fmt.Errorf("failed to create DataScienceCluster custom resource: %v", err)
	}
	return nil
}

func UpdateFromLegacyVersion(cli client.Client, platform deploy.Platform) error {
	// If platform is Managed, remove Kfdefs and create default dsc
	if platform == deploy.ManagedRhods {
		err := CreateDefaultDSC(cli, platform)
		if err != nil {
			return err
		}

		err = RemoveKfDefInstances(cli, platform)
		if err != nil {
			return err
		}
		return nil
	}

	if platform == deploy.SelfManagedRhods {
		kfDefList, err := getKfDefInstances(cli)
		if err != nil {
			return fmt.Errorf("error getting kfdef instances: %v", err)
		}

		if len(kfDefList.Items) > 0 {
			err := CreateDefaultDSC(cli, platform)
			if err != nil {
				return err
			}
		}
		return err
	}
	return nil
}

func GetOperatorNamespace() (string, error) {
	data, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err == nil {
		if ns := strings.TrimSpace(string(data)); len(ns) > 0 {
			return ns, nil
		}
	}
	return "", err
}

func RemoveKfDefInstances(cli client.Client, platform deploy.Platform) error {
	// Check if kfdef are deployed
	expectedKfDefList, err := getKfDefInstances(cli)
	if err != nil {
		return err
	}
	// Delete kfdefs
	if len(expectedKfDefList.Items) > 0 {
		for _, kfdef := range expectedKfDefList.Items {
			// Remove finalizer
			updatedKfDef := &kfdef
			updatedKfDef.Finalizers = []string{}
			err = cli.Update(context.TODO(), updatedKfDef)
			if err != nil {
				return fmt.Errorf("error removing finalizers from kfdef %v : %v", kfdef.Name, err)
			}
			err = cli.Delete(context.TODO(), updatedKfDef)
			if err != nil {
				return fmt.Errorf("error deleting kfdef %v : %v", kfdef.Name, err)
			}
		}
	}

	return nil
}

func removeCsv(c client.Client, r *rest.Config) error {
	// Get watchNamespace
	operatorNamespace, err := GetOperatorNamespace()
	if err != nil {
		return err
	}

	operatorCsv, err := getClusterServiceVersion(r, operatorNamespace)
	if err != nil {
		return err
	}

	if operatorCsv != nil {
		fmt.Printf("Deleting csv %s", operatorCsv.Name)
		err = c.Delete(context.TODO(), operatorCsv, []client.DeleteOption{}...)
		if err != nil {
			if apierrs.IsNotFound(err) {
				return nil
			}
			return fmt.Errorf("error deleting clusterserviceversion: %v", err)
		}
		fmt.Printf("Clusterserviceversion %s deleted as a part of uninstall.", operatorCsv.Name)
	}
	fmt.Printf("No clusterserviceversion for the operator found.")
	return nil
}

// getClusterServiceVersion retries the clusterserviceversions available in the operator namespace.
func getClusterServiceVersion(cfg *rest.Config, watchNameSpace string) (*ofapi.ClusterServiceVersion, error) {
	operatorClient, err := olmclientset.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("error getting operator client %v", err)
	}
	csvs, err := operatorClient.ClusterServiceVersions(watchNameSpace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	// get csv with CRD DataScienceCluster
	if len(csvs.Items) != 0 {
		for _, csv := range csvs.Items {
			for _, operatorCR := range csv.Spec.CustomResourceDefinitions.Owned {
				if operatorCR.Kind == "DataScienceCluster" {
					return &csv, nil
				}
			}
		}
	}
	return nil, nil
}

func getKfDefInstances(c client.Client) (*kfdefv1.KfDefList, error) {
	// If KfDef CRD is not found, we see it as a cluster not pre-installed v1 operator	// Check if kfdef are deployed
	kfdefCrd := &apiextv1.CustomResourceDefinition{}
	err := c.Get(context.TODO(), client.ObjectKey{Name: "kfdefs.kfdef.apps.kubeflow.org"}, kfdefCrd)
	if err != nil {
		if apierrs.IsNotFound(err) {
			// If no Crd found, return, since its a new Installation
			return nil, nil
		} else {
			return nil, fmt.Errorf("error retrieving kfdef CRD : %v", err)
		}
	}

	// If KfDef Instances found, and no DSC instances are found in Self-managed, that means this is an upgrade path from
	// legacy version. Create a default DSC instance
	kfDefList := &kfdefv1.KfDefList{}
	err = c.List(context.TODO(), kfDefList)
	if err != nil {
		if apierrs.IsNotFound(err) {
			// If no KfDefs, do nothing and return
			return nil, nil
		} else {
			return nil, fmt.Errorf("error getting list of kfdefs: %v", err)
		}
	}
	return kfDefList, nil
}
