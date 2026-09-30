/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package gateway

import (
	"context"
	"fmt"

	fwgc "github.com/opendatahub-io/odh-platform-utilities/framework/controller/actions/gc"
	configv1 "github.com/openshift/api/config/v1"
	corev1 "k8s.io/api/core/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/gc"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/template"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/handlers"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/precondition"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

const gatewayResourceApplyFailedReason = "GatewayResourceApplyFailed"

// gatewayCRDWatchPredicate matches CRD events that must re-trigger a GatewayConfig reconcile:
//
//   - the Dashboard CRD, which gates the dashboard redirect resources;
//   - the cert-manager Certificate CRD, which gates certificate handling and retries a
//     previously blocked XKS reconcile when cert-manager is installed after the operator.
func gatewayCRDWatchPredicate() predicate.Predicate {
	return predicate.Or(
		resources.CreatedOrUpdatedOrDeletedNamed(gvk.DashboardComponentCRDName),
		resources.CreatedOrUpdatedOrDeletedNamed(gvk.CertManagerCertificateCRDName),
	)
}

// Only changes to the cluster authentication type alter whether the proxy is desired.
func gatewayAuthenticationWatchPredicate() predicate.Predicate {
	isClusterAuthentication := func(obj client.Object) bool {
		return obj != nil && obj.GetName() == cluster.ClusterAuthenticationObj
	}
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return isClusterAuthentication(e.Object)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return isClusterAuthentication(e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			if !isClusterAuthentication(e.ObjectNew) {
				return false
			}
			oldAuth, oldOK := e.ObjectOld.(*unstructured.Unstructured)
			newAuth, newOK := e.ObjectNew.(*unstructured.Unstructured)
			if !oldOK || !newOK {
				return true
			}
			oldType, _, oldErr := unstructured.NestedString(oldAuth.Object, "spec", "type")
			newType, _, newErr := unstructured.NestedString(newAuth.Object, "spec", "type")
			return oldErr != nil || newErr != nil || oldType != newType
		},
	}
}

// The default GC predicate only sees GatewayConfig generation changes. Authentication/cluster
// can change the desired proxy resources without changing that generation.
func gatewayGCObjectPredicate(rr *odhtypes.ReconciliationRequest, obj unstructured.Unstructured) (bool, error) {
	defaultPredicate := fwgc.DefaultObjectPredicate(fwgc.DefaultAnnotationPrefix)
	if obj.GetName() == KubeAuthProxyName && obj.GetNamespace() == GetGatewayNamespace() &&
		(obj.GroupVersionKind() == gvk.Deployment || obj.GroupVersionKind() == gvk.NetworkPolicy) {
		for i := range rr.Resources {
			wanted := &rr.Resources[i]
			if wanted.GroupVersionKind() == obj.GroupVersionKind() &&
				wanted.GetNamespace() == obj.GetNamespace() && wanted.GetName() == obj.GetName() {
				return defaultPredicate(rr, obj)
			}
		}
		return true, nil
	}
	return defaultPredicate(rr, obj)
}

// gatewayDeploymentWatchPredicate also observes proxy availability, which changes in the
// Deployment status without changing its generation, labels, or annotations.
func gatewayDeploymentWatchPredicate() predicate.Predicate {
	return predicate.Or(
		predicates.DefaultPredicate,
		predicate.Funcs{
			UpdateFunc: func(e event.UpdateEvent) bool {
				oldDeployment, oldOK := e.ObjectOld.(*unstructured.Unstructured)
				newDeployment, newOK := e.ObjectNew.(*unstructured.Unstructured)
				if !oldOK || !newOK ||
					newDeployment.GetName() != KubeAuthProxyName ||
					newDeployment.GetNamespace() != GetGatewayNamespace() {
					return false
				}

				for _, field := range []string{"availableReplicas", "observedGeneration"} {
					oldValue, oldFound, oldErr := unstructured.NestedInt64(oldDeployment.Object, "status", field)
					newValue, newFound, newErr := unstructured.NestedInt64(newDeployment.Object, "status", field)
					if oldErr != nil || newErr != nil || oldFound != newFound || oldValue != newValue {
						return true
					}
				}
				return false
			},
		},
	)
}

func gatewayCertManagerPrecondition() precondition.PreCondition {
	return precondition.MonitorCRD(
		gvk.CertManagerCertificateCRDName,
		precondition.WithClusterTypes(cluster.ClusterTypeKubernetes),
		precondition.WithSkipFunc(func(_ context.Context, rr *odhtypes.ReconciliationRequest) (bool, error) {
			gatewayConfig, err := validateGatewayConfig(rr)
			if err != nil {
				return false, err
			}
			return gatewayConfig.Spec.Certificate != nil &&
				gatewayConfig.Spec.Certificate.Type == infrav1.Provided &&
				gatewayConfig.Spec.OIDC == nil, nil
		}),
		precondition.WithStopReconciliation(),
		precondition.WithMessage("cert-manager Certificate CRD is required for XKS certificate issuance"),
	)
}

// gatewayDeployAction records resource apply failures on GatewayConfigReady before
// returning the error to the reconciler. The later status action cannot run when
// deployment fails, so without this condition it can retain a stale Ready message.
func gatewayDeployAction(opts ...deploy.ActionOpts) actions.Fn {
	deployAction := deploy.NewAction(opts...)
	return func(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
		if err := deployAction(ctx, rr); err != nil {
			rr.Conditions.MarkFalse(
				ReadyConditionType,
				conditions.WithReason(gatewayResourceApplyFailedReason),
				conditions.WithMessage("Failed to apply GatewayConfig resources: %v", err),
			)
			return err
		}
		return nil
	}
}

func (h *ServiceHandler) NewReconciler(ctx context.Context, mgr ctrl.Manager) error {
	gw := reconciler.ReconcilerFor(mgr, &serviceApi.GatewayConfig{})
	// special for ROSA: auth is defined in day0 and OAuth not registered in apiserver
	if ok, err := cluster.IsIntegratedOAuth(ctx, mgr.GetAPIReader()); err == nil && ok {
		gw.OwnsGVK(gvk.OAuthClient)
	}

	gw.OwnsGVK(gvk.GatewayClass).
		OwnsGVK(gvk.KubernetesGateway, reconciler.WithPredicates(resources.GatewayStatusChanged())).
		OwnsGVK(gvk.Secret).
		OwnsGVK(gvk.ConfigMap).
		OwnsGVK(gvk.Service).
		OwnsGVK(gvk.NetworkPolicy).
		OwnsGVK(gvk.Deployment, reconciler.WithPredicates(gatewayDeploymentWatchPredicate())).
		OwnsGVK(gvk.HorizontalPodAutoscaler).
		OwnsGVK(gvk.HTTPRoute).
		OwnsGVK(gvk.Route, reconciler.Dynamic(reconciler.ClusterIsOpenShift())).
		OwnsGVK(gvk.ClusterRoleBinding).
		OwnsGVK(gvk.EnvoyFilter, reconciler.Dynamic(reconciler.CrdExists(gvk.EnvoyFilter))).
		OwnsGVK(gvk.DestinationRule, reconciler.Dynamic(reconciler.CrdExists(gvk.DestinationRule))).
		OwnsGVK(gvk.CertManagerCertificate, reconciler.Dynamic(reconciler.CrdExists(gvk.CertManagerCertificate))).
		Watches(
			&extv1.CustomResourceDefinition{},
			reconciler.WithEventHandler(
				handlers.ToNamed(serviceApi.GatewayConfigName)),
			reconciler.WithPredicates(gatewayCRDWatchPredicate()),
		).
		// Watch for certificate secrets (both OpenShift default ingress and provided).
		Watches(
			&corev1.Secret{},
			reconciler.WithEventHandler(handlers.ToNamed(serviceApi.GatewayConfigName)),
			reconciler.WithPredicates(
				resources.GatewayCertificateSecret(func(obj client.Object) bool {
					return cluster.IsGatewayCertificateSecret(ctx, mgr.GetClient(), obj, GetGatewayNamespace())
				}),
			),
		).
		// Watch for OIDC client secrets and provider CA secrets referenced by GatewayConfig
		// so that creating or updating these Secrets triggers re-reconciliation (Helm/GitOps
		// race condition: Secret may be created after the GatewayConfig CR).
		Watches(
			&corev1.Secret{},
			reconciler.WithEventHandler(handlers.ToNamed(serviceApi.GatewayConfigName)),
			reconciler.WithPredicates(
				resources.GatewayCertificateSecret(func(obj client.Object) bool {
					return IsGatewayReferencedSecret(ctx, mgr.GetClient(), obj, GetGatewayNamespace())
				}),
			),
		).
		// Reconcile when cert-manager creates, updates, or removes an XKS TLS Secret.
		Watches(
			&corev1.Secret{},
			reconciler.WithEventHandler(handlers.ToNamed(serviceApi.GatewayConfigName)),
			reconciler.WithPredicates(
				resources.GatewayCertificateSecret(func(obj client.Object) bool {
					return IsXKSCertManagerSecret(ctx, mgr.GetClient(), obj, GetGatewayNamespace())
				}),
			),
		).
		Watches(
			&gwapiv1.HTTPRoute{},
			reconciler.WithEventHandler(handlers.ToNamed(serviceApi.GatewayConfigName)),
			reconciler.WithPredicates(resources.HTTPRouteReferencesGateway(GetDefaultGatewayName(), GetGatewayNamespace())),
		).
		// Reconcile when Dashboard CR is created or deleted so dashboard redirect
		// resources are deployed or cleaned up accordingly.
		WatchesGVK(
			gvk.Dashboard,
			reconciler.Dynamic(reconciler.CrdExists(gvk.Dashboard)),
			reconciler.WithEventHandler(handlers.ToNamed(serviceApi.GatewayConfigName)),
			reconciler.WithPredicates(resources.CreatedOrUpdatedOrDeletedNamed(componentApi.DashboardInstanceName)),
		).
		// Reconcile when cluster APIServer TLS profile changes (kube-auth-proxy TLS args).
		WatchesGVK(
			gvk.OpenshiftAPIServer,
			reconciler.Dynamic(reconciler.ClusterIsOpenShift()),
			reconciler.WithEventHandler(handlers.ToNamed(serviceApi.GatewayConfigName)),
			reconciler.WithPredicates(resources.APIServerTLSSecurityProfileChanged()),
		).
		// Reconcile when the cluster authentication type changes, including to or from external auth.
		Watches(
			&configv1.Authentication{},
			reconciler.Dynamic(reconciler.ClusterIsOpenShift()),
			reconciler.WithEventHandler(handlers.ToNamed(serviceApi.GatewayConfigName)),
			reconciler.WithPredicates(gatewayAuthenticationWatchPredicate()),
		).
		WithReconcilerOpts(reconciler.WithPreConditions([]precondition.PreCondition{
			gatewayCertManagerPrecondition(),
		})).
		WithAction(syncAdditionalIngressStatus).
		WithAction(createGatewayInfrastructure).
		WithAction(createKubeAuthProxyInfrastructure). //  include destinationrule
		WithAction(createEnvoyFilter).
		WithAction(createNetworkPolicy).
		WithAction(createOCPRoutes).
		WithAction(createDashboardRedirectsAction).
		WithAction(template.NewAction(
			template.WithDataFn(getTemplateData),
		)).
		WithAction(gatewayDeployAction(
			deploy.WithCache(),
		)).
		WithAction(syncGatewayConfigStatus).
		WithAction(gc.NewAction(gc.WithObjectPredicate(gatewayGCObjectPredicate))).
		WithConditions(ReadyConditionType)

	if _, err := gw.Build(ctx); err != nil {
		return fmt.Errorf("could not create the GatewayConfig controller: %w", err)
	}
	return nil
}
