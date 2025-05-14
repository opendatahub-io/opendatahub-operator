package servicemesh

import (
	"context"
	"errors"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	featuresv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/features/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

func checkPreconditions(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	rr.Conditions.MarkUnknown(status.CapabilityServiceMesh)
	rr.Conditions.MarkUnknown(status.CapabilityServiceMeshAuthorization)

	// ensure ServiceMesh v2 operator is installed as pre-requisite
	if err := checkServiceMeshOperator(ctx, rr); err != nil {
		rr.Conditions.MarkFalse(
			status.CapabilityServiceMesh,
			conditions.WithReason(status.MissingOperatorReason),
			conditions.WithMessage(
				"OpenShift ServiceMesh v2 operator not found / not setup properly on the cluster, cannot setup ServiceMesh Authorization",
			),
		)
		rr.Conditions.MarkFalse(
			status.CapabilityServiceMeshAuthorization,
			conditions.WithReason(status.MissingOperatorReason),
			conditions.WithMessage(
				"OpenShift ServiceMesh v2 operator not found / not setup properly on the cluster, cannot setup ServiceMesh Authorization",
			),
		)

		return errors.New("OpenShift ServiceMesh v2 operator not found / not setup properly on the cluster, failed to setup ServiceMesh v2 resources")
	}

	return nil
}

func createControlPlaneNamespace(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	sm, ok := rr.Instance.(*serviceApi.ServiceMesh)
	if !ok {
		return fmt.Errorf("resource instance %v is not a serviceApi.ServiceMesh)", rr.Instance)
	}

	// ensure SMCP namespace exists
	if _, err := cluster.CreateNamespace(ctx, rr.Client, sm.Spec.ControlPlane.Namespace); err != nil {
		return fmt.Errorf("failed to create SMCP namespace %s: %w", sm.Spec.ControlPlane.Namespace, err)
	}

	return nil
}

func initializeServiceMesh(_ context.Context, rr *odhtypes.ReconciliationRequest) error {
	rr.Templates = append(
		rr.Templates,
		odhtypes.TemplateInfo{
			FS:   resourcesFS,
			Path: serviceMeshControlPlaneTemplate,
		},
	)

	return nil
}

func initializeServiceMeshMetricsCollection(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	log := logf.FromContext(ctx)

	sm, ok := rr.Instance.(*serviceApi.ServiceMesh)
	if !ok {
		return fmt.Errorf("resource instance %v is not a serviceApi.ServiceMesh)", rr.Instance)
	}

	if sm.Spec.ControlPlane.MetricsCollection != "Istio" {
		log.Info("MetricsCollection not set to Istio, skipping ServiceMesh metrics collection configuration")
		return nil
	}

	rr.Templates = append(
		rr.Templates,
		odhtypes.TemplateInfo{
			FS:   resourcesFS,
			Path: podMonitorTemplate,
		},
		odhtypes.TemplateInfo{
			FS:   resourcesFS,
			Path: serviceMonitorTemplate,
		},
	)

	return nil
}

func initializeAuthorino(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	log := logf.FromContext(ctx)

	sm, ok := rr.Instance.(*serviceApi.ServiceMesh)
	if !ok {
		return fmt.Errorf("resource instance %v is not a serviceApi.ServiceMesh)", rr.Instance)
	}

	// ensure Authorino operator is installed as pre-requisite
	authorinoOperatorFound, err := cluster.SubscriptionExists(ctx, rr.Client, authorinoOperatorName)
	if err != nil {
		return err
	}
	if !authorinoOperatorFound {
		log.Info("Authorino operator not found on the cluster, skipping authorization capability")

		rr.Conditions.MarkFalse(
			status.CapabilityServiceMeshAuthorization,
			conditions.WithReason(status.MissingOperatorReason),
			conditions.WithMessage(
				"Authorino operator is not installed on the cluster, skipping authorization capability",
			),
		)

		return nil
	}

	// create authorino namespace if it does not exist
	authorinoNamespace, err := getAuthorinoNamespace(rr)
	if err != nil {
		return fmt.Errorf("failed to obtain Authorino namespace from ServiceMesh CR: %w", err)
	}
	if _, err := cluster.CreateNamespace(
		ctx,
		rr.Client,
		authorinoNamespace,
		cluster.OwnedBy(sm, rr.Client.Scheme()),
		cluster.WithLabels(labels.ODH.OwnedNamespace, "true"),
	); err != nil {
		return fmt.Errorf("failed to create Authorino namespace %s: %w", authorinoNamespace, err)
	}

	rr.Templates = append(
		rr.Templates,
		odhtypes.TemplateInfo{
			FS:   resourcesFS,
			Path: authorinoTemplate,
		},
		odhtypes.TemplateInfo{
			FS:   resourcesFS,
			Path: authorinoServiceMeshMemberTemplate,
		},
		odhtypes.TemplateInfo{
			FS:   resourcesFS,
			Path: authorinoDeploymentInjectionTemplate,
		},
		odhtypes.TemplateInfo{
			FS:   resourcesFS,
			Path: authorinoServiceMeshControlPlaneTemplate,
		},
	)

	return nil
}

func updateMeshRefsConfigMap(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	sm, ok := rr.Instance.(*serviceApi.ServiceMesh)
	if !ok {
		return fmt.Errorf("resource instance %v is not a serviceApi.ServiceMesh)", rr.Instance)
	}

	data := map[string]string{
		"CONTROL_PLANE_NAME": sm.Spec.ControlPlane.Name,
		"MESH_NAMESPACE":     sm.Spec.ControlPlane.Namespace,
	}

	meshRefsConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      meshRefsConfigMapName,
			Namespace: rr.DSCI.Spec.ApplicationsNamespace,
		},
		Data: data,
	}
	if err := controllerutil.SetControllerReference(sm, meshRefsConfigMap, rr.Client.Scheme()); err != nil {
		return fmt.Errorf("error setting owner reference to ConfigMap: %s", meshRefsConfigMapName)
	}

	if err := rr.AddResources(meshRefsConfigMap); err != nil {
		return fmt.Errorf("error adding resource (ConfigMap): %s", meshRefsConfigMapName)
	}

	return nil
}

func updateAuthRefsConfigMap(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	sm, ok := rr.Instance.(*serviceApi.ServiceMesh)
	if !ok {
		return fmt.Errorf("resource instance %v is not a serviceApi.ServiceMesh)", rr.Instance)
	}

	audiences := sm.Spec.Auth.Audiences
	audiencesList := ""
	if audiences != nil && len(*audiences) > 0 {
		audiencesList = strings.Join(*audiences, ",")
	}

	authorinoNamespace, err := getAuthorinoNamespace(rr)
	if err != nil {
		return fmt.Errorf("failed to obtain Authorino namespace from ServiceMesh CR: %w", err)
	}

	data := map[string]string{
		"AUTH_AUDIENCE":   audiencesList,
		"AUTH_PROVIDER":   authProviderName,
		"AUTH_NAMESPACE":  authorinoNamespace,
		"AUTHORINO_LABEL": authorinoLabel,
	}

	authRefsConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      authRefsConfigMapName,
			Namespace: rr.DSCI.Spec.ApplicationsNamespace,
		},
		Data: data,
	}
	if err := controllerutil.SetControllerReference(sm, authRefsConfigMap, rr.Client.Scheme()); err != nil {
		return fmt.Errorf("error setting owner reference to ConfigMap: %s", authRefsConfigMapName)
	}

	if err := rr.AddResources(authRefsConfigMap); err != nil {
		return fmt.Errorf("error adding resource (ConfigMap): %s", authRefsConfigMapName)
	}

	return nil
}

func deleteFeatureTrackers(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	ftNames := []string{
		rr.DSCI.Spec.ApplicationsNamespace + "-mesh-shared-configmap",
		rr.DSCI.Spec.ApplicationsNamespace + "-mesh-control-plane-creation",
		rr.DSCI.Spec.ApplicationsNamespace + "-mesh-metrics-collection",
		rr.DSCI.Spec.ApplicationsNamespace + "-enable-proxy-injection-in-authorino-deployment",
		rr.DSCI.Spec.ApplicationsNamespace + "-mesh-control-plane-external-authz",
	}

	for _, n := range ftNames {
		ft := featuresv1.FeatureTracker{}
		err := rr.Client.Get(ctx, client.ObjectKey{Name: n}, &ft)
		if k8serr.IsNotFound(err) {
			continue
		} else if err != nil {
			return fmt.Errorf("failed to lookup FeatureTracker %s: %w", ft.GetName(), err)
		}

		err = rr.Client.Delete(ctx, &ft, client.PropagationPolicy(metav1.DeletePropagationForeground))
		if k8serr.IsNotFound(err) {
			continue
		} else if err != nil {
			return fmt.Errorf("failed to delete FeatureTracker %s: %w", ft.GetName(), err)
		}
	}

	return nil
}

func checkSMCPReadiness(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	sm, ok := rr.Instance.(*serviceApi.ServiceMesh)
	if !ok {
		return fmt.Errorf("resource instance %v is not a serviceApi.ServiceMesh)", rr.Instance)
	}

	smcp := &unstructured.Unstructured{}
	smcp.SetGroupVersionKind(gvk.ServiceMeshControlPlane)
	err := rr.Client.Get(ctx, client.ObjectKey{
		Name:      sm.Spec.ControlPlane.Name,
		Namespace: sm.Spec.ControlPlane.Namespace,
	}, smcp)

	if err != nil && !k8serr.IsNotFound(err) {
		return fmt.Errorf("failed to get ServiceMeshControlPlane: %w", err)
	}

	if k8serr.IsNotFound(err) {
		rr.Conditions.MarkFalse(
			status.CapabilityServiceMesh,
			conditions.WithReason(status.NotReadyReason),
			conditions.WithMessage("ServiceMeshControlPlane not found, SMCP may be initializing: %v", err),
		)
		return nil
	}

	ready, message := isSMCPReady(smcp)
	if ready {
		rr.Conditions.MarkTrue(
			status.CapabilityServiceMesh,
			conditions.WithReason(status.ReadyReason),
			conditions.WithMessage("ServiceMeshControlPlane is ready"),
		)
	} else {
		rr.Conditions.MarkFalse(
			status.CapabilityServiceMesh,
			conditions.WithReason(status.NotReadyReason),
			conditions.WithMessage("ServiceMeshControlPlane is not ready: %s", message),
		)
	}

	return nil
}

func checkAuthorinoReadiness(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	authorinoOperatorFound, err := cluster.SubscriptionExists(ctx, rr.Client, authorinoOperatorName)
	if err != nil {
		return err
	}

	if !authorinoOperatorFound {
		rr.Conditions.MarkFalse(
			status.CapabilityServiceMeshAuthorization,
			conditions.WithReason(status.MissingOperatorReason),
			conditions.WithMessage("Authorino operator is not installed"),
		)
		return nil
	}

	authorinoNamespace, err := getAuthorinoNamespace(rr)
	if err != nil {
		return err
	}

	deployment := &appsv1.Deployment{}
	err = rr.Client.Get(ctx, client.ObjectKey{
		Name:      authProviderName,
		Namespace: authorinoNamespace,
	}, deployment)

	if err != nil {
		rr.Conditions.MarkFalse(
			status.CapabilityServiceMeshAuthorization,
			conditions.WithReason(status.NotReadyReason),
			conditions.WithMessage("Authorino deployment not found: %v", err),
		)
		return nil
	}

	if deployment.Status.ReadyReplicas == deployment.Status.Replicas && deployment.Status.Replicas > 0 {
		rr.Conditions.MarkTrue(
			status.CapabilityServiceMeshAuthorization,
			conditions.WithReason(status.ReadyReason),
			conditions.WithMessage("Authorino deployment is ready"),
		)
	} else {
		rr.Conditions.MarkFalse(
			status.CapabilityServiceMeshAuthorization,
			conditions.WithReason(status.NotReadyReason),
			conditions.WithMessage("Authorino deployment not ready: %d/%d replicas ready",
				deployment.Status.ReadyReplicas, deployment.Status.Replicas),
		)
	}

	return nil
}

func cleanupSMCP(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	sm, ok := rr.Instance.(*serviceApi.ServiceMesh)
	if !ok {
		return fmt.Errorf("resource instance %v is not a serviceApi.ServiceMesh)", rr.Instance)
	}

	smcp := &unstructured.Unstructured{}
	smcp.SetGroupVersionKind(gvk.ServiceMeshControlPlane)
	err := rr.Client.Get(ctx, client.ObjectKey{
		Name:      sm.Spec.ControlPlane.Name,
		Namespace: sm.Spec.ControlPlane.Namespace,
	}, smcp)

	if k8serr.IsNotFound(err) {
		// SMCP not found, skipping deletion
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to get ServiceMeshControlPlane: %w", err)
	}

	// ensure that the SMCP instance being deleted was created by ODH operator (ServiceMesh controller)
	// this is determined based on the presence of platform label
	if resources.HasLabel(smcp, labels.PlatformPartOf, serviceApi.ServiceMeshServiceName) {
		if err := rr.Client.Delete(ctx, smcp); err != nil && !k8serr.IsNotFound(err) {
			return fmt.Errorf("failed to delete ServiceMeshControlPlane: %w", err)
		}
	}

	return nil
}
