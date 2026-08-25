//go:build integration

package upgrades_test

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlconfig "sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	dscctrl "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/datasciencecluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/gates"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/operatorconfig"
	upgradegates "github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade/gates"
	tp "github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/template"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"

	. "github.com/onsi/gomega"
)

type fixtureSpec struct {
	path string
	data map[string]any
}

type upgradeGateTestCtx struct {
	cli client.Client
}

type upgradeGateHarness struct {
	tf *testf.WithT
	tc *upgradeGateTestCtx
}

// applyFixture creates an additional fixture against a running harness,
// e.g. to mutate cluster state mid-test and observe the gate driver
// converge on a later reconcile.
func (h *upgradeGateHarness) applyFixture(t *testing.T, templatePath string, data map[string]any) {
	t.Helper()

	h.tc.applyFixture(t, templatePath, data)
}

//nolint:gochecknoglobals // Shared immutable object keys keep test assertions concise.
var (
	upgradeAcksKey = types.NamespacedName{Name: gates.AcksConfigMap, Namespace: operatorNamespace}
	defaultDSCKey  = types.NamespacedName{Name: "default-dsc"}
)

func fixture(path string, data map[string]any) fixtureSpec {
	return fixtureSpec{path: path, data: data}
}

func setupUpgradeGateTest(t *testing.T, extraFixtures ...fixtureSpec) *upgradeGateHarness {
	t.Helper()

	return setupUpgradeGateTestWithManagedComponents(t, nil, extraFixtures...)
}

func setupUpgradeGateTestWithManagedComponents(
	t *testing.T,
	managedComponents []string,
	extraFixtures ...fixtureSpec,
) *upgradeGateHarness {
	t.Helper()

	return setupUpgradeGateTestWithReleases(t, deployedVersion, managedComponents, extraFixtures...)
}

// setupUpgradeGateTestWithReleases is the deployed-release-explicit variant.
// deployedVer stamps the DSC/DSCI status.release used by
// cluster.GetDeployedRelease to decide whether gating applies (only 2.x
// upgrades gate). Pass an empty string to simulate a fresh install with no
// deployed release.
func setupUpgradeGateTestWithReleases(
	t *testing.T,
	deployedVer string,
	managedComponents []string,
	extraFixtures ...fixtureSpec,
) *upgradeGateHarness {
	t.Helper()

	g := NewWithT(t)
	t.Setenv("CI", "")

	root, err := envtestutil.FindProjectRoot()
	g.Expect(err).ToNot(HaveOccurred())

	te, err := envt.New(
		envt.WithProjectRoot(root),
		envt.WithCRDPaths(
			root+"/config/crd/bases",
			root+"/tests/integration/upgrades/resources/crds",
		),
		envt.WithManager(manager.Options{
			Controller: ctrlconfig.Controller{SkipNameValidation: ptr.To(true)},
		}),
	)
	g.Expect(err).ToNot(HaveOccurred())
	t.Cleanup(func() {
		_ = te.Stop()
	})

	tc := &upgradeGateTestCtx{cli: te.Client()}
	for _, fixture := range append(baseUpgradeFixtures(deployedVer, managedComponents), extraFixtures...) {
		tc.applyFixture(t, fixture.path, fixture.data)
	}

	ctx := t.Context()
	err = cluster.Init(ctx, tc.cli, operatorconfig.OperatorSettings{
		OperatorNamespace: operatorNamespace,
		PlatformType:      "OpenDataHub",
	})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(cluster.GetRelease().Version.String()).To(Equal(targetVersion))

	testCtx, err := testf.NewTestContext(
		testf.WithContext(ctx),
		testf.WithClient(tc.cli),
		testf.WithTOptions(
			testf.WithEventuallyTimeout(envt.DefaultMaxWait),
			testf.WithEventuallyPollingInterval(envt.DefaultPollInterval),
		),
	)
	g.Expect(err).ToNot(HaveOccurred())

	upgradegates.Register()

	g.Expect(dscctrl.NewDataScienceClusterReconciler(ctx, te.Manager())).To(Succeed())
	g.Expect(modules.NewModuleReconciler(ctx, te.Manager())).To(Succeed())
	te.StartManager(t, ctx)

	return &upgradeGateHarness{
		tf: testCtx.NewWithT(t),
		tc: tc,
	}
}

func baseUpgradeFixtures(deployedVer string, managedComponents []string) []fixtureSpec {
	return []fixtureSpec{
		fixture("resources/namespace.tmpl.yaml", map[string]any{
			"Name":   operatorNamespace,
			"Labels": map[string]string{},
		}),
		fixture("resources/dscinitialization.tmpl.yaml", map[string]any{
			"Name":     "default-dsci",
			"Platform": "OpenDataHub",
			"Version":  deployedVer,
		}),
		fixture("resources/datasciencecluster.tmpl.yaml", map[string]any{
			"Name":              "default-dsc",
			"Platform":          "OpenDataHub",
			"Version":           deployedVer,
			"ManagedComponents": managedComponents,
		}),
		fixture("resources/clusterserviceversion.tmpl.yaml", map[string]any{
			"Name":      "opendatahub-operator.v" + targetVersion,
			"Namespace": operatorNamespace,
			"Version":   targetVersion,
		}),
	}
}

func certManagerPresentFixtures() []fixtureSpec {
	return []fixtureSpec{
		fixture("resources/namespace.tmpl.yaml", map[string]any{
			"Name":   "cert-manager-operator",
			"Labels": map[string]string{},
		}),
		fixture("resources/subscription.tmpl.yaml", map[string]any{
			"Name":         "openshift-cert-manager-operator",
			"Namespace":    "cert-manager-operator",
			"Channel":      "",
			"InstalledCSV": "",
		}),
	}
}

func ackKey(gateKey string) string {
	return "ack-" + targetVersion + "-" + gateKey
}

func (tc *upgradeGateTestCtx) applyFixture(t *testing.T, templatePath string, data map[string]any) {
	t.Helper()

	g := NewWithT(t)
	ctx := t.Context()

	obj, err := tp.RenderObject(resourcesFS, templatePath, data)
	g.Expect(err).ToNot(HaveOccurred())

	statusValue, foundStatus, err := unstructured.NestedFieldCopy(obj.Object, "status")
	g.Expect(err).ToNot(HaveOccurred())
	if foundStatus {
		unstructured.RemoveNestedField(obj.Object, "status")
	}

	g.Expect(tc.cli.Create(ctx, obj)).To(Succeed())

	if !foundStatus {
		return
	}

	statusObj := obj.DeepCopy()
	g.Expect(unstructured.SetNestedField(statusObj.Object, statusValue, "status")).To(Succeed())
	g.Expect(tc.cli.Status().Update(ctx, statusObj)).To(Succeed())
}
