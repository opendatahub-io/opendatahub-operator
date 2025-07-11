package servicemesh

import (
	"context"
	"errors"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func checkServiceMeshOperator(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	if smOperatorFound, err := cluster.SubscriptionExists(ctx, rr.Client, serviceMeshOperatorName); !smOperatorFound || err != nil {
		return fmt.Errorf(
			"failed to find the pre-requisite operator subscription %q, please ensure operator is installed. %w",
			serviceMeshOperatorName,
			fmt.Errorf("missing operator %q", serviceMeshOperatorName),
		)
	}

	if err := cluster.CustomResourceDefinitionExists(ctx, rr.Client, gvk.ServiceMeshControlPlane.GroupKind()); err != nil {
		return fmt.Errorf("failed to find the Service Mesh Control Plane CRD, please ensure Service Mesh Operator is installed. %w", err)
	}

	// Extra check if SMCP validation service is running.
	validationService := &corev1.Service{}
	if err := rr.Client.Get(ctx, client.ObjectKey{
		Name:      "istio-operator-service",
		Namespace: "openshift-operators",
	}, validationService); err != nil {
		if k8serr.IsNotFound(err) {
			return fmt.Errorf("failed to find the Service Mesh VWC service, please ensure Service Mesh Operator is running. %w", err)
		}
		return fmt.Errorf("failed to find the Service Mesh VWC service. %w", err)
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
		return errors.New("error creating SMCP namespace")
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
		return errors.New("error obtaining Authorino namespace from ServiceMesh CR")
	}
	if _, err := cluster.CreateNamespace(
		ctx,
		rr.Client,
		authorinoNamespace,
		cluster.OwnedBy(sm, rr.Client.Scheme()),
		cluster.WithLabels(labels.ODH.OwnedNamespace, "true"),
	); err != nil {
		return errors.New("error creating Authorino namespace")
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

func getAuthorinoNamespace(rr *odhtypes.ReconciliationRequest) (string, error) {
	sm, ok := rr.Instance.(*serviceApi.ServiceMesh)
	if !ok {
		return "", fmt.Errorf("resource instance %v is not a serviceApi.ServiceMesh)", rr.Instance)
	}

	if len(strings.TrimSpace(sm.Spec.Auth.Namespace)) == 0 {
		// auth namespace not specified, use the following default:
		return rr.DSCI.Spec.ApplicationsNamespace + "-auth-provider", nil
	}

	return sm.Spec.Auth.Namespace, nil
}

func getTemplateData(_ context.Context, rr *odhtypes.ReconciliationRequest) (map[string]any, error) {
	sm, ok := rr.Instance.(*serviceApi.ServiceMesh)
	if !ok {
		return nil, fmt.Errorf("resource instance %v is not a serviceApi.ServiceMesh)", rr.Instance)
	}

	authorinoNamespace, err := getAuthorinoNamespace(rr)
	if err != nil {
		return nil, errors.New("error obtaining Authorino namespace from ServiceMesh CR")
	}

	return map[string]any{
		"AuthExtensionName": authorinoNamespace,
		"AuthNamespace":     authorinoNamespace,
		"AuthProviderName":  authProviderName,
		"ControlPlane":      sm.Spec.ControlPlane,
	}, nil
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
		return errors.New("error obtaining Authorino namespace from ServiceMesh CR")
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

func updateStatus(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	rr.Conditions.MarkTrue(
		status.CapabilityServiceMesh,
		conditions.WithReason(status.ConfiguredReason),
		conditions.WithMessage("ServiceMesh configured"),
	)

	authorinoOperatorFound, err := cluster.SubscriptionExists(ctx, rr.Client, authorinoOperatorName)
	if err != nil {
		return err
	}
	if authorinoOperatorFound {
		rr.Conditions.MarkTrue(
			status.CapabilityServiceMeshAuthorization,
			conditions.WithReason(status.ConfiguredReason),
			conditions.WithMessage("ServiceMesh authorization configured"),
		)
	}

	return nil
}
