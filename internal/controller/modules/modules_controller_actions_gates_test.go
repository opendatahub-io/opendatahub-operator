//nolint:testpackage // Exercises package-private gate preflight wiring.
package modules

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	semver "github.com/blang/semver/v4"
	ofversion "github.com/operator-framework/api/pkg/lib/version"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	conditionstest "github.com/opendatahub-io/opendatahub-operator/v2/internal/testutil/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	oerrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/gates"
	odhtype "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/operatorconfig"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/mocks"

	. "github.com/onsi/gomega"
)

func TestNewGateRenderRequestIsolatesMutableState(t *testing.T) {
	g := NewWithT(t)

	rr := &odhtype.ReconciliationRequest{
		ManifestsBasePath: "manifests",
		ChartsBasePath:    "charts",
		Manifests:         []odhtype.ManifestInfo{{Path: "module"}},
		Resources:         []unstructured.Unstructured{{Object: map[string]any{"kind": "ConfigMap"}}},
		Extensions:        map[string]any{"key": "value"},
		GateEntries:       map[string]string{"gate": "description"},
		SkipDeploy:        true,
		Generated:         true,
	}

	clone := newGateRenderRequest(rr)

	g.Expect(clone.ManifestsBasePath).To(Equal(rr.ManifestsBasePath))
	g.Expect(clone.ChartsBasePath).To(Equal(rr.ChartsBasePath))
	g.Expect(clone.Manifests).To(BeEmpty())
	g.Expect(clone.Templates).To(BeEmpty())
	g.Expect(clone.HelmCharts).To(BeEmpty())
	g.Expect(clone.Resources).To(BeEmpty())
	g.Expect(clone.GateEntries).To(BeEmpty())
	g.Expect(clone.Extensions).To(BeEmpty())
	g.Expect(clone.SkipDeploy).To(BeFalse())
	g.Expect(clone.Generated).To(BeFalse())
	g.Expect(rr.Manifests).To(HaveLen(1))
	g.Expect(rr.Resources).To(HaveLen(1))
	g.Expect(rr.GateEntries).To(HaveLen(1))
}

type gateTestModule struct {
	provisioningModuleStub

	manifests OperatorManifests
}

func (s gateTestModule) GetOperatorManifests(*PlatformContext) OperatorManifests {
	return s.manifests
}

func TestCheckPlatformModuleUpgradeGatesStopsBeforeTrackerMutation(t *testing.T) {
	g := NewWithT(t)
	withTestRegistry(t)

	const version = "99.99.99"
	const operatorNamespace = "test-operator"

	dsci := &dsciv2.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{Name: "test-dsci"},
		Spec: dsciv2.DSCInitializationSpec{
			ApplicationsNamespace: "opendatahub",
		},
	}
	cli, err := fakeclient.New(fakeclient.WithObjects(dsci))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(cluster.Init(t.Context(), cli, operatorconfig.OperatorSettings{
		OperatorNamespace: operatorNamespace,
		PlatformType:      string(cluster.XKS),
	})).To(Succeed())

	DefaultRegistry().Add(gateTestModule{
		provisioningModuleStub: provisioningModuleStub{
			moduleName: "gate-module",
			enabled:    true,
		},
		manifests: OperatorManifests{
			Manifests: []odhtype.ManifestInfo{{Path: "testdata/gate-module"}},
		},
	})
	DefaultRegistry().Add(gateTestModule{
		provisioningModuleStub: provisioningModuleStub{moduleName: "broken-module", enabled: true},
		manifests:              OperatorManifests{Manifests: []odhtype.ManifestInfo{{Path: "testdata/missing-module"}}},
	})

	platform := &configApi.Platform{
		ObjectMeta: metav1.ObjectMeta{Name: configApi.PlatformInstanceName},
	}
	tracker := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": configApi.GroupVersion.String(),
		"kind":       configApi.PlatformModuleKind,
		"metadata": map[string]any{
			"name": "gate-module",
		},
	}}
	rr := &odhtype.ReconciliationRequest{
		Client:   cli,
		Instance: platform,
		Release: common.Release{
			Name:    cluster.XKS,
			Version: ofversion.OperatorVersion{Version: semver.MustParse(version)},
		},
		Conditions: conditionstest.NewManager(platform, "ProvisioningProgress"),
		Resources:  []unstructured.Unstructured{tracker},
	}

	err = checkPlatformModuleUpgradeGates(t.Context(), rr)
	var stopErr oerrors.StopError
	g.Expect(errors.As(err, &stopErr)).To(BeTrue(), "unacknowledged gate must stop Platform reconciliation")
	g.Expect(err).To(MatchError(ContainSubstring("1 unacknowledged upgrade gate(s)")))
	g.Expect(rr.Resources).To(HaveLen(1), "gate preflight must not mutate tracker resources")
	g.Expect(rr.Resources[0]).To(Equal(tracker))
	g.Expect(rr.GateEntries).To(BeEmpty(), "chart gates belong to isolated preflight request")
	g.Expect(ModuleGatePreflightErrors(rr)).To(HaveKeyWithValue("broken-module", ContainSubstring("missing-module")))
}

func TestCheckPlatformModuleUpgradeGatesIsolatesRenderFailures(t *testing.T) {
	g := NewWithT(t)
	withTestRegistry(t)

	// Arrange an xKS client whose gate discovery can fail on demand.
	dsci := &dsciv2.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{Name: "test-dsci"},
		Spec:       dsciv2.DSCInitializationSpec{ApplicationsNamespace: "opendatahub"},
	}
	failGateDiscovery := false
	gateDiscoveryError := errors.New("gate discovery unavailable")
	cli, err := fakeclient.New(fakeclient.WithObjects(dsci), fakeclient.WithInterceptorFuncs(interceptor.Funcs{
		List: func(ctx context.Context, cli client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if _, isConfigMapList := list.(*corev1.ConfigMapList); isConfigMapList && failGateDiscovery {
				return gateDiscoveryError
			}
			return cli.List(ctx, list, opts...)
		},
	}))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(cluster.Init(t.Context(), cli, operatorconfig.OperatorSettings{
		OperatorNamespace: "test-operator",
		PlatformType:      string(cluster.XKS),
	})).To(Succeed())

	// One module can render; the other points to a missing manifest directory.
	manifestPath := t.TempDir()
	g.Expect(os.WriteFile(filepath.Join(manifestPath, "kustomization.yaml"), []byte(`apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
configMapGenerator:
- name: healthy-module-config
  literals:
  - enabled=true
`), 0o600)).To(Succeed())
	DefaultRegistry().Add(gateTestModule{
		provisioningModuleStub: provisioningModuleStub{moduleName: "healthy-module", enabled: true},
		manifests:              OperatorManifests{Manifests: []odhtype.ManifestInfo{{Path: manifestPath}}},
	})
	DefaultRegistry().Add(gateTestModule{
		provisioningModuleStub: provisioningModuleStub{moduleName: "broken-module", enabled: true},
		manifests:              OperatorManifests{Manifests: []odhtype.ManifestInfo{{Path: "testdata/missing-module"}}},
	})
	platform := &configApi.Platform{ObjectMeta: metav1.ObjectMeta{Name: configApi.PlatformInstanceName}}
	rr := &odhtype.ReconciliationRequest{
		Client:   cli,
		Instance: platform,
		Release: common.Release{
			Name:    cluster.XKS,
			Version: ofversion.OperatorVersion{Version: semver.MustParse("99.99.99")},
		},
		Conditions: conditionstest.NewManager(platform, "ProvisioningProgress"),
	}

	// The broken module requests a retry without leaking operator resources.
	err = checkPlatformModuleUpgradeGates(t.Context(), rr)
	var requeueErr oerrors.RequeueAfterError
	g.Expect(errors.As(err, &requeueErr)).To(BeTrue(), "render failures must allow the action chain to continue")
	g.Expect(requeueErr.After).To(Equal(30 * time.Second))
	g.Expect(rr.Resources).To(BeEmpty(), "preflight must not leak rendered operator resources")
	g.Expect(ModuleGatePreflightErrors(rr)).To(HaveLen(1))
	g.Expect(ModuleGatePreflightErrors(rr)).To(HaveKeyWithValue("broken-module", ContainSubstring("missing-module")))

	// RequeueAfterError continues the Platform action chain. Both trackers are
	// created; each tracker renders and gates its own operator independently.
	platform.SetGroupVersionKind(configApi.GroupVersion.WithKind(configApi.PlatformKind))
	rr.Controller = mocks.NewMockController(func(m *mocks.MockController) {
		m.On("Owns", gvk.PlatformModule).Return(true)
	})
	g.Expect(provisionPlatformModules(t.Context(), rr)).To(Succeed())
	g.Expect(deploy.NewAction(deploy.WithMode(deploy.ModePatch))(t.Context(), rr)).To(Succeed())
	for _, name := range []string{"healthy-module", "broken-module"} {
		g.Expect(cli.Get(t.Context(), client.ObjectKey{Name: name}, &configApi.PlatformModule{})).To(Succeed())
	}
	rr.Resources = nil

	// A corrected installation can render on retry; stale diagnostics must clear.
	DefaultRegistry().Add(gateTestModule{
		provisioningModuleStub: provisioningModuleStub{moduleName: "broken-module", enabled: true},
		manifests:              OperatorManifests{Manifests: []odhtype.ManifestInfo{{Path: manifestPath}}},
	})
	g.Expect(checkPlatformModuleUpgradeGates(t.Context(), rr)).To(Succeed())
	g.Expect(ModuleGatePreflightErrors(rr)).To(BeEmpty())
	// Restore the render failure to verify global gates still fail closed.
	DefaultRegistry().Add(gateTestModule{
		provisioningModuleStub: provisioningModuleStub{moduleName: "broken-module", enabled: true},
		manifests:              OperatorManifests{Manifests: []odhtype.ManifestInfo{{Path: "testdata/missing-module"}}},
	})

	// An unacknowledged global gate still stops Platform reconciliation.
	globalGate := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: "global-gate", Namespace: "test-operator",
			Labels: map[string]string{gates.UpgradeGateLabel: "true"},
		},
		Data: map[string]string{"ack-99.99.99-global-gate": "Review platform migration"},
	}
	g.Expect(cli.Create(t.Context(), globalGate)).To(Succeed())
	err = checkPlatformModuleUpgradeGates(t.Context(), rr)
	var stopErr oerrors.StopError
	g.Expect(errors.As(err, &stopErr)).To(BeTrue(), "global gates must block even with a broken module")

	// An API failure must also stop, rather than becoming a render retry.
	failGateDiscovery = true
	err = checkPlatformModuleUpgradeGates(t.Context(), rr)
	g.Expect(errors.Is(err, gateDiscoveryError)).To(BeTrue(), "gate API errors must fail closed")
	g.Expect(errors.As(err, &requeueErr)).To(BeFalse())
}
