// Package upgrade provides functions of upgrade ODH from v1 to v2 and vaiours v2 versions.
// It contains both the logic to upgrade the ODH components and the logic to cleanup the deprecated resources.
package upgrade

import (
	"context"
	"fmt"

	"github.com/hashicorp/go-multierror"
	operatorv1 "github.com/openshift/api/operator/v1"
	corev1 "k8s.io/api/core/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kfdefv1 "github.com/opendatahub-io/opendatahub-operator/apis/kfdef.apps.kubeflow.org/v1"
	dsc "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	dsci "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/codeflare"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/dashboard"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/datasciencepipelines"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/kserve"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/kueue"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/modelmeshserving"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/modelregistry"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/ray"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/trainingoperator"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/trustyai"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/workbenches"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/action"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

// CreateDefaultDSC creates a default instance of DSC.
// Note: When the platform is not Managed, and a DSC instance already exists, the function doesn't re-create/update the resource.
func CreateDefaultDSC(ctx context.Context, cli client.Client) error {
	// Set the default DSC name depending on the platform
	releaseDataScienceCluster := &dsc.DataScienceCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DataScienceCluster",
			APIVersion: "datasciencecluster.opendatahub.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "default-dsc",
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
					Component: components.Component{ManagementState: operatorv1.Managed},
				},
				CodeFlare: codeflare.CodeFlare{
					Component: components.Component{ManagementState: operatorv1.Managed},
				},
				Ray: ray.Ray{
					Component: components.Component{ManagementState: operatorv1.Managed},
				},
				Kueue: kueue.Kueue{
					Component: components.Component{ManagementState: operatorv1.Managed},
				},
				TrustyAI: trustyai.TrustyAI{
					Component: components.Component{ManagementState: operatorv1.Managed},
				},
				ModelRegistry: modelregistry.ModelRegistry{
					Component: components.Component{ManagementState: operatorv1.Removed},
				},
				TrainingOperator: trainingoperator.TrainingOperator{
					Component: components.Component{ManagementState: operatorv1.Removed},
				},
			},
		},
	}
	err := cli.Create(ctx, releaseDataScienceCluster)
	switch {
	case err == nil:
		fmt.Printf("created DataScienceCluster resource\n")
	case apierrs.IsAlreadyExists(err):
		// Do not update the DSC if it already exists
		fmt.Printf("DataScienceCluster resource already exists. It will not be updated with default DSC.\n")
		return nil
	default:
		return fmt.Errorf("failed to create DataScienceCluster custom resource: %w", err)
	}

	return nil
}

// createDefaultDSCI creates a default instance of DSCI
// If there exists an instance already, it patches the DSCISpec with default values
// Note: DSCI CR modifcations are not supported, as it is the initial prereq setting for the components.
func CreateDefaultDSCI(cli client.Client, _ cluster.Platform, appNamespace, monNamespace string) error {
	defaultDsciSpec := &dsci.DSCInitializationSpec{
		ApplicationsNamespace: appNamespace,
		Monitoring: dsci.Monitoring{
			ManagementState: operatorv1.Managed,
			Namespace:       monNamespace,
		},
		ServiceMesh: infrav1.ServiceMeshSpec{
			ManagementState: "Managed",
			ControlPlane: infrav1.ControlPlaneSpec{
				Name:              "data-science-smcp",
				Namespace:         "istio-system",
				MetricsCollection: "Istio",
			},
		},
		TrustedCABundle: dsci.TrustedCABundleSpec{
			ManagementState: "Managed",
		},
	}

	defaultDsci := &dsci.DSCInitialization{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DSCInitialization",
			APIVersion: "dscinitialization.opendatahub.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "default-dsci",
		},
		Spec: *defaultDsciSpec,
	}

	instances := &dsci.DSCInitializationList{}
	if err := cli.List(context.TODO(), instances); err != nil {
		return err
	}

	switch {
	case len(instances.Items) > 1:
		fmt.Println("only one instance of DSCInitialization object is allowed. Please delete other instances.")
		return nil
	case len(instances.Items) == 1:
		// Do not patch/update if DSCI already exists.
		fmt.Println("DSCInitialization resource already exists. It will not be updated with default DSCI.")
		return nil
	case len(instances.Items) == 0:
		fmt.Println("create default DSCI CR.")
		err := cli.Create(context.TODO(), defaultDsci)
		if err != nil {
			return err
		}
	}
	return nil
}

func UpdateFromLegacyVersion(cli client.Client, platform cluster.Platform, appNS string, montNamespace string) error {
	// If platform is Managed, remove Kfdefs and create default dsc
	if platform == cluster.ManagedRhods {
		fmt.Println("starting deletion of Deployment in managed cluster")
		if err := deleteResource(cli, appNS, "deployment"); err != nil {
			return err
		}
		// this is for the modelmesh monitoring part from v1 to v2
		if err := deleteResource(cli, montNamespace, "deployment"); err != nil {
			return err
		}
		if err := deleteResource(cli, montNamespace, "statefulset"); err != nil {
			return err
		}
		// only for downstream since ODH has a different way to create this CR by dashboard
		if err := unsetOwnerReference(cli, "odh-dashboard-config", appNS); err != nil {
			return err
		}

		// remove label created by previous v2 release which is problematic for Managed cluster
		fmt.Println("removing labels on Operator Namespace")
		operatorNamespace, err := cluster.GetOperatorNamespace()
		if err != nil {
			return err
		}
		if err := RemoveLabel(cli, operatorNamespace, labels.SecurityEnforce); err != nil {
			return err
		}

		fmt.Println("creating default DSC CR")
		if err := CreateDefaultDSC(context.TODO(), cli); err != nil {
			return err
		}
		return RemoveKfDefInstances(context.TODO(), cli)
	}

	if platform == cluster.SelfManagedRhods {
		// remove label created by previous v2 release which is problematic for Managed cluster
		fmt.Println("removing labels on Operator Namespace")
		operatorNamespace, err := cluster.GetOperatorNamespace()
		if err != nil {
			return err
		}
		if err := RemoveLabel(cli, operatorNamespace, labels.SecurityEnforce); err != nil {
			return err
		}
		// If KfDef CRD is not found, we see it as a cluster not pre-installed v1 operator	// Check if kfdef are deployed
		kfdefCrd := &apiextv1.CustomResourceDefinition{}
		if err := cli.Get(context.TODO(), client.ObjectKey{Name: "kfdefs.kfdef.apps.kubeflow.org"}, kfdefCrd); err != nil {
			if apierrs.IsNotFound(err) {
				// If no Crd found, return, since its a new Installation
				// return empty list
				return nil
			}
			return fmt.Errorf("error retrieving kfdef CRD : %w", err)
		}

		// If KfDef Instances found, and no DSC instances are found in Self-managed, that means this is an upgrade path from
		// legacy version. Create a default DSC instance
		kfDefList := &kfdefv1.KfDefList{}
		err = cli.List(context.TODO(), kfDefList)
		if err != nil {
			return fmt.Errorf("error getting kfdef instances: : %w", err)
		}
		fmt.Println("starting deletion of Deployment in selfmanaged cluster")
		if len(kfDefList.Items) > 0 {
			if err = deleteResource(cli, appNS, "deployment"); err != nil {
				return fmt.Errorf("error deleting deployment: %w", err)
			}
			// this is for the modelmesh monitoring part from v1 to v2
			if err := deleteResource(cli, montNamespace, "deployment"); err != nil {
				return err
			}
			if err := deleteResource(cli, montNamespace, "statefulset"); err != nil {
				return err
			}
			// only for downstream since ODH has a different way to create this CR by dashboard
			if err := unsetOwnerReference(cli, "odh-dashboard-config", appNS); err != nil {
				return err
			}
			// create default DSC
			if err = CreateDefaultDSC(context.TODO(), cli); err != nil {
				return err
			}
		}
	}
	return nil
}

func getDashboardWatsonResources(ns string) []action.ResourceSpec {
	metadataName := []string{"metadata", "name"}
	specAppName := []string{"spec", "appName"}
	appName := []string{"watson-studio"}

	return []action.ResourceSpec{
		{
			Gvk:       gvk.OdhQuickStart,
			Namespace: ns,
			Path:      specAppName,
			Values:    appName,
		},
		{
			Gvk:       gvk.OdhDocument,
			Namespace: ns,
			Path:      specAppName,
			Values:    appName,
		},
		{
			Gvk:       gvk.OdhApplication,
			Namespace: ns,
			Path:      metadataName,
			Values:    appName,
		},
	}
}

func getModelMeshResources(ns string, platform cluster.Platform) []action.ResourceSpec {
	metadataName := []string{"metadata", "name"}
	var toDelete []action.ResourceSpec

	if platform == cluster.ManagedRhods {
		del := []action.ResourceSpec{
			{
				Gvk:       gvk.Deployment,
				Namespace: ns,
				Path:      metadataName,
				Values:    []string{"rhods-prometheus-operator"},
			},
			{
				Gvk:       gvk.StatefulSet,
				Namespace: ns,
				Path:      metadataName,
				Values:    []string{"prometheus-rhods-model-monitoring"},
			},
			{
				Gvk:       gvk.Service,
				Namespace: ns,
				Path:      metadataName,
				Values:    []string{"rhods-model-monitoring"},
			},
			{
				Gvk:       gvk.Route,
				Namespace: ns,
				Path:      metadataName,
				Values:    []string{"rhods-model-monitoring"},
			},
			{
				Gvk:       gvk.Secret,
				Namespace: ns,
				Path:      metadataName,
				Values:    []string{"rhods-monitoring-oauth-config"},
			},
			{
				Gvk:       gvk.ClusterRole,
				Namespace: ns,
				Path:      metadataName,
				Values:    []string{"rhods-namespace-read", "rhods-prometheus-operator"},
			},
			{
				Gvk:       gvk.ClusterRoleBinding,
				Namespace: ns,
				Path:      metadataName,
				Values:    []string{"rhods-namespace-read", "rhods-prometheus-operator"},
			},
			{
				Gvk:       gvk.ServiceAccount,
				Namespace: ns,
				Path:      metadataName,
				Values:    []string{"rhods-prometheus-operator"},
			},
			{
				Gvk:       gvk.ServiceMonitor,
				Namespace: ns,
				Path:      metadataName,
				Values:    []string{"modelmesh-federated-metrics"},
			},
		}
		toDelete = append(toDelete, del...)
	}
	// common logic for both self-managed and managed
	del := action.ResourceSpec{
		Gvk:       gvk.ServiceMonitor,
		Namespace: ns,
		Path:      metadataName,
		Values:    []string{"rhods-monitor-federation2"},
	}

	toDelete = append(toDelete, del)
	return toDelete
}

// TODO: remove function once we have a generic solution across all components.
func CleanupExistingResource(ctx context.Context, cli client.Client, platform cluster.Platform, dscApplicationsNamespace, dscMonitoringNamespace string) error {
	var multiErr *multierror.Error
	// Special Handling of cleanup of deprecated model monitoring stack

	// Remove deprecated opendatahub namespace(owned by kuberay)
	multiErr = multierror.Append(multiErr, deleteDeprecatedNamespace(ctx, cli, "opendatahub"))

	toDelete := getModelMeshResources(dscMonitoringNamespace, platform)
	toDelete = append(toDelete, getDashboardWatsonResources(dscApplicationsNamespace)...)
	// Handling for dashboard Jupyterhub CR, see jira #443
	toDelete = append(toDelete, action.ResourceSpec{
		Gvk:       gvk.OdhApplication,
		Namespace: dscApplicationsNamespace,
		Path:      []string{"metadata", "name"},
		Values:    []string{"jupyterhub"},
	})

	multiErr = multierror.Append(multiErr, action.NewDelete(cli).Exec(ctx, toDelete...))

	return multiErr.ErrorOrNil()
}

func RemoveKfDefInstances(ctx context.Context, cli client.Client) error {
	return action.NewDeleteWithFinalizer(cli).
		Exec(ctx, action.ResourceSpec{
			Gvk: gvk.KfDef,
		})
}

func unsetOwnerReference(cli client.Client, instanceName string, applicationNS string) error {
	return action.NewDeleteOwnersReferences(cli).
		Exec(context.TODO(), action.ResourceSpec{
			Gvk:       gvk.OdhDashboardConfig,
			Namespace: applicationNS,
			Path:      []string{"metadata", "name"},
			Values:    []string{instanceName},
		})
}

func deleteResource(cli client.Client, namespace string, resourceType string) error {
	// In v2, Deployment selectors use a label "app.opendatahub.io/<componentName>" which is
	// not present in v1. Since label selectors are immutable, we need to delete the existing
	// deployments and recreated them.
	// because we can't proceed if a deployment is not deleted, we use exponential backoff
	// to retry the deletion until it succeeds

	toDelete := action.ResourceSpec{
		Namespace: namespace,
	}

	switch resourceType {
	case "deployment":
		toDelete.Gvk = gvk.Deployment
	case "statefulset":
		toDelete.Gvk = gvk.StatefulSet
	}

	matcher := action.Not(action.MatchMapKeyContains(labels.ODHAppPrefix, "spec", "selector", "matchLabels"))
	act := action.NewDeleteMatched(cli, matcher)

	return act.ExecWithRetry(context.TODO(), action.IfAnyLeft(matcher), toDelete)
}

func RemoveLabel(cli client.Client, objectName string, labelKey string) error {
	return action.NewDeleteLabel(cli, labelKey).
		Exec(context.TODO(), action.ResourceSpec{
			Gvk:    gvk.Namespace,
			Path:   []string{"metadata", "name"},
			Values: []string{objectName},
		})
}

func deleteDeprecatedNamespace(ctx context.Context, cli client.Client, namespace string) error {
	foundNamespace := &corev1.Namespace{}
	if err := cli.Get(ctx, client.ObjectKey{Name: namespace}, foundNamespace); err != nil {
		if apierrs.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("could not get %s namespace: %w", namespace, err)
	}

	// Check if namespace is owned by DSC
	isOwnedByDSC := false
	for _, owner := range foundNamespace.OwnerReferences {
		if owner.Kind == "DataScienceCluster" {
			isOwnedByDSC = true
		}
	}
	if !isOwnedByDSC {
		return nil
	}

	// Check if namespace has pods running
	podList := &corev1.PodList{}
	listOpts := []client.ListOption{
		client.InNamespace(namespace),
	}
	if err := cli.List(ctx, podList, listOpts...); err != nil {
		return fmt.Errorf("error getting pods from namespace %s: %w", namespace, err)
	}
	if len(podList.Items) != 0 {
		fmt.Printf("Skip deletion of namespace %s due to running Pods in it\n", namespace)
		return nil
	}

	// Delete namespace if no pods found
	if err := cli.Delete(ctx, foundNamespace); err != nil {
		return fmt.Errorf("could not delete %s namespace: %w", namespace, err)
	}

	return nil
}
