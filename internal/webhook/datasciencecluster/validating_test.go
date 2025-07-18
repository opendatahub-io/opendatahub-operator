package datasciencecluster_test

import (
	"context"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/datasciencecluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/envtestutil"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

// TestDataScienceCluster_ValidatingWebhook exercises the validating webhook logic for DataScienceCluster resources.
// It verifies singleton enforcement and deletion rules using table-driven tests and a fake client.
func TestDataScienceCluster_ValidatingWebhook(t *testing.T) {
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
	}{
		{
			name:         "Allows creation if none exist",
			existingObjs: nil,
			req: envtestutil.NewAdmissionRequest(
				t,
				admissionv1.Create,
				envtestutil.NewDSC("test-create", ns),
				gvk.DataScienceCluster,
				metav1.GroupVersionResource{
					Group:    gvk.DataScienceCluster.Group,
					Version:  gvk.DataScienceCluster.Version,
					Resource: "datascienceclusters",
				},
			),
			allowed: true,
		},
		{
			name: "Denies creation if one already exists",
			existingObjs: []client.Object{
				envtestutil.NewDSC("existing", ns),
			},
			req: envtestutil.NewAdmissionRequest(
				t,
				admissionv1.Create,
				envtestutil.NewDSC("test-create", ns),
				gvk.DataScienceCluster,
				metav1.GroupVersionResource{
					Group:    gvk.DataScienceCluster.Group,
					Version:  gvk.DataScienceCluster.Version,
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
				envtestutil.NewDSC("test-delete", ns),
				gvk.DataScienceCluster,
				metav1.GroupVersionResource{
					Group:    gvk.DataScienceCluster.Group,
					Version:  gvk.DataScienceCluster.Version,
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
			objs = append(objs, envtestutil.NewDSCI("dsci-for-dsc", ns))
			cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(objs...).Build()
			validator := &datasciencecluster.Validator{
				Client: cli,
				Name:   "test",
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
