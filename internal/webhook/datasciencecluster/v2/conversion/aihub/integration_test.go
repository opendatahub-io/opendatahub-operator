package aihub_test

import (
	"fmt"
	"strings"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dscApi "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	v2webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/datasciencecluster/v2"
	v3webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/datasciencecluster/v3"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/envtestutil"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"

	. "github.com/onsi/gomega"
)

const aiHubIntegrationDSCName = "aihub-conversion"

func TestAIHubVersionedConversion(t *testing.T) {
	g := NewWithT(t)
	ctx, env, teardown := envtestutil.SetupEnvAndClient(t,
		[]envt.RegisterWebhooksFn{v2webhook.RegisterWebhooks, v3webhook.RegisterWebhooks},
		nil,
		envtestutil.DefaultWebhookTimeout,
	)
	t.Cleanup(teardown)

	g.Expect(env.ConfigureCRDConversion(ctx, "datascienceclusters.datasciencecluster.opendatahub.io")).To(Succeed())

	cli := env.Client()
	key := types.NamespacedName{Name: aiHubIntegrationDSCName}

	t.Run("v2 to v3", func(t *testing.T) {
		deleteAIHubDSC(t, cli, key)

		g := NewWithT(t)
		g.Expect(cli.Create(ctx, &dscv2.DataScienceCluster{
			ObjectMeta: metav1.ObjectMeta{Name: aiHubIntegrationDSCName},
			Spec: dscv2.DataScienceClusterSpec{
				Components: dscv2.Components{
					ModelRegistry: componentApi.DSCModelRegistry{
						ModelRegistryCommonSpec: componentApi.ModelRegistryCommonSpec{
							RegistriesNamespace: "model-registries",
						},
					},
				},
			},
		})).To(Succeed())

		actual := &dscApi.DataScienceCluster{}
		g.Eventually(func() error { return cli.Get(ctx, key, actual) }).Should(Succeed())
		g.Expect(actual.Spec.Components.AIHub).To(Equal(componentApi.DSCAIHub{
			AIHubCommonSpec: componentApi.AIHubCommonSpec{
				InstancesNamespace: "model-registries",
			},
		}))
	})

	t.Run("v3 to v2", func(t *testing.T) {
		deleteAIHubDSC(t, cli, key)

		g := NewWithT(t)
		g.Expect(cli.Create(ctx, &dscApi.DataScienceCluster{
			ObjectMeta: metav1.ObjectMeta{Name: aiHubIntegrationDSCName},
			Spec: dscApi.DataScienceClusterSpec{
				Components: dscApi.Components{
					AIHub: componentApi.DSCAIHub{
						AIHubCommonSpec: componentApi.AIHubCommonSpec{
							InstancesNamespace: "model-registries",
						},
					},
				},
			},
		})).To(Succeed())

		actual := &dscv2.DataScienceCluster{}
		g.Eventually(func() error { return cli.Get(ctx, key, actual) }).Should(Succeed())
		g.Expect(actual.Spec.Components.ModelRegistry).To(Equal(componentApi.DSCModelRegistry{
			ModelRegistryCommonSpec: componentApi.ModelRegistryCommonSpec{
				RegistriesNamespace: "model-registries",
			},
		}))
	})

	for _, version := range []string{"v2", "v3"} {
		for _, state := range []operatorv1.ManagementState{operatorv1.Managed, operatorv1.Removed, ""} {
			for _, namespace := range []struct {
				name    string
				value   string
				present bool
			}{
				{name: "omitted"},
				{name: "empty", present: true},
				{name: "explicit", present: true, value: registriesNamespace},
				{name: "maximum length", present: true, value: strings.Repeat("a", 63)},
			} {
				t.Run(fmt.Sprintf("%s admission/state=%s/namespace=%s", version, state, namespace.name), func(t *testing.T) {
					deleteAIHubDSC(t, cli, key)
					g := NewWithT(t)
					object := versionedAIHubDSC(version, state)
					if namespace.present {
						g.Expect(setAIHubNamespace(object, namespace.value)).To(Succeed())
					}
					g.Expect(cli.Create(ctx, object)).To(Succeed())
					expected := namespace.value
					// The v2 conversion omits an empty namespace in its JSON output,
					// so the v3 storage schema applies its default even when Removed.
					if !namespace.present || (expected == "" && (state == operatorv1.Managed || version == "v2")) {
						expected = componentApi.DefaultModelRegistriesNamespace
					}
					v3 := &dscApi.DataScienceCluster{}
					g.Expect(cli.Get(ctx, key, v3)).To(Succeed())
					g.Expect(v3.Spec.Components.AIHub.InstancesNamespace).To(Equal(expected))
					g.Expect(v3.Spec.Components.AIHub.ManagementState).To(Equal(state))
					v2 := &dscv2.DataScienceCluster{}
					g.Expect(cli.Get(ctx, key, v2)).To(Succeed())
					g.Expect(v2.Spec.Components.ModelRegistry.RegistriesNamespace).To(Equal(expected))
					g.Expect(v2.Spec.Components.ModelRegistry.ManagementState).To(Equal(state))
				})
			}
		}

		for _, namespace := range []string{"Invalid", "invalid_name", "invalid.name", "-invalid", "invalid-", strings.Repeat("a", 64)} {
			t.Run(fmt.Sprintf("%s rejects namespace=%s", version, namespace), func(t *testing.T) {
				deleteAIHubDSC(t, cli, key)
				g := NewWithT(t)
				object := versionedAIHubDSC(version, operatorv1.Managed)
				g.Expect(setAIHubNamespace(object, namespace)).To(Succeed())
				g.Expect(cli.Create(ctx, object)).To(MatchError(k8serr.IsInvalid, "IsInvalid"))
			})
		}

		t.Run(version+" namespace management transitions", func(t *testing.T) {
			deleteAIHubDSC(t, cli, key)
			g := NewWithT(t)
			object := versionedAIHubDSC(version, operatorv1.Managed)
			g.Expect(setAIHubNamespace(object, registriesNamespace)).To(Succeed())
			g.Expect(cli.Create(ctx, object)).To(Succeed())
			g.Expect(cli.Get(ctx, key, object)).To(Succeed())
			g.Expect(setAIHubNamespace(object, "changed-registries")).To(Succeed())
			g.Expect(cli.Update(ctx, object)).To(MatchError(k8serr.IsInvalid, "IsInvalid"))
			g.Expect(cli.Get(ctx, key, object)).To(Succeed())
			component, _ := aiHubVersionFields(version)
			g.Expect(unstructured.SetNestedField(object.Object, string(operatorv1.Removed), "spec", "components", component, "managementState")).To(Succeed())
			g.Expect(cli.Update(ctx, object)).To(Succeed())
			g.Expect(cli.Get(ctx, key, object)).To(Succeed())
			g.Expect(setAIHubNamespace(object, "changed-registries")).To(Succeed())
			g.Expect(cli.Update(ctx, object)).To(Succeed())
			v3 := &dscApi.DataScienceCluster{}
			g.Expect(cli.Get(ctx, key, v3)).To(Succeed())
			g.Expect(v3.Spec.Components.AIHub.InstancesNamespace).To(Equal("changed-registries"))
			g.Expect(v3.Spec.Components.AIHub.ManagementState).To(Equal(operatorv1.Removed))
		})
	}

	t.Run("v2 read modify write preserves v3 namespace and status", func(t *testing.T) {
		deleteAIHubDSC(t, cli, key)
		g := NewWithT(t)
		v3 := &dscApi.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Name: key.Name}}
		v3.Spec.Components.AIHub.ManagementState = operatorv1.Managed
		v3.Spec.Components.AIHub.InstancesNamespace = registriesNamespace
		g.Expect(cli.Create(ctx, v3)).To(Succeed())
		g.Expect(cli.Get(ctx, key, v3)).To(Succeed())
		v3.Status.Components.AIHub = componentApi.DSCAIHubStatus{
			ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Managed},
			AIHubCommonStatus: &componentApi.AIHubCommonStatus{
				InstancesNamespace:     "observed-registries",
				ComponentReleaseStatus: common.ComponentReleaseStatus{Releases: aiHubReleases()},
			},
		}
		v3.Status.Conditions = aiHubConditions("AIHubReady")
		g.Expect(cli.Status().Update(ctx, v3)).To(Succeed())
		original := v3.DeepCopy()
		v2 := &dscv2.DataScienceCluster{}
		g.Expect(cli.Get(ctx, key, v2)).To(Succeed())
		g.Expect(v2.Spec.Components.ModelRegistry.RegistriesNamespace).To(Equal(registriesNamespace))
		g.Expect(v2.Status.Components.ModelRegistry.RegistriesNamespace).To(Equal("observed-registries"))
		g.Expect(v2.Status.Components.ModelRegistry.Releases).To(Equal(aiHubReleases()))
		g.Expect(v2.Status.Conditions).To(BeComparableTo(aiHubConditions("ModelRegistryReady")))
		g.Expect(cli.Status().Update(ctx, v2)).To(Succeed())
		g.Expect(cli.Get(ctx, key, v2)).To(Succeed())
		v2.Spec.Components.Ray.ManagementState = operatorv1.Removed
		g.Expect(cli.Update(ctx, v2)).To(Succeed())
		g.Expect(cli.Get(ctx, key, v3)).To(Succeed())
		g.Expect(v3.Spec.Components.AIHub).To(Equal(original.Spec.Components.AIHub))
		g.Expect(v3.Status.Components.AIHub).To(Equal(original.Status.Components.AIHub))
		g.Expect(v3.Status.Conditions).To(BeComparableTo(original.Status.Conditions))
		g.Expect(v3.Spec.Components.Ray.ManagementState).To(Equal(operatorv1.Removed))
	})
}

func aiHubVersionFields(version string) (string, string) {
	if version == "v2" {
		return "modelregistry", "registriesNamespace"
	}
	return "aiHub", "instancesNamespace"
}

func versionedAIHubDSC(version string, state operatorv1.ManagementState) *unstructured.Unstructured {
	component, _ := aiHubVersionFields(version)
	settings := map[string]any{}
	if state != "" {
		settings["managementState"] = string(state)
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "datasciencecluster.opendatahub.io/" + version,
		"kind":       "DataScienceCluster",
		"metadata":   map[string]any{"name": aiHubIntegrationDSCName},
		"spec":       map[string]any{"components": map[string]any{component: settings}},
	}}
}

func setAIHubNamespace(object *unstructured.Unstructured, namespace string) error {
	component, field := aiHubVersionFields(object.GroupVersionKind().Version)
	return unstructured.SetNestedField(object.Object, namespace, "spec", "components", component, field)
}

func deleteAIHubDSC(t *testing.T, cli client.Client, key types.NamespacedName) {
	t.Helper()

	g := NewWithT(t)
	object := &dscv2.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Name: key.Name}}
	g.Eventually(func() error {
		if err := cli.Get(t.Context(), client.ObjectKeyFromObject(object), object); err != nil {
			return err
		}
		return cli.Delete(t.Context(), object)
	}).Should(MatchError(k8serr.IsNotFound, "IsNotFound"))
}
