package registry_test

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/registry"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/operatorconfig"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

// crComponentHandler is a fake handler that returns a real component CR.
type crComponentHandler struct {
	name    string
	enabled bool
	cr      common.PlatformObject
}

func (f *crComponentHandler) Init(_ common.Platform, _ operatorconfig.OperatorSettings) error {
	return nil
}
func (f *crComponentHandler) GetName() string { return f.name }
func (f *crComponentHandler) NewCRObject(_ context.Context, _ client.Client, _ *dscv2.DataScienceCluster) (common.PlatformObject, error) {
	return f.cr, nil
}
func (f *crComponentHandler) NewComponentReconciler(_ context.Context, _ ctrl.Manager) error {
	return nil
}
func (f *crComponentHandler) UpdateDSCStatus(_ context.Context, _ *types.ReconciliationRequest) (metav1.ConditionStatus, error) {
	return metav1.ConditionTrue, nil
}
func (f *crComponentHandler) IsEnabled(_ *dscv2.DataScienceCluster) bool {
	return f.enabled
}

func TestReadinessChecker_ReadyWithMatchingPlatformVersion(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	dsp := &componentApi.DataSciencePipelines{
		ObjectMeta: metav1.ObjectMeta{
			Name: componentApi.DataSciencePipelinesInstanceName,
		},
		Status: componentApi.DataSciencePipelinesStatus{
			Status: common.Status{
				Conditions: []common.Condition{
					{Type: status.ConditionTypeReady, Status: metav1.ConditionTrue},
				},
			},
			DataSciencePipelinesCommonStatus: componentApi.DataSciencePipelinesCommonStatus{
				ComponentReleaseStatus: common.ComponentReleaseStatus{
					Releases: []common.ComponentRelease{
						{Name: "platform", Version: "2.20.0"},
					},
				},
			},
		},
	}

	cli, err := fakeclient.New(
		fakeclient.WithObjects(dsp),
		fakeclient.WithGVKs(fakeclient.GVKMapping{
			GVK:   gvk.DataSciencePipelines,
			Scope: meta.RESTScopeRoot,
		}),
	)
	g.Expect(err).ShouldNot(HaveOccurred())

	reg := &registry.Registry{}
	reg.Add(&crComponentHandler{
		name:    "datasciencepipelines",
		enabled: true,
		cr: &componentApi.DataSciencePipelines{
			ObjectMeta: metav1.ObjectMeta{
				Name: componentApi.DataSciencePipelinesInstanceName,
			},
		},
	})

	checker := registry.NewReadinessChecker(reg, cli, nil, "2.20.0")
	ready, err := checker.IsReady(context.Background(), "datasciencepipelines")
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(ready).Should(BeTrue())
}

func TestReadinessChecker_NotReadyVersionMismatch(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	dsp := &componentApi.DataSciencePipelines{
		ObjectMeta: metav1.ObjectMeta{
			Name: componentApi.DataSciencePipelinesInstanceName,
		},
		Status: componentApi.DataSciencePipelinesStatus{
			Status: common.Status{
				Conditions: []common.Condition{
					{Type: status.ConditionTypeReady, Status: metav1.ConditionTrue},
				},
			},
			DataSciencePipelinesCommonStatus: componentApi.DataSciencePipelinesCommonStatus{
				ComponentReleaseStatus: common.ComponentReleaseStatus{
					Releases: []common.ComponentRelease{
						{Name: "platform", Version: "2.19.0"},
					},
				},
			},
		},
	}

	cli, err := fakeclient.New(
		fakeclient.WithObjects(dsp),
		fakeclient.WithGVKs(fakeclient.GVKMapping{
			GVK:   gvk.DataSciencePipelines,
			Scope: meta.RESTScopeRoot,
		}),
	)
	g.Expect(err).ShouldNot(HaveOccurred())

	reg := &registry.Registry{}
	reg.Add(&crComponentHandler{
		name:    "datasciencepipelines",
		enabled: true,
		cr: &componentApi.DataSciencePipelines{
			ObjectMeta: metav1.ObjectMeta{
				Name: componentApi.DataSciencePipelinesInstanceName,
			},
		},
	})

	checker := registry.NewReadinessChecker(reg, cli, nil, "2.20.0")
	ready, err := checker.IsReady(context.Background(), "datasciencepipelines")
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(ready).Should(BeFalse(), "component reporting old version should not be ready")
}

func TestReadinessChecker_ReadyWhenNoPlatformRelease(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	dsp := &componentApi.DataSciencePipelines{
		ObjectMeta: metav1.ObjectMeta{
			Name: componentApi.DataSciencePipelinesInstanceName,
		},
		Status: componentApi.DataSciencePipelinesStatus{
			Status: common.Status{
				Conditions: []common.Condition{
					{Type: status.ConditionTypeReady, Status: metav1.ConditionTrue},
				},
			},
		},
	}

	cli, err := fakeclient.New(
		fakeclient.WithObjects(dsp),
		fakeclient.WithGVKs(fakeclient.GVKMapping{
			GVK:   gvk.DataSciencePipelines,
			Scope: meta.RESTScopeRoot,
		}),
	)
	g.Expect(err).ShouldNot(HaveOccurred())

	reg := &registry.Registry{}
	reg.Add(&crComponentHandler{
		name:    "datasciencepipelines",
		enabled: true,
		cr: &componentApi.DataSciencePipelines{
			ObjectMeta: metav1.ObjectMeta{
				Name: componentApi.DataSciencePipelinesInstanceName,
			},
		},
	})

	checker := registry.NewReadinessChecker(reg, cli, nil, "2.20.0")
	ready, err := checker.IsReady(context.Background(), "datasciencepipelines")
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(ready).Should(BeTrue(), "component without platform release should fall through to Ready check")
}

func TestReadinessChecker_ReadyWhenEmptyPlatformVersion(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	dsp := &componentApi.DataSciencePipelines{
		ObjectMeta: metav1.ObjectMeta{
			Name: componentApi.DataSciencePipelinesInstanceName,
		},
		Status: componentApi.DataSciencePipelinesStatus{
			Status: common.Status{
				Conditions: []common.Condition{
					{Type: status.ConditionTypeReady, Status: metav1.ConditionTrue},
				},
			},
			DataSciencePipelinesCommonStatus: componentApi.DataSciencePipelinesCommonStatus{
				ComponentReleaseStatus: common.ComponentReleaseStatus{
					Releases: []common.ComponentRelease{
						{Name: "platform", Version: "2.19.0"},
					},
				},
			},
		},
	}

	cli, err := fakeclient.New(
		fakeclient.WithObjects(dsp),
		fakeclient.WithGVKs(fakeclient.GVKMapping{
			GVK:   gvk.DataSciencePipelines,
			Scope: meta.RESTScopeRoot,
		}),
	)
	g.Expect(err).ShouldNot(HaveOccurred())

	reg := &registry.Registry{}
	reg.Add(&crComponentHandler{
		name:    "datasciencepipelines",
		enabled: true,
		cr: &componentApi.DataSciencePipelines{
			ObjectMeta: metav1.ObjectMeta{
				Name: componentApi.DataSciencePipelinesInstanceName,
			},
		},
	})

	checker := registry.NewReadinessChecker(reg, cli, nil, "")
	ready, err := checker.IsReady(context.Background(), "datasciencepipelines")
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(ready).Should(BeTrue(), "empty platform version should skip version check")
}

func TestReadinessChecker_NotReadyWhenConditionFalse(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	dsp := &componentApi.DataSciencePipelines{
		ObjectMeta: metav1.ObjectMeta{
			Name: componentApi.DataSciencePipelinesInstanceName,
		},
		Status: componentApi.DataSciencePipelinesStatus{
			Status: common.Status{
				Conditions: []common.Condition{
					{Type: status.ConditionTypeReady, Status: metav1.ConditionFalse},
				},
			},
			DataSciencePipelinesCommonStatus: componentApi.DataSciencePipelinesCommonStatus{
				ComponentReleaseStatus: common.ComponentReleaseStatus{
					Releases: []common.ComponentRelease{
						{Name: "platform", Version: "2.20.0"},
					},
				},
			},
		},
	}

	cli, err := fakeclient.New(
		fakeclient.WithObjects(dsp),
		fakeclient.WithGVKs(fakeclient.GVKMapping{
			GVK:   gvk.DataSciencePipelines,
			Scope: meta.RESTScopeRoot,
		}),
	)
	g.Expect(err).ShouldNot(HaveOccurred())

	reg := &registry.Registry{}
	reg.Add(&crComponentHandler{
		name:    "datasciencepipelines",
		enabled: true,
		cr: &componentApi.DataSciencePipelines{
			ObjectMeta: metav1.ObjectMeta{
				Name: componentApi.DataSciencePipelinesInstanceName,
			},
		},
	})

	checker := registry.NewReadinessChecker(reg, cli, nil, "2.20.0")
	ready, err := checker.IsReady(context.Background(), "datasciencepipelines")
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(ready).Should(BeFalse(), "component with Ready=False should not be ready regardless of version")
}

func TestReadinessChecker_DisabledComponentIsReady(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	reg := &registry.Registry{}
	reg.Add(&crComponentHandler{
		name:    "disabled-comp",
		enabled: false,
	})

	checker := registry.NewReadinessChecker(reg, nil, nil, "2.20.0")
	ready, err := checker.IsReady(context.Background(), "disabled-comp")
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(ready).Should(BeTrue(), "disabled component should be considered ready")
}
