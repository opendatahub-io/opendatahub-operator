package security_test

import (
	"context"
	"testing"

	"github.com/rs/xid"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/security"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func TestUpdatePodSecurityRoleBindingAction(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	m := map[cluster.Platform][]string{
		cluster.OpenDataHub:      {"odh-dashboard"},
		cluster.SelfManagedRhoai: {"rhods-dashboard"},
		cluster.ManagedRhoai:     {"rhods-dashboard", "fake-account"},
	}

	action := security.NewUpdatePodSecurityRoleBindingAction(m)

	for p, s := range m {
		k := p
		vl := s

		t.Run(string(k), func(t *testing.T) {
			t.Parallel()

			g := NewWithT(t)
			ns := xid.New().String()

			cl, err := fakeclient.New(
				&rbacv1.RoleBinding{
					TypeMeta: metav1.TypeMeta{
						APIVersion: gvk.RoleBinding.GroupVersion().String(),
						Kind:       gvk.RoleBinding.Kind,
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      ns,
						Namespace: ns,
					},
				},
			)

			g.Expect(err).ShouldNot(HaveOccurred())

			err = action(ctx, &types.ReconciliationRequest{
				Client:   cl,
				Instance: nil,
				DSCI:     &dsciv1.DSCInitialization{Spec: dsciv1.DSCInitializationSpec{ApplicationsNamespace: ns}},
				Release:  cluster.Release{Name: k},
			})

			g.Expect(err).ShouldNot(HaveOccurred())

			rb := rbacv1.RoleBinding{}
			err = cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: ns}, &rb)

			g.Expect(err).ShouldNot(HaveOccurred())
			for _, v := range vl {
				g.Expect(cluster.SubjectExistInRoleBinding(rb.Subjects, v, ns)).Should(BeTrue())
			}
		})
	}
}
