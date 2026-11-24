package e2e_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/opendatahub-io/opendatahub-operator/pkg/clusterhealth"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/e2e/internal/moduleinventory"
)

func TestDAGReportStatusExtraction(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"status": map[string]any{
			"conditions": []any{
				map[string]any{"type": "Ready", "status": string(metav1.ConditionFalse)},
			},
			"releases": []any{
				map[string]any{"name": "platform", "version": "2.30.0"},
			},
		},
	}}

	assert.Equal(t, string(metav1.ConditionFalse), moduleConditionStatus(obj, "Ready"))
	assert.Equal(t, "2.30.0", moduleReleaseVersion(obj))
	assert.Empty(t, moduleConditionStatus(obj, "Degraded"))
}

func TestTentativeModuleAttributionPrecedence(t *testing.T) {
	assert.False(t, shouldApplyTentativeModuleAttribution("infrastructure", "kserve"),
		"infrastructure classification must not be overwritten")
	assert.True(t, shouldApplyTentativeModuleAttribution("unknown", "kserve"))
	assert.True(t, shouldApplyTentativeModuleAttribution("product", "kserve"))
	assert.False(t, shouldApplyTentativeModuleAttribution("product", ""))
}

func TestDAGClassificationReportWriteGuard(t *testing.T) {
	t.Parallel()

	assert.True(t, shouldWriteDAGClassificationReport(true, false))
	assert.False(t, shouldWriteDAGClassificationReport(true, true), "a child report must not be overwritten by parent cleanup")
	assert.False(t, shouldWriteDAGClassificationReport(false, false))
}

func TestMergeDeploymentEnvVarsPreservesValueFrom(t *testing.T) {
	t.Parallel()

	desired := corev1.EnvVar{
		Name: "SECRET_TOKEN",
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "operator-secret"},
				Key:                  "token",
			},
		},
	}
	existing := []any{
		map[string]any{"name": "KEEP", "value": "unchanged"},
		map[string]any{"name": desired.Name, "value": "temporary"},
	}

	merged, err := mergeDeploymentEnvVars(existing, []corev1.EnvVar{desired}, map[string]bool{desired.Name: true})
	require.NoError(t, err)

	var container corev1.Container
	require.NoError(t, runtime.DefaultUnstructuredConverter.FromUnstructured(
		map[string]any{"env": merged},
		&container,
	))
	require.Len(t, container.Env, 2)
	assert.Equal(t, corev1.EnvVar{Name: "KEEP", Value: "unchanged"}, container.Env[0])
	assert.Equal(t, desired, container.Env[1])

	restored, ok := merged[1].(map[string]any)
	require.True(t, ok)
	assert.NotContains(t, restored, "value", "value and valueFrom must not be emitted together")
	assert.Contains(t, restored, "valueFrom")
}

func TestCorrelateModuleImagePullFailure(t *testing.T) {
	t.Parallel()

	const (
		appsNamespace     = "redhat-ods-applications"
		operatorNamespace = "redhat-ods-operator"
		modulePod         = "kserve-controller-abc"
		operatorPod       = "rhods-operator-abc"
		moduleContainer   = "manager"
		operatorContainer = "manager"
	)
	moduleDeployments := []moduleDeploymentEvidence{{
		module:    "kserve",
		namespace: appsNamespace,
		selector:  map[string]string{"app": "kserve-controller"},
	}}

	newReport := func(moduleWaiting string) *clusterhealth.Report {
		infraWaiting := `ImagePullBackOff manifest unknown: quay.io/platform/operator:missing`
		return &clusterhealth.Report{
			Pods: clusterhealth.SectionResult[clusterhealth.PodsSection]{
				Data: clusterhealth.PodsSection{
					ByNamespace: map[string][]clusterhealth.PodInfo{
						operatorNamespace: {{
							Namespace: operatorNamespace,
							Name:      operatorPod,
							Containers: []clusterhealth.ContainerInfo{{
								Name:    operatorContainer,
								Waiting: infraWaiting,
							}},
						}},
						appsNamespace: {{
							Namespace: appsNamespace,
							Name:      modulePod,
							Containers: []clusterhealth.ContainerInfo{{
								Name:    moduleContainer,
								Waiting: moduleWaiting,
							}},
						}},
					},
					Data: []corev1.Pod{
						{
							ObjectMeta: metav1.ObjectMeta{Namespace: operatorNamespace, Name: operatorPod, Labels: map[string]string{"app": "operator"}},
						},
						{
							ObjectMeta: metav1.ObjectMeta{Namespace: appsNamespace, Name: modulePod, Labels: map[string]string{"app": "kserve-controller"}},
						},
					},
				},
			},
		}
	}

	tests := []struct {
		name          string
		moduleWaiting string
		evidence      string
		wantModule    string
		wantInvalid   bool
	}{
		{
			name:          "unrelated infrastructure pull is not attributed to module",
			moduleWaiting: `ImagePullBackOff Back-off pulling quay.io/kserve/controller:latest`,
			evidence:      `container rhods-operator-abc/manager waiting: ImagePullBackOff manifest unknown: quay.io/platform/operator:missing`,
		},
		{
			name:          "exact invalid module image is attributed",
			moduleWaiting: `ErrImagePull manifest unknown: quay.io/kserve/controller:missing`,
			evidence:      `container kserve-controller-abc/manager waiting: ErrImagePull manifest unknown: quay.io/kserve/controller:missing`,
			wantModule:    "kserve",
			wantInvalid:   true,
		},
		{
			name:          "exact transient module pull remains infrastructure",
			moduleWaiting: `ImagePullBackOff dial tcp: i/o timeout`,
			evidence:      `container kserve-controller-abc/manager waiting: ImagePullBackOff dial tcp: i/o timeout`,
			wantModule:    "kserve",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			attribution := correlateModuleImagePullFailure(newReport(tt.moduleWaiting), []string{tt.evidence}, moduleDeployments)
			assert.Equal(t, tt.wantModule, attribution.module)
			assert.Equal(t, tt.wantInvalid, attribution.invalidImage)
		})
	}

	t.Run("ambiguous pod evidence fails closed", func(t *testing.T) {
		t.Parallel()

		waiting := `ErrImagePull manifest unknown: quay.io/kserve/controller:missing`
		report := newReport(waiting)
		operatorInfo := report.Pods.Data.ByNamespace[operatorNamespace][0]
		operatorInfo.Name = modulePod
		operatorInfo.Containers[0].Waiting = waiting
		report.Pods.Data.ByNamespace[operatorNamespace][0] = operatorInfo
		report.Pods.Data.Data[0].Name = modulePod

		attribution := correlateModuleImagePullFailure(
			report,
			[]string{`container kserve-controller-abc/manager waiting: ErrImagePull manifest unknown: quay.io/kserve/controller:missing`},
			moduleDeployments,
		)
		assert.Empty(t, attribution.module)
		assert.False(t, attribution.invalidImage)
	})
}

func TestModuleHealthFromList(t *testing.T) {
	healthy := unstructured.Unstructured{Object: map[string]any{
		"status": map[string]any{"conditions": []any{
			map[string]any{"type": status.ConditionTypeReady, "status": string(metav1.ConditionTrue)},
		}},
	}}
	degraded := healthy.DeepCopy()
	degraded.Object["status"] = map[string]any{"conditions": []any{
		map[string]any{"type": status.ConditionTypeReady, "status": string(metav1.ConditionTrue)},
		map[string]any{"type": status.ConditionTypeDegraded, "status": string(metav1.ConditionTrue)},
	}}
	noMatch := &meta.NoKindMatchError{GroupKind: schema.GroupKind{Group: "example.io", Kind: "Module"}}
	missingCRD := k8serr.NewNotFound(schema.GroupResource{Group: "example.io", Resource: "modules"}, "")
	forbidden := k8serr.NewForbidden(schema.GroupResource{Group: "example.io", Resource: "modules"}, "", errors.New("denied"))

	assert.Equal(t, moduleHealthHealthy, moduleHealthFromList(nil, []unstructured.Unstructured{healthy}))
	assert.Equal(t, moduleHealthUnknown, moduleHealthFromList(nil, nil), "missing CR with an established CRD is a platform orchestration failure")
	assert.Equal(t, moduleHealthUnknown, moduleHealthFromList(nil, []unstructured.Unstructured{healthy, healthy}), "multiple singleton CRs are invalid platform state")
	assert.Equal(t, moduleHealthUnhealthy, moduleHealthFromList(noMatch, nil), "NoMatch is module-owned evidence")
	assert.Equal(t, moduleHealthUnhealthy, moduleHealthFromList(missingCRD, nil), "missing CRD is module-owned evidence")
	assert.Equal(t, moduleHealthUnhealthy, moduleHealthFromList(nil, []unstructured.Unstructured{*degraded}))
	assert.Equal(t, moduleHealthUnknown, moduleHealthFromList(errors.New("API server unavailable"), nil),
		"ordinary list failures are not module-owned evidence")
	assert.Equal(t, moduleHealthUnknown, moduleHealthFromList(forbidden, nil),
		"generic Kubernetes API errors are not module-owned evidence")
}

func TestModuleInstallationHealthStages(t *testing.T) {
	healthyCR := unstructured.Unstructured{Object: map[string]any{
		"status": map[string]any{
			"conditions": []any{map[string]any{"type": status.ConditionTypeReady, "status": string(metav1.ConditionTrue)}},
			"releases":   []any{map[string]any{"name": "platform", "version": "3.0.0"}},
		},
	}}
	healthyDeployment := &unstructured.Unstructured{Object: map[string]any{
		"status": map[string]any{"availableReplicas": int64(1)},
	}}
	missingDeployment := k8serr.NewNotFound(schema.GroupResource{Group: "apps", Resource: "deployments"}, "module-controller")
	noMatch := &meta.NoKindMatchError{GroupKind: schema.GroupKind{Group: "example.io", Kind: "Module"}}

	tests := []struct {
		name          string
		listErr       error
		items         []unstructured.Unstructured
		deploymentErr error
		deployment    *unstructured.Unstructured
		expected      string
		wantHealth    moduleHealth
		wantStage     string
	}{
		{
			name:       "healthy",
			items:      []unstructured.Unstructured{healthyCR},
			deployment: healthyDeployment,
			expected:   "3.0.0",
			wantHealth: moduleHealthHealthy,
		},
		{
			name:       "missing CRD",
			listErr:    noMatch,
			wantHealth: moduleHealthUnhealthy,
			wantStage:  "crd-establishment",
		},
		{
			name:          "missing controller deployment",
			items:         []unstructured.Unstructured{healthyCR},
			deploymentErr: missingDeployment,
			deployment:    &unstructured.Unstructured{},
			wantHealth:    moduleHealthUnhealthy,
			wantStage:     "controller-deployment",
		},
		{
			name:       "stale platform release",
			items:      []unstructured.Unstructured{healthyCR},
			deployment: healthyDeployment,
			expected:   "4.0.0",
			wantHealth: moduleHealthUnhealthy,
			wantStage:  "release-handshake",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := moduleInstallationHealth(tt.listErr, tt.items, tt.deploymentErr, tt.deployment, tt.expected)
			assert.Equal(t, tt.wantHealth, got.health)
			assert.Equal(t, tt.wantStage, got.stage)
		})
	}
}

func TestFirstUnhealthyRunlevelIgnoresBlockedLaterRunlevels(t *testing.T) {
	candidate := firstUnhealthyRunlevel([]managedModuleHealth{
		{name: "dashboard", runlevel: 20, health: moduleHealthHealthy},
		{name: "workbenches", runlevel: 20, health: moduleHealthUnhealthy, stage: "crd-establishment", message: "missing CRD"},
		{name: "kserve", runlevel: 31, health: moduleHealthUnknown},
		{name: "spark", runlevel: 32, health: moduleHealthUnknown},
	})
	assert.Equal(t, "workbenches", candidate.module)
	assert.Equal(t, "crd-establishment", candidate.stage)
	assert.Contains(t, candidate.boundary, "runlevel 20")
	assert.Contains(t, candidate.message, "later runlevels may be blocked")
}

func TestFirstUnhealthyRunlevelRequiresUnambiguousFailure(t *testing.T) {
	tests := []struct {
		name    string
		modules []managedModuleHealth
	}{
		{
			name: "unknown peer in first failing runlevel",
			modules: []managedModuleHealth{
				{name: "dashboard", runlevel: 20, health: moduleHealthUnknown},
				{name: "workbenches", runlevel: 20, health: moduleHealthUnhealthy},
			},
		},
		{
			name: "multiple failures in first failing runlevel",
			modules: []managedModuleHealth{
				{name: "dashboard", runlevel: 20, health: moduleHealthUnhealthy},
				{name: "workbenches", runlevel: 20, health: moduleHealthUnhealthy},
			},
		},
		{
			name: "unknown earlier runlevel blocks later attribution",
			modules: []managedModuleHealth{
				{name: "dashboard", runlevel: 20, health: moduleHealthUnknown},
				{name: "kserve", runlevel: 31, health: moduleHealthUnhealthy},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Empty(t, firstUnhealthyRunlevel(tt.modules).module)
		})
	}
}

func TestExpectsModuleCRDocumentsOnlyException(t *testing.T) {
	assert.False(t, expectsModuleCR(componentEntry{name: componentApi.KueueComponentName}))
	assert.True(t, expectsModuleCR(componentEntry{name: componentApi.KserveComponentName}))
}

func TestRegisteredModulesDeclareControllerDeploymentName(t *testing.T) {
	inventory, err := moduleinventory.Builtins()
	require.NoError(t, err)

	for _, module := range inventory.All() {
		named, ok := module.Handler.(interface{ GetDeploymentName() string })
		require.Truef(t, ok, "registered module %s does not expose its controller Deployment name", module.Name)
		assert.NotEmptyf(t, named.GetDeploymentName(), "registered module %s has an empty controller Deployment name", module.Name)
	}
}

func TestDAGInventoryCoversRegisteredProductionModules(t *testing.T) {
	inventory, err := moduleinventory.Builtins()
	require.NoError(t, err)

	dagGVKs := make(map[schema.GroupVersionKind]string)
	for _, batch := range dagBatches {
		for _, component := range batch.components {
			dagGVKs[component.gvk] = component.name
		}
	}
	intentionalExclusions := map[string]string{
		componentApi.DatabaseServiceComponentName: "DatabaseService is not required in every supported DAG environment",
	}
	for _, module := range inventory.All() {
		if reason, excluded := intentionalExclusions[module.Name]; excluded {
			assert.NotEmpty(t, reason)
			continue
		}
		_, found := dagGVKs[module.Handler.GetGVK()]
		assert.Truef(t, found, "registered production module %s (%s) is absent from DAG coverage", module.Name, module.Handler.GetGVK())
	}
}
