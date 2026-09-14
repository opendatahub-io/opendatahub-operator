//go:build !nowebhook

package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	admissionv1 "k8s.io/api/admission/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	webhookutils "github.com/opendatahub-io/opendatahub-operator/v2/pkg/webhook"
)

const (
	unsupportedCertificateTypeMessage = "certificate.type OpenshiftDefaultIngress is not supported on Kubernetes clusters; use SelfSigned or Provided"
	unsupportedIngressModeMessage     = "ingressMode OcpRoute is not supported on Kubernetes clusters; use LoadBalancer"
)

//+kubebuilder:webhook:path=/validate-gatewayconfig,matchPolicy=Exact,mutating=false,failurePolicy=fail,sideEffects=None,groups=services.platform.opendatahub.io,resources=gatewayconfigs,verbs=create;update,versions=v1alpha1,name=gatewayconfig-validator.opendatahub.io,admissionReviewVersions=v1
//nolint:lll

// Validator implements admission validation for GatewayConfig resources.
type Validator struct {
	Decoder admission.Decoder
	Name    string
}

var _ admission.Handler = &Validator{}

// SetupWithManager registers the GatewayConfig validating webhook.
func (v *Validator) SetupWithManager(mgr ctrl.Manager) error {
	mgr.GetWebhookServer().Register("/validate-gatewayconfig", &webhook.Admission{
		Handler:        v,
		LogConstructor: webhookutils.NewWebhookLogConstructor(v.Name),
	})
	return nil
}

// Handle rejects OpenShift-only GatewayConfig values on vanilla Kubernetes.
func (v *Validator) Handle(_ context.Context, req admission.Request) admission.Response {
	if v.Decoder == nil {
		return admission.Errored(http.StatusInternalServerError, errors.New("webhook decoder not initialized"))
	}

	if req.Kind.Group != gvk.GatewayConfig.Group || req.Kind.Version != gvk.GatewayConfig.Version || req.Kind.Kind != gvk.GatewayConfig.Kind {
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("unexpected gvk: %v; expecting: %v", req.Kind, gvk.GatewayConfig))
	}

	if req.Operation != admissionv1.Create && req.Operation != admissionv1.Update {
		return admission.Allowed(fmt.Sprintf("Operation %s on %s allowed", req.Operation, req.Kind.Kind))
	}

	gatewayConfig := &serviceApi.GatewayConfig{}
	if err := v.Decoder.DecodeRaw(req.Object, gatewayConfig); err != nil {
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("failed to decode GatewayConfig: %w", err))
	}

	if cluster.GetClusterInfo().Type != cluster.ClusterTypeKubernetes {
		return admission.Allowed("GatewayConfig values are supported on this platform")
	}

	var messages []string
	if gatewayConfig.Spec.Certificate != nil && gatewayConfig.Spec.Certificate.Type == infrav1.OpenshiftDefaultIngress {
		messages = append(messages, unsupportedCertificateTypeMessage)
	}
	if gatewayConfig.Spec.IngressMode == serviceApi.IngressModeOcpRoute {
		messages = append(messages, unsupportedIngressModeMessage)
	}
	if len(messages) > 0 {
		return admission.Denied(strings.Join(messages, "; "))
	}

	return admission.Allowed("GatewayConfig values are valid on Kubernetes")
}
