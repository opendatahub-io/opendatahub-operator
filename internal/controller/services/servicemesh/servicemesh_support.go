package servicemesh

import (
	"context"
	"errors"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

type AuthorinoDeploymentPredicate struct {
	predicate.Funcs
}

func (AuthorinoDeploymentPredicate) Update(e event.UpdateEvent) bool {
	if e.ObjectOld == nil || e.ObjectNew == nil {
		return false
	}

	oldDeployment, ok := e.ObjectOld.(*appsv1.Deployment)
	if !ok {
		return false
	}

	newDeployment, ok := e.ObjectNew.(*appsv1.Deployment)
	if !ok {
		return false
	}

	if newDeployment.GetName() != "authorino" {
		return false
	}

	return oldDeployment.Generation != newDeployment.Generation ||
		oldDeployment.Status.Replicas != newDeployment.Status.Replicas ||
		oldDeployment.Status.ReadyReplicas != newDeployment.Status.ReadyReplicas
}

func NewAuthorinoDeploymentPredicate() *AuthorinoDeploymentPredicate {
	return &AuthorinoDeploymentPredicate{}
}

type SMCPReadyPredicate struct {
	predicate.Funcs
}

func (SMCPReadyPredicate) Update(e event.UpdateEvent) bool {
	if e.ObjectOld == nil || e.ObjectNew == nil {
		return false
	}

	oldObj, ok := e.ObjectOld.(*unstructured.Unstructured)
	if !ok {
		return false
	}
	newObj, ok := e.ObjectNew.(*unstructured.Unstructured)
	if !ok {
		return false
	}

	if newObj.GetKind() != gvk.ServiceMeshControlPlane.Kind {
		return false
	}

	oldReady, _ := isSMCPReady(oldObj)
	newReady, _ := isSMCPReady(newObj)

	return oldReady != newReady
}

func NewSMCPReadyPredicate() *SMCPReadyPredicate {
	return &SMCPReadyPredicate{}
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
		return nil, fmt.Errorf("error obtaining Authorino namespace from ServiceMesh CR: %w", err)
	}

	return map[string]any{
		"AuthExtensionName": authorinoNamespace,
		"AuthNamespace":     authorinoNamespace,
		"AuthProviderName":  authProviderName,
		"ControlPlane":      sm.Spec.ControlPlane,
	}, nil
}

func isSMCPReady(smcp *unstructured.Unstructured) (bool, string) {
	_, found, err := unstructured.NestedFieldNoCopy(smcp.Object, "status", "readiness")
	if err != nil {
		return false, fmt.Sprintf("error checking SMCP readiness: %v", err)
	}

	if !found {
		return false, "SMCP readiness status not found, SMCP may be initializing"
	}

	conditions, found, err := unstructured.NestedSlice(smcp.Object, "status", "conditions")
	if err != nil {
		return false, fmt.Sprintf("error checking SMCP conditions: %v", err)
	}

	if !found {
		return false, "no SMCP conditions found, SMCP may be starting up"
	}

	var lastConditionMessage string

	for _, condition := range conditions {
		conditionMap, ok := condition.(map[string]interface{})
		if !ok {
			continue
		}

		condType, found := conditionMap["type"]
		if !found {
			continue
		}

		condStatus, found := conditionMap["status"]
		if !found {
			continue
		}

		if message, found := conditionMap["message"]; found {
			lastConditionMessage = fmt.Sprintf("%v", message)
		}

		if condType == "Ready" {
			if condStatus == "True" {
				return true, "SMCP Ready condition is True"
			}

			if lastConditionMessage != "" {
				return false, fmt.Sprintf("SMCP Ready condition is false: %s", lastConditionMessage)
			}
			return false, "SMCPReady condition is false"
		}
	}

	return false, "SMCP Ready condition not found, SMCP may be initializing"
}

func isAuthorinoReady(authorino *unstructured.Unstructured) (bool, error) {
	conditions, found, err := unstructured.NestedSlice(authorino.Object, "status", "conditions")
	if err != nil {
		return false, err
	}

	if !found {
		return false, errors.New("no Authorino conditions found, Authorino may be starting up")
	}

	for _, condition := range conditions {
		conditionMap, ok := condition.(map[string]interface{})
		if !ok {
			continue
		}
		condType, found := conditionMap["type"]
		if !found {
			continue
		}
		condStatus, found := conditionMap["status"]
		if !found {
			continue
		}

		if condType == "Ready" && condStatus == "True" {
			return true, nil
		}
	}

	return false, errors.New("no Authorino Ready condition found, Authorino may be initializing")
}

func getAutorinoResource(ctx context.Context, rr *odhtypes.ReconciliationRequest) (*unstructured.Unstructured, error) {
	authorinoNamespace, err := getAuthorinoNamespace(rr)
	if err != nil {
		return nil, err
	}

	authorino := &unstructured.Unstructured{}
	authorino.SetGroupVersionKind(gvk.Authorino)
	err = rr.Client.Get(ctx, client.ObjectKey{
		Name:      authProviderName,
		Namespace: authorinoNamespace,
	}, authorino)

	return authorino, err
}

func createAuthorinoDeploymentPatch(name string, namespace string) *unstructured.Unstructured {
	authorinoDeploymentPatch := &unstructured.Unstructured{}

	authorinoDeploymentPatch.Object = map[string]interface{}{
		"apiVersion": gvk.Deployment.GroupVersion().String(),
		"kind":       gvk.Deployment.Kind,
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": namespace,
		},
		"spec": map[string]interface{}{
			"template": map[string]interface{}{
				"metadata": map[string]interface{}{
					"labels": map[string]interface{}{
						"sidecar.istio.io/inject": "true",
					},
				},
			},
		},
	}

	return authorinoDeploymentPatch
}
