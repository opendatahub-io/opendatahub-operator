/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

//nolint:testpackage // Use private native-client seams rather than public test hooks.
package storagemigration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensions "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/yaml"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"

	. "github.com/onsi/gomega"
)

func integrationCRD(group string, plural string, kind string) *apiextensionsv1.CustomResourceDefinition {
	schema := &apiextensionsv1.JSONSchemaProps{
		Type: "object",
		Properties: map[string]apiextensionsv1.JSONSchemaProps{
			"spec": {
				Type:                   "object",
				XPreserveUnknownFields: new(true),
			},
			"status": {
				Type:                   "object",
				XPreserveUnknownFields: new(true),
			},
		},
	}

	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: plural + "." + group,
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: group,
			Scope: apiextensionsv1.ClusterScoped,
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural: plural,
				Kind:   kind,
			},
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name:    "v1alpha1",
					Served:  true,
					Storage: true,
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: schema,
					},
					Subresources: &apiextensionsv1.CustomResourceSubresources{
						Status: &apiextensionsv1.CustomResourceSubresourceStatus{},
					},
				},
			},
		},
	}
}

// conflictDefinitions changes metadata after the engine's safety read, before its patch.
type conflictDefinitions struct {
	apiextensions.CustomResourceDefinitionInterface
}

func (c conflictDefinitions) Patch(
	ctx context.Context,
	name string,
	patchType types.PatchType,
	data []byte,
	options metav1.PatchOptions,
	subresources ...string,
) (*apiextensionsv1.CustomResourceDefinition, error) {
	current, err := c.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	current.Annotations = map[string]string{"concurrent-writer": "changed"}
	if _, err = c.Update(ctx, current, metav1.UpdateOptions{}); err != nil {
		return nil, err
	}

	patched, err := c.CustomResourceDefinitionInterface.Patch(ctx, name, patchType, data, options, subresources...)
	if err != nil {
		return nil, err
	}

	return patched, nil
}

func TestStorageMigrationIntegration(t *testing.T) {
	g := NewWithT(t)

	api := migrationAPIs[3]
	target := integrationCRD("datasciencecluster.opendatahub.io", "datascienceclusters", "DataScienceCluster")
	target.Spec.Versions[0].Name = "v2"

	v3 := *target.Spec.Versions[0].DeepCopy()
	v3.Name = "v3"
	v3.Storage = false
	target.Spec.Versions = append(target.Spec.Versions, v3)

	migrationCRD := integrationCRD(api.Group, api.Resource, requestKind)
	resources.SetAnnotation(migrationCRD, apiextensionsv1.KubeAPIApprovedAnnotation, "unapproved, test fixture")

	environment := &envtest.Environment{
		UseExistingCluster: new(false),
		CRDs: []*apiextensionsv1.CustomResourceDefinition{
			target,
			migrationCRD,
		},
	}
	t.Cleanup(func() {
		g.Expect(environment.Stop()).To(Succeed())
	})
	config, err := environment.Start()
	g.Expect(err).To(Succeed())

	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()

	definitions, err := apiextensions.NewForConfig(config)
	g.Expect(err).To(Succeed())

	requests, err := dynamic.NewForConfig(config)
	g.Expect(err).To(Succeed())

	crds := definitions.CustomResourceDefinitions()

	// Store a real fixture at v2 before switching the CRD's storage version to v3.
	objects := requests.Resource(schema.GroupVersionResource{
		Group:    target.Spec.Group,
		Version:  "v2",
		Resource: target.Spec.Names.Plural,
	})
	fixture := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": target.Spec.Group + "/v2",
			"kind":       target.Spec.Names.Kind,
			"metadata":   map[string]any{"name": "example"},
			"spec":       map[string]any{"data": "preserve me"},
		},
	}
	object, err := objects.Create(ctx, fixture, metav1.CreateOptions{})
	g.Expect(err).To(Succeed())

	target, err = crds.Get(ctx, target.Name, metav1.GetOptions{})
	g.Expect(err).To(Succeed())

	target.Spec.Versions[0].Storage = false
	target.Spec.Versions[1].Storage = true
	target, err = crds.Update(ctx, target, metav1.UpdateOptions{})
	g.Expect(err).To(Succeed())

	resetHistory := func(t *testing.T) *apiextensionsv1.CustomResourceDefinition {
		t.Helper()
		g := NewWithT(t)

		current, getErr := crds.Get(ctx, target.Name, metav1.GetOptions{})
		g.Expect(getErr).To(Succeed())

		current.Status.StoredVersions = []string{"v2", "v3"}
		current, updateErr := crds.UpdateStatus(ctx, current, metav1.UpdateOptions{})
		g.Expect(updateErr).To(Succeed())

		return current
	}

	for _, platform := range []string{"odh", "rhoai", "denied"} {
		t.Run(platform, func(t *testing.T) {
			g := NewWithT(t)

			original := resetHistory(t)

			userConfig := grantMigrationRole(ctx, t, environment, platform)

			userDefinitions, userErr := apiextensions.NewForConfig(userConfig)
			g.Expect(userErr).To(Succeed())

			userRequests, userErr := dynamic.NewForConfig(userConfig)
			g.Expect(userErr).To(Succeed())

			eventClient, userErr := kubernetes.NewForConfig(userConfig)
			g.Expect(userErr).To(Succeed())

			recording, stopRecording := context.WithCancel(ctx)
			broadcaster := events.NewBroadcaster(&events.EventSinkImpl{
				Interface: eventClient.EventsV1(),
			})
			g.Expect(broadcaster.StartRecordingToSinkWithContext(recording)).To(Succeed())
			t.Cleanup(func() {
				stopRecording()
				broadcaster.Shutdown()
			})

			reporter := "storage-migration-" + platform
			recorder := broadcaster.NewRecorder(runtime.NewScheme(), reporter)

			g.Eventually(func() error {
				_, getErr := userDefinitions.CustomResourceDefinitions().Get(ctx, target.Name, metav1.GetOptions{})
				return getErr
			}).WithContext(ctx).WithTimeout(5 * time.Second).WithPolling(10 * time.Millisecond).Should(Succeed())

			// The generated role must not allow status patches on unrelated CRDs.
			_, patchErr := userDefinitions.CustomResourceDefinitions().Patch(ctx, migrationCRD.Name, types.MergePatchType,
				[]byte(`{"status":{"storedVersions":["v1alpha1"]}}`), metav1.PatchOptions{}, "status")
			g.Expect(k8serr.IsForbidden(patchErr)).To(BeTrue(), "role %s must not patch status of CRD %s: %v", platform, migrationCRD.Name, patchErr)

			m := migration{
				crdName:     target.Name,
				definitions: userDefinitions.CustomResourceDefinitions(),
				requests:    userRequests,
				discovery:   testDiscovery(api),
				interval:    time.Millisecond,
				recorder:    recorder,
			}
			attempt, stop := context.WithTimeout(ctx, 10*time.Second)
			done := make(chan error, 1)

			var workers sync.WaitGroup
			workers.Go(func() {
				runErr := m.execute(attempt)
				done <- runErr
			})

			// Always join the engine before shutting down its API server, even on assertion failure.
			t.Cleanup(func() {
				stop()
				workers.Wait()
			})

			expected, buildErr := newRequest(original, "v3", api)
			g.Expect(buildErr).To(Succeed())
			name := expected.GetName()
			var request *unstructured.Unstructured

			g.Eventually(func(g Gomega) {
				var getErr error
				request, getErr = requests.Resource(api).Get(attempt, name, metav1.GetOptions{})
				g.Expect(getErr).To(Succeed())
			}).WithContext(attempt).WithTimeout(5 * time.Second).WithPolling(time.Millisecond).Should(Succeed())

			beforeStatus, getErr := crds.Get(ctx, target.Name, metav1.GetOptions{})
			g.Expect(getErr).To(Succeed())
			g.Expect(beforeStatus.Status.StoredVersions).To(Equal(original.Status.StoredVersions), "pending request cannot prune")

			setCondition(request, "Succeeded")
			_, statusErr := requests.Resource(api).UpdateStatus(ctx, request, metav1.UpdateOptions{})
			g.Expect(statusErr).To(Succeed())

			var runErr error
			g.Eventually(done).WithContext(attempt).Should(Receive(&runErr))

			after, getErr := crds.Get(ctx, target.Name, metav1.GetOptions{})
			g.Expect(getErr).To(Succeed())
			g.Expect(after.Spec).To(Equal(original.Spec))
			g.Expect(after.Status.Conditions).To(Equal(original.Status.Conditions))

			if platform == "denied" {
				g.Expect(k8serr.IsForbidden(runErr)).To(BeTrue(), "%v", runErr)
				g.Expect(after.Status.StoredVersions).To(Equal(original.Status.StoredVersions))
				_, getErr = requests.Resource(api).Get(ctx, name, metav1.GetOptions{})
				g.Expect(getErr).To(Succeed())
				g.Expect(requests.Resource(api).Delete(ctx, name, metav1.DeleteOptions{})).To(Succeed())
				expectMigrationEvent(ctx, t, config, reporter, failedReason, original)
				return
			}

			g.Expect(runErr).To(Succeed())
			g.Expect(after.Status.StoredVersions).To(Equal([]string{"v3"}))
			_, getErr = requests.Resource(api).Get(ctx, name, metav1.GetOptions{})
			g.Expect(k8serr.IsNotFound(getErr)).To(BeTrue())

			for _, reason := range []string{
				validatedReason,
				discoveredReason,
				succeededReason,
				cleanedUpReason,
			} {
				expectMigrationEvent(ctx, t, config, reporter, reason, original)
			}

			runErr = m.execute(ctx)
			g.Expect(runErr).To(Succeed())
			again, getErr := crds.Get(ctx, target.Name, metav1.GetOptions{})
			g.Expect(getErr).To(Succeed())
			g.Expect(again.ResourceVersion).To(Equal(after.ResourceVersion), "repeat no-op must not write")
			expectMigrationEvent(ctx, t, config, reporter, EventReasonNotNeeded, original)

			preserved, getErr := objects.Get(ctx, object.GetName(), metav1.GetOptions{})
			g.Expect(getErr).To(Succeed())
			g.Expect(preserved.GetResourceVersion()).To(Equal(object.GetResourceVersion()))
			g.Expect(preserved.Object["spec"]).To(Equal(object.Object["spec"]))

			assertMigrationEventAccess(ctx, t, userConfig, target)
		})
	}

	t.Run("missing CRD event", func(t *testing.T) {
		testMissingCRDEvent(ctx, t, config)
	})

	for _, condition := range []string{"Failed", "Succeeded"} {
		t.Run(condition, func(t *testing.T) {
			original := resetHistory(t)
			testTerminalRequest(ctx, t, crds, requests, original, condition)
		})
	}

	t.Run("generation changed", func(t *testing.T) {
		original := resetHistory(t)
		testGenerationChanged(ctx, t, crds, original)
	})
}

func testGenerationChanged(
	ctx context.Context,
	t *testing.T,
	crds apiextensions.CustomResourceDefinitionInterface,
	original *apiextensionsv1.CustomResourceDefinition,
) {
	t.Helper()
	g := NewWithT(t)

	changed := original.DeepCopy()
	changed.Spec.Names.ShortNames = []string{"svmtest"}
	changed, err := crds.Update(ctx, changed, metav1.UpdateOptions{})
	g.Expect(err).To(Succeed())
	g.Expect(changed.Generation).To(BeNumerically(">", original.Generation))

	err = pruneStorageHistory(ctx, crds, original, "v3")
	g.Expect(err).To(MatchError(ContainSubstring("generation changed")))

	after, err := crds.Get(ctx, original.Name, metav1.GetOptions{})
	g.Expect(err).To(Succeed())
	g.Expect(after.Status.StoredVersions).To(Equal(original.Status.StoredVersions))
}

func testMissingCRDEvent(ctx context.Context, t *testing.T, config *rest.Config) {
	t.Helper()
	g := NewWithT(t)

	client, err := kubernetes.NewForConfig(config)
	g.Expect(err).To(Succeed())

	recording, stopRecording := context.WithCancel(ctx)
	broadcaster := events.NewBroadcaster(&events.EventSinkImpl{
		Interface: client.EventsV1(),
	})
	g.Expect(broadcaster.StartRecordingToSinkWithContext(recording)).To(Succeed())
	t.Cleanup(func() {
		stopRecording()
		broadcaster.Shutdown()
	})

	const reporter = "storage-migration-missing"
	missing := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "missing.example.test",
		},
	}
	recorder := broadcaster.NewRecorder(runtime.NewScheme(), reporter)

	g.Expect(Migrate(ctx, config, missing.Name, recorder)).To(Succeed())
	expectMigrationEvent(ctx, t, config, reporter, EventReasonNotNeeded, missing)
}

func grantMigrationRole(ctx context.Context, t *testing.T, environment *envtest.Environment, platform string) *rest.Config {
	t.Helper()
	g := NewWithT(t)

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	admin, err := kubernetes.NewForConfig(environment.Config)
	g.Expect(err).To(Succeed())

	rolePath := filepath.Join("..", "..", "config", "rbac", "role.yaml")
	if platform == "rhoai" {
		rolePath = filepath.Join("..", "..", "config", "rhoai", "rbac", "role.yaml")
	}

	data, readErr := os.ReadFile(rolePath)
	g.Expect(readErr).To(Succeed(), "generate both platform manifests before this suite")

	var role rbacv1.ClusterRole
	g.Expect(yaml.Unmarshal(data, &role)).To(Succeed())

	role.ObjectMeta = metav1.ObjectMeta{
		Name: "migration-test-" + platform,
	}
	if platform == "denied" {
		role.Rules = slices.DeleteFunc(role.Rules, func(rule rbacv1.PolicyRule) bool {
			return slices.Contains(rule.APIGroups, "apiextensions.k8s.io") && slices.Contains(rule.Resources, "customresourcedefinitions/status")
		})
	}

	_, createErr := admin.RbacV1().ClusterRoles().Create(ctx, &role, metav1.CreateOptions{})
	g.Expect(createErr).To(Succeed())

	user, userErr := environment.AddUser(envtest.User{
		Name: role.Name,
	}, environment.Config)
	g.Expect(userErr).To(Succeed())

	binding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: role.Name,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     role.Name,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:     "User",
				APIGroup: rbacv1.GroupName,
				Name:     role.Name,
			},
		},
	}
	_, bindingErr := admin.RbacV1().ClusterRoleBindings().Create(ctx, binding, metav1.CreateOptions{})
	g.Expect(bindingErr).To(Succeed())
	return user.Config()
}

func assertMigrationEventAccess(ctx context.Context, t *testing.T, config *rest.Config, target *apiextensionsv1.CustomResourceDefinition) {
	t.Helper()
	g := NewWithT(t)

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Check modern event create/series-patch authorization with the same identity.
	userClient, userErr := kubernetes.NewForConfig(config)
	g.Expect(userErr).To(Succeed())

	event, eventErr := userClient.EventsV1().Events("default").Create(ctx, &eventsv1.Event{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "migration-test-",
			Namespace:    "default",
		},
		EventTime: metav1.NewMicroTime(time.Now()),
		Regarding: corev1.ObjectReference{
			Kind:       gvk.CustomResourceDefinition.Kind,
			APIVersion: gvk.CustomResourceDefinition.GroupVersion().String(),
			Name:       target.Name,
			UID:        target.UID,
		},
		Reason:              failedReason,
		Action:              runAction,
		Type:                corev1.EventTypeWarning,
		Note:                "Authorization smoke test",
		ReportingController: EventReporter,
		ReportingInstance:   "migration-test",
	}, metav1.CreateOptions{})
	g.Expect(eventErr).To(Succeed())

	patch, marshalErr := json.Marshal(map[string]any{
		"series": eventsv1.EventSeries{
			Count:            2,
			LastObservedTime: metav1.NewMicroTime(time.Now()),
		},
	})
	g.Expect(marshalErr).To(Succeed())

	_, eventErr = userClient.EventsV1().Events("default").Patch(ctx, event.Name, types.MergePatchType, patch, metav1.PatchOptions{})
	g.Expect(eventErr).To(Succeed())
}

func expectMigrationEvent(
	ctx context.Context,
	t *testing.T,
	config *rest.Config,
	reporter string,
	reason string,
	crd *apiextensionsv1.CustomResourceDefinition,
) {
	t.Helper()
	g := NewWithT(t)
	client, err := kubernetes.NewForConfig(config)
	g.Expect(err).To(Succeed())

	eventType := corev1.EventTypeNormal
	if reason == failedReason {
		eventType = corev1.EventTypeWarning
	}

	regarding := corev1.ObjectReference{
		APIVersion: gvk.CustomResourceDefinition.GroupVersion().String(),
		Kind:       gvk.CustomResourceDefinition.Kind,
		Name:       crd.Name,
		UID:        crd.UID,
	}

	g.Eventually(func(g Gomega) {
		list, err := client.EventsV1().Events("default").List(ctx, metav1.ListOptions{})
		g.Expect(err).To(Succeed())
		g.Expect(list.Items).To(ContainElement(And(
			HaveField("ReportingController", Equal(reporter)),
			HaveField("Reason", Equal(reason)),
			HaveField("Type", Equal(eventType)),
			HaveField("Regarding", Equal(regarding)),
		)))
	}).WithContext(ctx).WithTimeout(5 * time.Second).WithPolling(10 * time.Millisecond).Should(Succeed())
}

func testTerminalRequest(
	ctx context.Context,
	t *testing.T,
	crds apiextensions.CustomResourceDefinitionInterface,
	requests dynamic.Interface,
	original *apiextensionsv1.CustomResourceDefinition,
	condition string,
) {
	t.Helper()
	g := NewWithT(t)

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	api := migrationAPIs[3]
	m := migration{
		crdName:     original.Name,
		definitions: crds,
		requests:    requests,
		discovery:   testDiscovery(api),
		interval:    time.Millisecond,
		recorder:    events.NewFakeRecorder(16),
	}

	expected, err := newRequest(original, "v3", api)
	g.Expect(err).To(Succeed())

	request, err := requests.Resource(api).Create(ctx, expected, metav1.CreateOptions{})
	g.Expect(err).To(Succeed())

	setCondition(request, condition)
	request, err = requests.Resource(api).UpdateStatus(ctx, request, metav1.UpdateOptions{})
	g.Expect(err).To(Succeed())

	if condition == "Succeeded" {
		m.definitions = conflictDefinitions{
			crds,
		}
	}

	runErr := m.execute(ctx)
	g.Expect(runErr).To(HaveOccurred())

	if condition == "Succeeded" {
		g.Expect(k8serr.IsConflict(runErr)).To(BeTrue(), "%v", runErr)
	} else {
		g.Expect(runErr).To(MatchError(ContainSubstring("manually delete")))
	}

	after, err := crds.Get(ctx, original.Name, metav1.GetOptions{})
	g.Expect(err).To(Succeed())
	g.Expect(after.Status.StoredVersions).To(Equal(original.Status.StoredVersions))

	retained, err := requests.Resource(api).Get(ctx, request.GetName(), metav1.GetOptions{})
	g.Expect(err).To(Succeed())
	g.Expect(retained.GetResourceVersion()).To(Equal(request.GetResourceVersion()))

	g.Expect(requests.Resource(api).Delete(ctx, request.GetName(), metav1.DeleteOptions{})).To(Succeed())
}
