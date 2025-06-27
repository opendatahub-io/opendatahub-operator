package auth_test

import (
	"context"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/auth"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/envtestutil"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

// TestAuth_ValidatingWebhook exercises the validating webhook logic for Auth resources.
// It verifies that invalid groups in AdminGroups and AllowedGroups are properly rejected
// using table-driven tests and a fake client.
func TestAuth_ValidatingWebhook(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	ctx := context.Background()
	sch, err := scheme.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	ns := "test-ns"

	cases := []struct {
		name         string
		existingObjs []client.Object
		req          admission.Request
		allowed      bool
		errorMessage string
	}{
		{
			name:         "Allows creation with valid groups",
			existingObjs: nil,
			req: envtestutil.NewAdmissionRequest(
				t,
				admissionv1.Create,
				envtestutil.NewAuth("test-valid", ns, []string{"valid-admin-group"}, []string{"valid-allowed-group"}),
				gvk.Auth,
				metav1.GroupVersionResource{
					Group:    gvk.Auth.Group,
					Version:  gvk.Auth.Version,
					Resource: "auths",
				},
			),
			allowed: true,
		},
		{
			name:         "Allows update with valid groups",
			existingObjs: nil,
			req: envtestutil.NewAdmissionRequest(
				t,
				admissionv1.Update,
				envtestutil.NewAuth("test-valid", ns, []string{"updated-admin-group"}, []string{"updated-allowed-group"}),
				gvk.Auth,
				metav1.GroupVersionResource{
					Group:    gvk.Auth.Group,
					Version:  gvk.Auth.Version,
					Resource: "auths",
				},
			),
			allowed: true,
		},
		{
			name:         "Allows delete operation (no validation)",
			existingObjs: nil,
			req: envtestutil.NewAdmissionRequest(
				t,
				admissionv1.Delete,
				envtestutil.NewAuth("test-delete", ns, []string{"any-admin-group"}, []string{"any-allowed-group"}),
				gvk.Auth,
				metav1.GroupVersionResource{
					Group:    gvk.Auth.Group,
					Version:  gvk.Auth.Version,
					Resource: "auths",
				},
			),
			allowed: true,
		},
		{
			name:         "Denies creation with system:authenticated in AdminGroups",
			existingObjs: nil,
			req: envtestutil.NewAdmissionRequest(
				t,
				admissionv1.Create,
				envtestutil.NewAuth("test-invalid-admin", ns, []string{"valid-admin-group", "system:authenticated"}, []string{"valid-allowed-group"}),
				gvk.Auth,
				metav1.GroupVersionResource{
					Group:    gvk.Auth.Group,
					Version:  gvk.Auth.Version,
					Resource: "auths",
				},
			),
			allowed:      false,
			errorMessage: "Invalid groups found in AdminGroups: 'system:authenticated'. Groups cannot be 'system:authenticated' or empty string",
		},
		{
			name:         "Denies creation with empty string in AdminGroups",
			existingObjs: nil,
			req: envtestutil.NewAdmissionRequest(
				t,
				admissionv1.Create,
				envtestutil.NewAuth("test-invalid-admin-empty", ns, []string{"valid-admin-group", ""}, []string{"valid-allowed-group"}),
				gvk.Auth,
				metav1.GroupVersionResource{
					Group:    gvk.Auth.Group,
					Version:  gvk.Auth.Version,
					Resource: "auths",
				},
			),
			allowed:      false,
			errorMessage: "Invalid groups found in AdminGroups: ''. Groups cannot be 'system:authenticated' or empty string",
		},
		{
			name:         "Allows creation with system:authenticated in AllowedGroups",
			existingObjs: nil,
			req: envtestutil.NewAdmissionRequest(
				t,
				admissionv1.Create,
				envtestutil.NewAuth("test-valid-allowed", ns, []string{"valid-admin-group"}, []string{"valid-allowed-group", "system:authenticated"}),
				gvk.Auth,
				metav1.GroupVersionResource{
					Group:    gvk.Auth.Group,
					Version:  gvk.Auth.Version,
					Resource: "auths",
				},
			),
			allowed: true,
		},
		{
			name:         "Denies creation with empty string in AllowedGroups",
			existingObjs: nil,
			req: envtestutil.NewAdmissionRequest(
				t,
				admissionv1.Create,
				envtestutil.NewAuth("test-invalid-allowed-empty", ns, []string{"valid-admin-group"}, []string{"valid-allowed-group", ""}),
				gvk.Auth,
				metav1.GroupVersionResource{
					Group:    gvk.Auth.Group,
					Version:  gvk.Auth.Version,
					Resource: "auths",
				},
			),
			allowed:      false,
			errorMessage: "Invalid groups found in AllowedGroups: ''. Groups cannot be empty string",
		},
		{
			name:         "Denies creation with multiple invalid groups in AdminGroups",
			existingObjs: nil,
			req: envtestutil.NewAdmissionRequest(
				t,
				admissionv1.Create,
				envtestutil.NewAuth("test-multiple-invalid-admin", ns, []string{"valid-admin-group", "system:authenticated", ""}, []string{"valid-allowed-group"}),
				gvk.Auth,
				metav1.GroupVersionResource{
					Group:    gvk.Auth.Group,
					Version:  gvk.Auth.Version,
					Resource: "auths",
				},
			),
			allowed:      false,
			errorMessage: "Invalid groups found in AdminGroups: 'system:authenticated', ''. Groups cannot be 'system:authenticated' or empty string",
		},
		{
			name:         "Denies update with invalid groups",
			existingObjs: nil,
			req: envtestutil.NewAdmissionRequest(
				t,
				admissionv1.Update,
				envtestutil.NewAuth("test-invalid-update", ns, []string{"valid-admin-group"}, []string{"valid-allowed-group", ""}),
				gvk.Auth,
				metav1.GroupVersionResource{
					Group:    gvk.Auth.Group,
					Version:  gvk.Auth.Version,
					Resource: "auths",
				},
			),
			allowed:      false,
			errorMessage: "Invalid groups found in AllowedGroups: ''. Groups cannot be empty string",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(tc.existingObjs...).Build()
			decoder := admission.NewDecoder(sch)
			validator := &auth.Validator{
				Client:  cli,
				Name:    "test",
				Decoder: decoder,
			}
			resp := validator.Handle(ctx, tc.req)
			g.Expect(resp.Allowed).To(Equal(tc.allowed))
			if !tc.allowed {
				g.Expect(resp.Result.Message).ToNot(BeEmpty(), "Expected error message when request is denied")
				if tc.errorMessage != "" {
					g.Expect(resp.Result.Message).To(ContainSubstring(tc.errorMessage), "Expected specific error message")
				}
			}
		})
	}
}
