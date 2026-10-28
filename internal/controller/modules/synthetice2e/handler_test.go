package synthetice2e_test

import (
	"context"
	"path/filepath"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/synthetice2e"
)

func TestHandler(t *testing.T) {
	h := synthetice2e.NewHandler()

	assert.Equal(t, synthetice2e.ModuleName, h.GetName())
	assert.False(t, h.IsEnabled(nil))
	assert.False(t, h.IsEnabled(&configApi.PlatformModules{}))
	assert.True(t, h.IsEnabled(&configApi.PlatformModules{
		SyntheticE2E: common.ManagementSpec{ManagementState: operatorv1.Managed},
	}))

	cr, err := h.BuildModuleCR(context.Background(), nil, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, synthetice2e.CRName, cr.GetName())
	assert.Equal(t, "synthetic.platform.opendatahub.io/v1alpha1", cr.GetAPIVersion())
	assert.Equal(t, "SyntheticModule", cr.GetKind())
	spec, found, err := unstructured.NestedMap(cr.Object, "spec")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "synthetic module ready", spec["message"])
}

func TestHelmChartUsesApplicationsNamespace(t *testing.T) {
	h := synthetice2e.NewHandler()
	chartsRoot := filepath.Join("..", "..", "..", "..", "tests", "e2e", "syntheticmodule", "charts")
	platform := &modules.PlatformContext{
		ApplicationsNamespace: "custom-applications",
		ChartsBasePath:        chartsRoot,
	}

	manifests := h.GetOperatorManifests(platform)
	require.Empty(t, manifests.Manifests)
	require.Len(t, manifests.HelmCharts, 1)
	chart := manifests.HelmCharts[0]
	assert.Equal(t, filepath.Join(chartsRoot, synthetice2e.ModuleName), chart.Chart)
	assert.Equal(t, synthetice2e.DeploymentName, chart.ReleaseName)
	assert.Equal(t, platform.ApplicationsNamespace, chart.ReleaseNamespace)

	values, err := chart.Values(t.Context())
	require.NoError(t, err)
	assert.Equal(t, platform.ApplicationsNamespace, values["namespace"])
}
