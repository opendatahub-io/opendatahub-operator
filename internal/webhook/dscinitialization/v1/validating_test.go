package v1_test

import (
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	v1webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/envtestutil"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

// TestDSCInitializationV1_ValidatingWebhook exercises the validating webhook logic for DSCInitialization v1 resources.
// It verifies singleton enforcement and deletion restrictions using table-driven tests and a fake client.
func TestDSCInitializationV1_ValidatingWebhook(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	ctx := t.Context()
	sch, err := scheme.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	ns := "test-ns"

	cases := []struct {
		name         string
		existingObjs []client.Object
		req          admission.Request
		allowed      bool
	}{
		{
			name:         "Allows creation if none exist",
			existingObjs: nil,
			req: envtestutil.NewAdmissionRequest(
				t,
				admissionv1.Create,
				envtestutil.NewDSCIV1("test-create", ns),
				gvk.DSCInitialization,
				metav1.GroupVersionResource{
					Group:    gvk.DSCInitialization.Group,
					Version:  "v1",
					Resource: "dscinitializations",
				},
			),
			allowed: true,
		},
		{
			name: "Denies creation if one already exists",
			existingObjs: []client.Object{
				envtestutil.NewDSCIV1("existing", ns),
			},
			req: envtestutil.NewAdmissionRequest(
				t,
				admissionv1.Create,
				envtestutil.NewDSCIV1("test-create", ns),
				gvk.DSCInitialization,
				metav1.GroupVersionResource{
					Group:    gvk.DSCInitialization.Group,
					Version:  "v1",
					Resource: "dscinitializations",
				},
			),
			allowed: false,
		},
		{
			name: "Denies deletion if DSC exists",
			existingObjs: []client.Object{
				&dscv1.DataScienceCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "dsc-1",
						Namespace: ns,
					},
				},
				envtestutil.NewDSCIV1("dsci-1", ns),
			},
			req: envtestutil.NewAdmissionRequest(
				t,
				admissionv1.Delete,
				envtestutil.NewDSCIV1("dsci-1", ns),
				gvk.DSCInitialization,
				metav1.GroupVersionResource{
					Group:    gvk.DSCInitialization.Group,
					Version:  "v1",
					Resource: "dscinitializations",
				},
			),
			allowed: false,
		},
		{
			name: "Allows deletion if no DSC exists",
			existingObjs: []client.Object{
				envtestutil.NewDSCIV1("dsci-1", ns),
			},
			req: envtestutil.NewAdmissionRequest(
				t,
				admissionv1.Delete,
				envtestutil.NewDSCIV1("dsci-1", ns),
				gvk.DSCInitialization,
				metav1.GroupVersionResource{
					Group:    gvk.DSCInitialization.Group,
					Version:  "v1",
					Resource: "dscinitializations",
				},
			),
			allowed: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(tc.existingObjs...).Build()
			validator := &v1webhook.Validator{
				Client: cli,
				Name:   "test-v1",
			}
			resp := validator.Handle(ctx, tc.req)
			g.Expect(resp.Allowed).To(Equal(tc.allowed))
			if !tc.allowed {
				g.Expect(resp.Result.Message).ToNot(BeEmpty(), "Expected error message when request is denied")
			}
		})
	}
}
