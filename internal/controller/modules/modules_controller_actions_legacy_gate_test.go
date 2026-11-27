//nolint:testpackage // Exercises the internal migration gate.
package modules

import (
	"testing"

	semver "github.com/blang/semver/v4"
	ofversion "github.com/operator-framework/api/pkg/lib/version"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func TestLegacyMigrationUsesTrackerProvisioningForManagedModules(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	withTestRegistry(t)
	handler := &legacyTestHandler{
		provisioningModuleStub: provisioningModuleStub{moduleName: "module", enabled: true},
		state:                  CRStateDeleting,
	}
	DefaultRegistry().Add(handler)
	release := common.Release{Name: cluster.XKS, Version: ofversion.OperatorVersion{Version: semver.MustParse("3.7.0")}}
	platform := &configApi.Platform{ObjectMeta: metav1.ObjectMeta{Name: "default", UID: "platform"}}
	tracker := &configApi.PlatformModule{
		ObjectMeta: metav1.ObjectMeta{
			Name: "module", UID: "tracker", Generation: 1,
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(platform, gvk.Platform)},
		},
		Status: configApi.PlatformModuleStatus{
			Status: common.Status{Conditions: []common.Condition{
				{Type: status.ConditionTypeProvisioningSucceeded, Status: metav1.ConditionTrue, ObservedGeneration: 1},
				{Type: status.ConditionDeploymentsAvailable, Status: metav1.ConditionFalse},
			}},
			Release: release,
		},
	}
	cli, err := fakeclient.New(fakeclient.WithObjects(platform, tracker))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(checkLegacyMigrationReadiness(ctx, cli, platform, release)).To(Succeed())
}

func TestLegacyMigrationBlocksOnFailedProvisioning(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()
	withTestRegistry(t)
	handler := &legacyTestHandler{
		provisioningModuleStub: provisioningModuleStub{moduleName: "legacy-mod", enabled: true},
		state:                  CRStateDeleting,
	}
	DefaultRegistry().Add(handler)
	release := common.Release{Name: cluster.XKS, Version: ofversion.OperatorVersion{Version: semver.MustParse("3.7.0")}}
	platform := &configApi.Platform{ObjectMeta: metav1.ObjectMeta{Name: "default", UID: "platform"}}
	// The tracker published the current release and its operator deployment
	// is available, but provisioning never succeeded: the migration must stay
	// blocked until the module finishes provisioning.
	tracker := &configApi.PlatformModule{
		ObjectMeta: metav1.ObjectMeta{
			Name: "legacy-mod", UID: "tracker", Generation: 1,
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(platform, gvk.Platform)},
		},
		Status: configApi.PlatformModuleStatus{
			Status: common.Status{Conditions: []common.Condition{
				{Type: status.ConditionTypeProvisioningSucceeded, Status: metav1.ConditionFalse, ObservedGeneration: 1},
				{Type: status.ConditionDeploymentsAvailable, Status: metav1.ConditionTrue},
			}},
			Release: release,
		},
	}
	cli, err := fakeclient.New(fakeclient.WithObjects(platform, tracker))
	g.Expect(err).NotTo(HaveOccurred())
	err = checkLegacyMigrationReadiness(ctx, cli, platform, release)
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("waiting for module legacy-mod to finish"),
		"migration must block until the module finishes provisioning")
}
