package v1_test

import (
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	v1webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/envtestutil"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

// TestDataScienceClusterV1_ValidatingWebhook exercises the validating webhook logic for DataScienceCluster v1 resources.
// It verifies singleton enforcement and deletion rules using table-driven tests and a fake client.
func TestDataScienceClusterV1_ValidatingWebhook(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	ctx := t.Context()

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
				envtestutil.NewDSCV1("test-create"),
				gvk.DataScienceCluster,
				metav1.GroupVersionResource{
					Group:    gvk.DataScienceClusterV1.Group,
					Version:  gvk.DataScienceClusterV1.Version,
					Resource: "datascienceclusters",
				},
			),
			allowed: true,
		},
		{
			name: "Denies creation if one already exists",
			existingObjs: []client.Object{
				envtestutil.NewDSC("existing"),
			},
			req: envtestutil.NewAdmissionRequest(
				t,
				admissionv1.Create,
				envtestutil.NewDSCV1("test-create"),
				gvk.DataScienceCluster,
				metav1.GroupVersionResource{
					Group:    gvk.DataScienceClusterV1.Group,
					Version:  gvk.DataScienceClusterV1.Version,
					Resource: "datascienceclusters",
				},
			),
			allowed: false,
		},
		{
			name:         "Allows deletion always",
			existingObjs: nil,
			req: envtestutil.NewAdmissionRequest(
				t,
				admissionv1.Delete,
				envtestutil.NewDSCV1("test-delete"),
				gvk.DataScienceCluster,
				metav1.GroupVersionResource{
					Group:    gvk.DataScienceClusterV1.Group,
					Version:  gvk.DataScienceClusterV1.Version,
					Resource: "datascienceclusters",
				},
			),
			allowed: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			objs := append([]client.Object{}, tc.existingObjs...)
			objs = append(objs, envtestutil.NewDSCIV1("dsci-for-dsc"))
			cli, err := fakeclient.New(fakeclient.WithObjects(objs...))
			g.Expect(err).ShouldNot(HaveOccurred())
			validator := &v1webhook.Validator{
				Client: cli,
				Name:   "test-v1",
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
