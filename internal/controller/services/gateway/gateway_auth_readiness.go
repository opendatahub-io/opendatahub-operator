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
	status *serviceApi.AdditionalIngressStatus,
) bool {
	previous := conditions.FindStatusCondition(status, serviceApi.AdditionalIngressAuthenticationReadyConditionType)
	if previous != nil && previous.ObservedGeneration == config.Generation && previous.Reason == additionalIngressReasonAuthenticationDisabled {
		return false
	}
	if previous != nil && previous.ObservedGeneration == config.Generation && previous.Status == metav1.ConditionFalse &&
		(previous.Reason == additionalIngressReasonInvalidAuthenticationConfig || previous.Reason == additionalIngressReasonOwnershipConflict ||
			previous.Reason == additionalIngressReasonAuthenticationUnavailable) {
		return previous.Reason != additionalIngressReasonInvalidAuthenticationConfig
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
	setAdditionalIngressCondition(status, config.Generation, serviceApi.AdditionalIngressAuthenticationReadyConditionType, state, reason, message)
	return state != metav1.ConditionTrue
}
