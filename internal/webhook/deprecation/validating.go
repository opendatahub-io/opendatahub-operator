//go:build !nowebhook

package deprecation

import (
	"context"
	"fmt"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	webhookutils "github.com/opendatahub-io/opendatahub-operator/v2/pkg/webhook"
)

type TypeMap struct {
	DeprecatedGVK  schema.GroupVersionKind
	ReplacementGVK schema.GroupVersionKind
}

// TypeValidator is a generic webhook that denies CREATE and UPDATE operations
// on deprecated API resources and directs users to the replacement API.
type TypeValidator struct {
	TypeMap

	Decoder     admission.Decoder
	Name        string
	WebhookPath string
}

var _ admission.Handler = &TypeValidator{}

func (v *TypeValidator) SetupWithManager(mgr ctrl.Manager) error {
	hookServer := mgr.GetWebhookServer()
	hookServer.Register(v.WebhookPath, &webhook.Admission{
		Handler:        v,
		LogConstructor: webhookutils.NewWebhookLogConstructor(v.Name),
	})

	return nil
}

func (v *TypeValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	log := logf.FromContext(ctx)

	if !v.isExpectedKind(req.Kind) {
		err := fmt.Errorf("unexpected kind: %s", req.Kind)
		log.Error(err, "got wrong kind", "group", req.Kind.Group, "version", req.Kind.Version, "kind", req.Kind.Kind)
		return admission.Errored(http.StatusBadRequest, err)
	}

	var resp admission.Response

	switch req.Operation {
	case admissionv1.Create, admissionv1.Update:
		msg := fmt.Sprintf("%s/%s is not supported, please use %s/%s",
			v.DeprecatedGVK.Group,
			v.DeprecatedGVK.Kind,
			v.ReplacementGVK.Group,
			v.ReplacementGVK.Kind,
		)
		resp = admission.Denied(msg)
	default:
		resp = admission.Allowed(fmt.Sprintf("Operation %s on %s allowed", req.Operation, req.Kind))
	}

	return resp
}

func (v *TypeValidator) isExpectedKind(kind metav1.GroupVersionKind) bool {
	return kind.Group == v.DeprecatedGVK.Group &&
		kind.Version == v.DeprecatedGVK.Version &&
		kind.Kind == v.DeprecatedGVK.Kind
}
