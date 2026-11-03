package gateway

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	controllerstatus "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

const (
	additionalIngressReasonAuthenticationDisabled      = "AuthenticationDisabled"
	additionalIngressReasonAuthenticationBlocked       = "AuthenticationBlocked"
	additionalIngressReasonAuthenticationUnavailable   = "AuthenticationUnavailable"
	additionalIngressReasonInvalidAuthenticationConfig = "InvalidAuthenticationConfig"
)

// Preserve the default Gateway's platform-specific authentication checks.
func checkGatewayAuthentication(ctx context.Context, cli client.Client, target authProxyTarget) (string, error) {
	if cluster.GetClusterInfo().Type != cluster.ClusterTypeKubernetes || target.OIDC == nil {
		return "", nil
	}
	message, err := checkCertManagerCertificate(ctx, cli, target.TLSName)
	if err != nil || message != "" {
		return message, err
	}
	return checkKubeAuthProxyDeployment(ctx, cli, target.ProxyName)
}

func syncAdditionalIngressAuthReadiness(
	ctx context.Context,
	rr *odhtypes.ReconciliationRequest,
	config *serviceApi.GatewayConfig,
	ingress serviceApi.AdditionalIngress,
) bool {
	result := gatewayAuthState(rr)[ingress.Name]
	if result.Condition.Reason == additionalIngressReasonAuthenticationDisabled ||
		result.Condition.Reason == additionalIngressReasonAuthenticationBlocked ||
		result.Condition.Reason == additionalIngressReasonInvalidAuthenticationConfig {
		return false
	}
	if result.failedPreparation() {
		return true
	}
	state, reason, message := metav1.ConditionTrue, controllerstatus.ReadyReason, "Authentication resources are ready for "+ingress.Name
	target := additionalAuthProxyTarget(ingress, config)
	pending, err := checkKubeAuthProxyDeployment(ctx, rr.Client, target.ProxyName)
	switch {
	case err != nil:
		state, reason, message = metav1.ConditionUnknown, additionalIngressReasonStatusReadFailed, "Authentication readiness could not be read"
	case pending != "":
		state, reason, message = metav1.ConditionFalse, controllerstatus.NotReadyReason, pending
	}
	recordAuthResult(rr, target, state, reason, message)
	return state != metav1.ConditionTrue
}

// Publish current results at aggregation time, avoiding temporary Pending -> Ready
// transitions in successful passes. The post-status hook also publishes on errors.
func publishAdditionalIngressAuthConditions(rr *odhtypes.ReconciliationRequest, config *serviceApi.GatewayConfig) {
	for _, result := range gatewayAuthState(rr) {
		if status := additionalIngressStatusByName(config, result.Target.IngressName); status != nil {
			conditions.SetStatusCondition(status, result.Condition)
		}
	}
}
