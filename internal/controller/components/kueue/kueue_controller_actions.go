package kueue

import (
	"context"
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

func checkPreConditions(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	kueueCRInstance, ok := rr.Instance.(*componentApi.Kueue)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.Kueue)", rr.Instance)
	}

	rConfig, err := cluster.HasCRD(ctx, rr.Client, gvk.MultiKueueConfigV1Alpha1)
	if err != nil {
		return odherrors.NewStopError("failed to check %s CRDs version: %w", gvk.MultiKueueConfigV1Alpha1, err)
	}

	rCluster, err := cluster.HasCRD(ctx, rr.Client, gvk.MultikueueClusterV1Alpha1)
	if err != nil {
		return odherrors.NewStopError("failed to check %s CRDs version: %w", gvk.MultikueueClusterV1Alpha1, err)
	}

	if rConfig || rCluster {
		return odherrors.NewStopError(status.MultiKueueCRDMessage)
	}

	switch kueueCRInstance.Spec.ManagementState {
	case operatorv1.Managed:
		if found, err := cluster.OperatorExists(ctx, rr.Client, kueueOperator); err != nil || found {
			if err != nil {
				return odherrors.NewStopErrorW(err)
			}

			return odherrors.NewStopErrorW(ErrKueueOperatorAlreadyInstalled)
		}
	case operatorv1.Unmanaged:
		if found, err := cluster.OperatorExists(ctx, rr.Client, kueueOperator); err != nil || !found {
			if err != nil {
				return odherrors.NewStopErrorW(err)
			}

			return odherrors.NewStopErrorW(ErrKueueOperatorNotInstalled)
		}
	default:
		return nil
	}

	return nil
}

func initialize(_ context.Context, rr *odhtypes.ReconciliationRequest) error {
	kueueCRInstance, ok := rr.Instance.(*componentApi.Kueue)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.Kueue)", rr.Instance)
	}

	if kueueCRInstance.Spec.ManagementState == operatorv1.Managed {
		rr.Manifests = append(rr.Manifests, manifestsPath())
	}
	rr.Manifests = append(rr.Manifests, kueueConfigManifestsPath())

	return nil
}

func devFlags(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	kueue, ok := rr.Instance.(*componentApi.Kueue)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.Kueue)", rr.Instance)
	}

	if kueue.Spec.DevFlags == nil {
		return nil
	}

	// Implement devflags support logic
	// If dev flags are set, update default manifests path
	if len(kueue.Spec.DevFlags.Manifests) != 0 {
		manifestConfig := kueue.Spec.DevFlags.Manifests[0]
		if err := odhdeploy.DownloadManifests(ctx, ComponentName, manifestConfig); err != nil {
			return err
		}

		if manifestConfig.SourcePath != "" {
			rr.Manifests[0].Path = odhdeploy.DefaultManifestPath
			rr.Manifests[0].ContextDir = ComponentName
			rr.Manifests[0].SourcePath = manifestConfig.SourcePath
		}
	}

	return nil
}

func configureClusterQueueViewerRoleAction(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	c := rr.Client
	var cr rbacv1.ClusterRole
	cr.Name = ClusterQueueViewerRoleName
	if err := c.Get(ctx, client.ObjectKeyFromObject(&cr), &cr); err != nil {
		return client.IgnoreNotFound(err)
	}
	l := cr.GetLabels()
	if l == nil {
		l = map[string]string{}
	}
	if l[KueueBatchUserLabel] == "true" {
		return nil
	}
	l[KueueBatchUserLabel] = "true"
	cr.SetLabels(l)
	return c.Update(ctx, &cr)
}

func manageKueueAdminRoleBinding(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	log := logf.FromContext(ctx)

	// Get the Auth CR to access admin groups
	authCR := &serviceApi.Auth{}
	err := rr.Client.Get(ctx, client.ObjectKey{Name: serviceApi.AuthInstanceName}, authCR)
	if err != nil {
		if k8serr.IsNotFound(err) {
			log.Info("Auth CR not found, skipping kueue admin role binding creation")
			return nil
		}
		return fmt.Errorf("failed to get Auth CR: %w", err)
	}

	// Filter admin groups (exclude system:authenticated and empty strings)
	// This is needed for upgrade scenarios where Auth CRs might contain invalid groups
	// from before the webhook was implemented
	var validAdminGroups []string
	for _, group := range authCR.Spec.AdminGroups {
		if group != "system:authenticated" && group != "" {
			validAdminGroups = append(validAdminGroups, group)
		}
	}

	// Create subjects for the role binding
	subjects := []rbacv1.Subject{}
	for _, group := range validAdminGroups {
		subjects = append(subjects, rbacv1.Subject{
			Kind:     gvk.Group.Kind,
			APIGroup: gvk.Group.Group,
			Name:     group,
		})
	}

	// Create the ClusterRoleBinding
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: KueueAdminRoleBindingName,
		},
		Subjects: subjects,
		RoleRef: rbacv1.RoleRef{
			APIGroup: gvk.ClusterRole.Group,
			Kind:     gvk.ClusterRole.Kind,
			Name:     KueueAdminRoleName,
		},
	}

	err = rr.AddResources(crb)
	if err != nil {
		return fmt.Errorf("error creating kueue admin ClusterRoleBinding: %w", err)
	}

	log.Info("Successfully managed kueue admin role binding", "adminGroups", validAdminGroups)
	return nil
}

func manageDefaultKueueResourcesAction(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	kueueCRInstance, ok := rr.Instance.(*componentApi.Kueue)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.Kueue)", rr.Instance)
	}

	// Only proceed if Kueue is in Managed or Unmanaged state.
	if kueueCRInstance.Spec.ManagementState == operatorv1.Removed {
		return nil
	}

	// In Unmanaged case create RHBoK Kueue CR 'default'.
	if kueueCRInstance.Spec.ManagementState == operatorv1.Unmanaged {
		defaultKueueConfig, err := createKueueCR(ctx, rr)
		if err != nil {
			return err
		}

		rr.Resources = append(rr.Resources, *defaultKueueConfig)
	}

	// Generate default ClusterQueue.
	clusterQueue := createDefaultClusterQueue(kueueCRInstance.Spec.DefaultClusterQueueName)
	rr.Resources = append(rr.Resources, *clusterQueue)

	// Get all managed namespaces (i.e. the one opterd in with the addition of the proper labels).
	managedNamespaces, err := getManagedNamespaces(ctx, rr.Client)
	if err != nil {
		return fmt.Errorf("failed to get managed namespaces: %w", err)
	}
	// Update managed namespaces with missing labels
	err = ensureKueueLabelsOnManagedNamespaces(ctx, rr.Client, managedNamespaces)
	if err != nil {
		return fmt.Errorf("failed to add missing labels to managed namespaces: %v with error: %w", managedNamespaces, err)
	}

	// Generate LocalQueues in each managed namespaces.
	for _, ns := range managedNamespaces {
		localQueue := createDefaultLocalQueue(kueueCRInstance.Spec.DefaultLocalQueueName, kueueCRInstance.Spec.DefaultClusterQueueName, ns.Name)
		rr.Resources = append(rr.Resources, *localQueue)
	}

	return nil
}
