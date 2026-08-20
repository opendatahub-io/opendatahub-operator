package datasciencepipelines_test

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dspgate "github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade/gates/datasciencepipelines"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"

	. "github.com/onsi/gomega"
)

type dataSciencePipelinesGateTestCtx struct {
	cli client.Client
}

func TestDataSciencePipelinesGates(t *testing.T) {
	te, err := envt.New()
	if err != nil {
		t.Fatalf("start envtest: %v", err)
	}
	t.Cleanup(func() {
		_ = te.Stop()
	})

	tc := &dataSciencePipelinesGateTestCtx{cli: te.Client()}

	t.Run("clean cluster passes", tc.testCleanClusterPasses)
	t.Run("stored version removed passes", tc.testStoredVersionRemovedPasses)
	t.Run("stored version present blocks", tc.testStoredVersionPresentBlocks)
	t.Run("role missing dspa api blocks", tc.testRoleMissingDSPAPISubresourceBlocks)
	t.Run("role with dspa api passes", tc.testRoleWithDSPAPISubresourcePasses)
	t.Run("role missing route verb parity blocks", tc.testRoleMissingRouteVerbParityBlocks)
}

func (tc *dataSciencePipelinesGateTestCtx) testCleanClusterPasses(t *testing.T) {
	g := NewWithT(t)

	err := dspgate.Check(t.Context(), tc.cli, componentApi.DataSciencePipelinesComponentName, "")
	g.Expect(err).ToNot(HaveOccurred())
}

func (tc *dataSciencePipelinesGateTestCtx) testStoredVersionRemovedPasses(t *testing.T) {
	g := NewWithT(t)

	crd := newDSPACRD("v1")
	createDSPACRD(t, g, tc.cli, crd)
	defer deleteCRD(g, tc.cli, crd)

	err := dspgate.Check(t.Context(), tc.cli, componentApi.DataSciencePipelinesComponentName, "")
	g.Expect(err).ToNot(HaveOccurred())
}

func (tc *dataSciencePipelinesGateTestCtx) testStoredVersionPresentBlocks(t *testing.T) {
	g := NewWithT(t)

	crd := newDSPACRD("v1alpha1", "v1")
	createDSPACRD(t, g, tc.cli, crd)
	defer deleteCRD(g, tc.cli, crd)

	err := dspgate.Check(t.Context(), tc.cli, componentApi.DataSciencePipelinesComponentName, "")
	g.Expect(err).To(HaveOccurred())

	var blockingErr *dspgate.UpgradeBlockedError
	g.Expect(errors.As(err, &blockingErr)).To(BeTrue())
	g.Expect(blockingErr.StoredVersion).To(Equal("v1alpha1"))
	g.Expect(blockingErr.RolesMissingAPISubresource).To(BeEmpty())
}

func (tc *dataSciencePipelinesGateTestCtx) testRoleMissingDSPAPISubresourceBlocks(t *testing.T) {
	g := NewWithT(t)

	role := newLegacyRouteRole("legacy-dsp-access", []string{"get", "list"})
	createRole(t, g, tc.cli, role)
	defer deleteRole(g, tc.cli, role)

	err := dspgate.Check(t.Context(), tc.cli, componentApi.DataSciencePipelinesComponentName, "")
	g.Expect(err).To(HaveOccurred())

	var blockingErr *dspgate.UpgradeBlockedError
	g.Expect(errors.As(err, &blockingErr)).To(BeTrue())
	g.Expect(blockingErr.StoredVersion).To(BeEmpty())
	g.Expect(blockingErr.RolesMissingAPISubresource).To(Equal([]string{"user-ns/legacy-dsp-access"}))
}

func (tc *dataSciencePipelinesGateTestCtx) testRoleWithDSPAPISubresourcePasses(t *testing.T) {
	g := NewWithT(t)

	role := newMigratedDSPRole("migrated-dsp-access", []string{"get", "list"})
	createRole(t, g, tc.cli, role)
	defer deleteRole(g, tc.cli, role)

	err := dspgate.Check(t.Context(), tc.cli, componentApi.DataSciencePipelinesComponentName, "")
	g.Expect(err).ToNot(HaveOccurred())
}

func (tc *dataSciencePipelinesGateTestCtx) testRoleMissingRouteVerbParityBlocks(t *testing.T) {
	g := NewWithT(t)

	role := newMigratedDSPRole("partial-dsp-access", []string{"get", "list", "watch"})
	role.Rules[1].Verbs = []string{"get", "list"}
	createRole(t, g, tc.cli, role)
	defer deleteRole(g, tc.cli, role)

	err := dspgate.Check(t.Context(), tc.cli, componentApi.DataSciencePipelinesComponentName, "")
	g.Expect(err).To(HaveOccurred())

	var blockingErr *dspgate.UpgradeBlockedError
	g.Expect(errors.As(err, &blockingErr)).To(BeTrue())
	g.Expect(blockingErr.RolesMissingAPISubresource).To(Equal([]string{"user-ns/partial-dsp-access"}))
}

func createDSPACRD(
	_ *testing.T,
	g *WithT,
	cli client.Client,
	crd *apiextensionsv1.CustomResourceDefinition,
) {
	g.Expect(cli.Create(context.Background(), crd)).ToNot(HaveOccurred())
}

func deleteCRD(g *WithT, cli client.Client, crd *apiextensionsv1.CustomResourceDefinition) {
	key := client.ObjectKeyFromObject(crd)

	err := cli.Delete(context.Background(), crd)
	g.Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())

	g.Eventually(func() error {
		current := &apiextensionsv1.CustomResourceDefinition{}
		return cli.Get(context.Background(), key, current)
	}).WithTimeout(envt.DefaultMaxWait).WithPolling(envt.DefaultPollInterval).Should(
		Satisfy(k8serr.IsNotFound),
	)
}

func createRole(_ *testing.T, g *WithT, cli client.Client, role *rbacv1.Role) {
	if role.Namespace != "" {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: role.Namespace}}
		err := cli.Create(context.Background(), ns)
		g.Expect(client.IgnoreAlreadyExists(err)).ToNot(HaveOccurred())
	}
	g.Expect(cli.Create(context.Background(), role)).ToNot(HaveOccurred())
}

func deleteRole(g *WithT, cli client.Client, role *rbacv1.Role) {
	key := client.ObjectKeyFromObject(role)

	err := cli.Delete(context.Background(), role)
	g.Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())

	g.Eventually(func() error {
		current := &rbacv1.Role{}
		return cli.Get(context.Background(), key, current)
	}).WithTimeout(envt.DefaultMaxWait).WithPolling(envt.DefaultPollInterval).Should(
		Satisfy(k8serr.IsNotFound),
	)
}
