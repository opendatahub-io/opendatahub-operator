//go:build !nowebhook

package dashboard_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/onsi/gomega/types"
	"github.com/rs/xid"
	admissionv1 "k8s.io/api/admission/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/dashboard"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/deprecation"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/envtestutil"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"
	webhookutils "github.com/opendatahub-io/opendatahub-operator/v2/pkg/webhook"

	. "github.com/onsi/gomega"
)

func TestValidator_Unit(t *testing.T) {
	g := NewWithT(t)

	s, err := scheme.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	tmaps := []struct {
		name      string
		validator *deprecation.TypeValidator
	}{
		{
			name:      "AcceleratorProfile",
			validator: dashboard.NewAcceleratorProfileWebhook(s),
		},
		{
			name:      "HardwareProfile",
			validator: dashboard.NewHardwareProfileWebhook(s),
		},
	}

	testCases := []struct {
		name            string
		operation       admissionv1.Operation
		kindFunc        func(deprecatedKind metav1.GroupVersionKind) metav1.GroupVersionKind
		responseMatcher types.GomegaMatcher
	}{
		{
			name:      "Create - should deny",
			operation: admissionv1.Create,
			kindFunc: func(deprecatedKind metav1.GroupVersionKind) metav1.GroupVersionKind {
				return deprecatedKind
			},
			responseMatcher: And(
				HaveField("Allowed", BeFalse()),
				HaveField("Result.Code", Equal(int32(http.StatusForbidden))),
				HaveField("Result.Message", ContainSubstring("is not supported")),
			),
		},
		{
			name:      "Update - should deny",
			operation: admissionv1.Update,
			kindFunc: func(deprecatedKind metav1.GroupVersionKind) metav1.GroupVersionKind {
				return deprecatedKind
			},
			responseMatcher: And(
				HaveField("Allowed", BeFalse()),
				HaveField("Result.Code", Equal(int32(http.StatusForbidden))),
				HaveField("Result.Message", ContainSubstring("infrastructure.opendatahub.io/HardwareProfile")),
			),
		},
		{
			name:      "Delete - should allow",
			operation: admissionv1.Delete,
			kindFunc: func(deprecatedKind metav1.GroupVersionKind) metav1.GroupVersionKind {
				return deprecatedKind
			},
			responseMatcher: HaveField("Allowed", BeTrue()),
		},
		{
			name:      "Wrong kind - should error",
			operation: admissionv1.Create,
			kindFunc: func(_ metav1.GroupVersionKind) metav1.GroupVersionKind {
				return metav1.GroupVersionKind{Group: "other.io", Version: "v1", Kind: "Other"}
			},
			responseMatcher: And(
				HaveField("Allowed", BeFalse()),
				HaveField("Result.Code", Equal(int32(http.StatusBadRequest))),
				HaveField("Result.Message", ContainSubstring("unexpected kind")),
			),
		},
	}

	for _, tm := range tmaps {
		t.Run(tm.name, func(t *testing.T) {
			for _, tc := range testCases {
				t.Run(tc.name, func(t *testing.T) {
					g := NewWithT(t)

					req := admission.Request{
						AdmissionRequest: admissionv1.AdmissionRequest{
							Operation: tc.operation,
							Kind: tc.kindFunc(metav1.GroupVersionKind{
								Group:   tm.validator.DeprecatedGVK.Group,
								Version: tm.validator.DeprecatedGVK.Version,
								Kind:    tm.validator.DeprecatedGVK.Kind,
							}),
						},
					}

					g.Expect(
						tm.validator.Handle(context.Background(), req),
					).To(
						tc.responseMatcher,
					)
				})
			}
		})
	}
}

// bypassHandler wraps a handler and allows bypassing validation based on a custom function.
type bypassHandler struct {
	delegate   admission.Handler
	bypassFunc func(req admission.Request) bool
}

func (h *bypassHandler) Handle(ctx context.Context, req admission.Request) admission.Response {
	if h.bypassFunc != nil && h.bypassFunc(req) {
		return admission.Allowed("Bypass allowed for test resource")
	}
	return h.delegate.Handle(ctx, req)
}

// registerWebhookWithBypass registers a webhook with a bypass function for testing.
func registerWebhookWithBypass(
	mgr ctrl.Manager,
	validator *deprecation.TypeValidator,
	bypassFunc func(req admission.Request) bool,
) error {
	handler := &bypassHandler{
		delegate:   validator,
		bypassFunc: bypassFunc,
	}

	mgr.GetWebhookServer().Register(
		validator.WebhookPath,
		&webhook.Admission{
			Handler:        handler,
			LogConstructor: webhookutils.NewWebhookLogConstructor(validator.Name),
		},
	)

	return nil
}

func TestValidator_Integration(t *testing.T) {
	g := NewWithT(t)

	ctx, env, teardown := envtestutil.SetupEnvAndClientWithCRDs(
		t,
		[]envt.RegisterWebhooksFn{
			func(mgr ctrl.Manager) error {
				bypassFunc := func(req admission.Request) bool {
					return req.Operation == admissionv1.Create && strings.HasPrefix(req.Name, "test-bypass")
				}

				if err := registerWebhookWithBypass(
					mgr,
					dashboard.NewAcceleratorProfileWebhook(mgr.GetScheme()),
					bypassFunc,
				); err != nil {
					return err
				}

				if err := registerWebhookWithBypass(
					mgr,
					dashboard.NewHardwareProfileWebhook(mgr.GetScheme()),
					bypassFunc,
				); err != nil {
					return err
				}

				return nil
			},
		},
		30*time.Second,
		envtestutil.WithDashboardHardwareProfile(),
		envtestutil.WithDashboardAcceleratorProfile(),
	)

	t.Cleanup(teardown)

	ns := "test-" + xid.New().String()
	g.Expect(env.Client().Create(ctx, envtestutil.NewNamespace(ns, nil))).To(Succeed())

	tmaps := []deprecation.TypeMap{
		{DeprecatedGVK: gvk.DashboardAcceleratorProfile, ReplacementGVK: gvk.HardwareProfile},
		{DeprecatedGVK: gvk.DashboardHardwareProfile, ReplacementGVK: gvk.HardwareProfile},
	}

	for _, item := range tmaps {
		msg := fmt.Sprintf("%s/%s is not supported, please use %s/%s",
			item.DeprecatedGVK.Group,
			item.DeprecatedGVK.Kind,
			item.ReplacementGVK.Group,
			item.ReplacementGVK.Kind,
		)

		t.Run("Should deny creation of "+item.DeprecatedGVK.Kind, func(t *testing.T) {
			g := NewWithT(t)

			hp := resources.GvkToUnstructured(item.DeprecatedGVK)
			hp.SetName("test-profile-" + xid.New().String())
			hp.SetNamespace(ns)

			err := env.Client().Create(ctx, hp)

			g.Expect(err).To(HaveOccurred())

			statusErr := &k8serr.StatusError{}
			ok := errors.As(err, &statusErr)
			g.Expect(ok).To(BeTrue(), "Expected error to be of type StatusError")

			g.Expect(statusErr.Status().Code).To(Equal(int32(http.StatusForbidden)))
			g.Expect(statusErr.Status().Message).To(ContainSubstring(msg))
		})

		t.Run("Should deny update of "+item.DeprecatedGVK.Kind, func(t *testing.T) {
			g := NewWithT(t)

			ap := resources.GvkToUnstructured(item.DeprecatedGVK)
			ap.SetName("test-bypass-" + xid.New().String())
			ap.SetNamespace(ns)

			g.Expect(env.Client().Create(ctx, ap)).To(Succeed())

			err := env.Client().Update(ctx, ap)

			g.Expect(err).To(HaveOccurred())

			statusErr := &k8serr.StatusError{}
			ok := errors.As(err, &statusErr)
			g.Expect(ok).To(BeTrue(), "Expected error to be of type StatusError")

			g.Expect(statusErr.Status().Code).To(Equal(int32(http.StatusForbidden)))
			g.Expect(statusErr.Status().Message).To(ContainSubstring(msg))
		})

		t.Run("Should allow deletion of "+item.DeprecatedGVK.Kind, func(t *testing.T) {
			g := NewWithT(t)

			ap := resources.GvkToUnstructured(item.DeprecatedGVK)
			ap.SetName("test-bypass-" + xid.New().String())
			ap.SetNamespace(ns)

			g.Expect(env.Client().Create(ctx, ap)).To(Succeed())

			err := env.Client().Delete(ctx, ap)

			g.Expect(err).ToNot(HaveOccurred())
		})
	}
}
