package upgrade_test

import (
	"testing"

	"github.com/blang/semver/v4"
	"github.com/operator-framework/api/pkg/lib/version"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/gates"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

const (
	testOperatorNS      = "test-operator-ns"
	testOperatorVersion = "3.0.0"
)

var testComponents = []string{"dashboard", "kserve", "ray"}

func dsciWithVersion(major, minor uint64) *dsciv2.DSCInitialization {
	return &dsciv2.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{
			Name: "default-dsci",
		},
		Status: dsciv2.DSCInitializationStatus{
			Release: common.Release{
				Version: version.OperatorVersion{
					Version: semver.Version{Major: major, Minor: minor},
				},
			},
		},
	}
}

func TestCreateUpgradeGateConfigMap_UpgradeFrom2x(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	dsci := dsciWithVersion(2, 16)
	cli, err := fakeclient.New(fakeclient.WithObjects(dsci))
	g.Expect(err).ShouldNot(HaveOccurred())

	err = upgrade.CreateUpgradeGateConfigMap(ctx, cli, testOperatorNS, testOperatorVersion, testComponents)
	g.Expect(err).ShouldNot(HaveOccurred())

	cm := &corev1.ConfigMap{}
	err = cli.Get(ctx, client.ObjectKey{Name: gates.GateConfigMap, Namespace: testOperatorNS}, cm)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(cm.Labels).To(HaveKeyWithValue(gates.UpgradeGateLabel, "true"))
	g.Expect(cm.Data).To(HaveLen(len(testComponents)))

	for _, comp := range testComponents {
		key := "ack-" + testOperatorVersion + "-" + comp
		g.Expect(cm.Data).To(HaveKey(key))
		g.Expect(cm.Data[key]).To(ContainSubstring(comp))
	}
}

func TestCreateUpgradeGateConfigMap_FreshInstall(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	// No DSCI → GetDeployedRelease returns empty Release (Major == 0)
	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	err = upgrade.CreateUpgradeGateConfigMap(ctx, cli, testOperatorNS, testOperatorVersion, testComponents)
	g.Expect(err).ShouldNot(HaveOccurred())

	cm := &corev1.ConfigMap{}
	err = cli.Get(ctx, client.ObjectKey{Name: gates.GateConfigMap, Namespace: testOperatorNS}, cm)
	g.Expect(err).Should(HaveOccurred(), "no ConfigMap should be created on fresh install")
}

func TestCreateUpgradeGateConfigMap_SameMajor(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	dsci := dsciWithVersion(3, 0)
	cli, err := fakeclient.New(fakeclient.WithObjects(dsci))
	g.Expect(err).ShouldNot(HaveOccurred())

	err = upgrade.CreateUpgradeGateConfigMap(ctx, cli, testOperatorNS, "3.1.0", testComponents)
	g.Expect(err).ShouldNot(HaveOccurred())

	cm := &corev1.ConfigMap{}
	err = cli.Get(ctx, client.ObjectKey{Name: gates.GateConfigMap, Namespace: testOperatorNS}, cm)
	g.Expect(err).Should(HaveOccurred(), "no ConfigMap should be created for same-major upgrade")
}

func TestCreateUpgradeGateConfigMap_Idempotent(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	dsci := dsciWithVersion(2, 16)
	cli, err := fakeclient.New(fakeclient.WithObjects(dsci))
	g.Expect(err).ShouldNot(HaveOccurred())

	err = upgrade.CreateUpgradeGateConfigMap(ctx, cli, testOperatorNS, testOperatorVersion, testComponents)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Second call must not error (AlreadyExists is silenced)
	err = upgrade.CreateUpgradeGateConfigMap(ctx, cli, testOperatorNS, testOperatorVersion, testComponents)
	g.Expect(err).ShouldNot(HaveOccurred())

	cmList := &corev1.ConfigMapList{}
	err = cli.List(ctx, cmList, client.InNamespace(testOperatorNS), client.MatchingLabels{gates.UpgradeGateLabel: "true"})
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(cmList.Items).To(HaveLen(1), "only one gate ConfigMap should exist")
}

func TestCreateUpgradeGateConfigMap_DiscoverableByGateChecker(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	dsci := dsciWithVersion(2, 20)
	cli, err := fakeclient.New(fakeclient.WithObjects(dsci))
	g.Expect(err).ShouldNot(HaveOccurred())

	err = upgrade.CreateUpgradeGateConfigMap(ctx, cli, testOperatorNS, testOperatorVersion, testComponents)
	g.Expect(err).ShouldNot(HaveOccurred())

	gc := gates.NewGateChecker(cli, testOperatorNS)
	discovered, err := gc.DiscoverGates(ctx)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(discovered).To(HaveLen(len(testComponents)))

	for _, comp := range testComponents {
		key := "ack-" + testOperatorVersion + "-" + comp
		g.Expect(discovered).To(HaveKey(key))
	}
}

func TestCreateUpgradeGateConfigMap_AllManagedComponents(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	dsci := dsciWithVersion(2, 16)
	cli, err := fakeclient.New(fakeclient.WithObjects(dsci))
	g.Expect(err).ShouldNot(HaveOccurred())

	err = upgrade.CreateUpgradeGateConfigMap(ctx, cli, testOperatorNS, testOperatorVersion, upgrade.ManagedComponents)
	g.Expect(err).ShouldNot(HaveOccurred())

	cm := &corev1.ConfigMap{}
	err = cli.Get(ctx, client.ObjectKey{Name: gates.GateConfigMap, Namespace: testOperatorNS}, cm)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(cm.Data).To(HaveLen(len(upgrade.ManagedComponents)))
	for _, comp := range upgrade.ManagedComponents {
		key := "ack-" + testOperatorVersion + "-" + comp
		g.Expect(cm.Data).To(HaveKey(key))
	}
}
