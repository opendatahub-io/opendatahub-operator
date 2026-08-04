package v1_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	v1webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/envtestutil"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

// TestDSCInitializationV1_ValidatingWebhook exercises the validating webhook logic for DSCInitialization v1 resources.
// It verifies singleton enforcement, deletion restrictions, and customCABundle PEM validation
// using table-driven tests and a fake client.
func TestDSCInitializationV1_ValidatingWebhook(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	ns := "test-ns"
	validPEM := envtestutil.GenerateTestCertPEM(t)

	withTrustedCABundle := func(state operatorv1.ManagementState, bundle string) func(*dsciv1.DSCInitialization) {
		return func(dsci *dsciv1.DSCInitialization) {
			dsci.Spec.TrustedCABundle = &dsciv1.TrustedCABundleSpec{
				ManagementState: state,
				CustomCABundle:  bundle,
			}
		}
	}

	gvr := metav1.GroupVersionResource{
		Group:    gvk.DSCInitializationV1.Group,
		Version:  gvk.DSCInitializationV1.Version,
		Resource: "dscinitializations",
	}

	cases := []struct {
		name         string
		existingObjs []client.Object
		req          admission.Request
		allowed      bool
	}{
		{
			name:         "Allows creation if none exist",
			existingObjs: nil,
			req:          envtestutil.NewAdmissionRequest(t, admissionv1.Create, envtestutil.NewDSCIV1("test-create"), gvk.DSCInitialization, gvr),
			allowed:      true,
		},
		{
			name:         "Denies creation if one already exists",
			existingObjs: []client.Object{envtestutil.NewDSCI("existing")},
			req:          envtestutil.NewAdmissionRequest(t, admissionv1.Create, envtestutil.NewDSCIV1("test-create"), gvk.DSCInitialization, gvr),
			allowed:      false,
		},
		{
			name: "Denies deletion if DSC exists",
			existingObjs: []client.Object{
				&dscv2.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Name: "dsc-1", Namespace: ns}},
				envtestutil.NewDSCI("dsci-1"),
			},
			req:     envtestutil.NewAdmissionRequest(t, admissionv1.Delete, envtestutil.NewDSCIV1("dsci-1"), gvk.DSCInitialization, gvr),
			allowed: false,
		},
		{
			name:         "Allows deletion if no DSC exists",
			existingObjs: []client.Object{envtestutil.NewDSCI("dsci-1")},
			req:          envtestutil.NewAdmissionRequest(t, admissionv1.Delete, envtestutil.NewDSCIV1("dsci-1"), gvk.DSCInitialization, gvr),
			allowed:      true,
		},
		// PEM validation on Create
		{
			name: "Create with valid PEM and TrustedCABundle Managed",
			req: envtestutil.NewAdmissionRequest(t, admissionv1.Create,
				envtestutil.NewDSCIV1("test-valid-pem", withTrustedCABundle(operatorv1.Managed, validPEM)),
				gvk.DSCInitialization, gvr),
			allowed: true,
		},
		{
			name: "Denies create with invalid PEM and TrustedCABundle Managed",
			req: envtestutil.NewAdmissionRequest(t, admissionv1.Create,
				envtestutil.NewDSCIV1("test-bad-pem", withTrustedCABundle(operatorv1.Managed, "not-a-pem")),
				gvk.DSCInitialization, gvr),
			allowed: false,
		},
		// PEM validation on Update
		{
			name: "Update with valid PEM and TrustedCABundle Managed",
			req: envtestutil.NewAdmissionRequest(t, admissionv1.Update,
				envtestutil.NewDSCIV1("test-update", withTrustedCABundle(operatorv1.Managed, validPEM)),
				gvk.DSCInitialization, gvr),
			allowed: true,
		},
		{
			name: "Denies update with invalid PEM and TrustedCABundle Managed",
			req: envtestutil.NewAdmissionRequest(t, admissionv1.Update,
				envtestutil.NewDSCIV1("test-update-bad", withTrustedCABundle(operatorv1.Managed, "garbage")),
				gvk.DSCInitialization, gvr),
			allowed: false,
		},
		{
			name: "Allows update with invalid PEM when TrustedCABundle is Removed",
			req: envtestutil.NewAdmissionRequest(t, admissionv1.Update,
				envtestutil.NewDSCIV1("test-removed", withTrustedCABundle(operatorv1.Removed, "garbage")),
				gvk.DSCInitialization, gvr),
			allowed: true,
		},
		{
			name: "Allows update with empty customCABundle when TrustedCABundle is Managed",
			req: envtestutil.NewAdmissionRequest(t, admissionv1.Update,
				envtestutil.NewDSCIV1("test-empty", withTrustedCABundle(operatorv1.Managed, "")),
				gvk.DSCInitialization, gvr),
			allowed: true,
		},
		{
			name: "Allows update with no TrustedCABundle set",
			req: envtestutil.NewAdmissionRequest(t, admissionv1.Update,
				envtestutil.NewDSCIV1("test-nil"),
				gvk.DSCInitialization, gvr),
			allowed: true,
		},
		{
			name: "Update with valid multi-cert PEM chain",
			req: envtestutil.NewAdmissionRequest(t, admissionv1.Update,
				envtestutil.NewDSCIV1("test-chain", withTrustedCABundle(operatorv1.Managed, validPEM+validPEM)),
				gvk.DSCInitialization, gvr),
			allowed: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)
			sch, err := scheme.New()
			g.Expect(err).ShouldNot(HaveOccurred())
			cli, err := fakeclient.New(fakeclient.WithObjects(tc.existingObjs...), fakeclient.WithScheme(sch))
			g.Expect(err).ShouldNot(HaveOccurred())
			validator := &v1webhook.Validator{
				Client:  cli,
				Name:    "test-v1",
				Decoder: admission.NewDecoder(sch),
			}
			resp := validator.Handle(ctx, tc.req)
			t.Logf("Admission response: Allowed=%v, Result=%+v", resp.Allowed, resp.Result)
			g.Expect(resp.Allowed).To(Equal(tc.allowed))
			if !tc.allowed {
				g.Expect(resp.Result.Message).ToNot(BeEmpty(), "Expected error message when request is denied")
			}
		})
	}
}
