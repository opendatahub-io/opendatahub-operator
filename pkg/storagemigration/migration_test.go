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

//nolint:testpackage // Exercise private client seams without exporting a test-only API.
package storagemigration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsfake "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	discoveryfake "k8s.io/client-go/discovery/fake"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/rest"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/events"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"

	. "github.com/onsi/gomega"
)

func testCRD() *apiextensionsv1.CustomResourceDefinition {
	return &apiextensionsv1.CustomResourceDefinition{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apiextensions.k8s.io/v1",
			Kind:       "CustomResourceDefinition",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            "widgets.example.test",
			UID:             "original-crd",
			ResourceVersion: "12",
			Generation:      2,
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "example.test",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural: "widgets",
				Kind:   "Widget",
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name:   "v1",
					Served: true,
				},
				{
					Name:    "v2",
					Served:  true,
					Storage: true,
				},
			},
		},
		Status: apiextensionsv1.CustomResourceDefinitionStatus{
			StoredVersions: []string{"v1", "v2"},
		},
	}
}

func testDiscovery(apis ...schema.GroupVersionResource) *discoveryfake.FakeDiscovery {
	d := &discoveryfake.FakeDiscovery{
		Fake: &clienttesting.Fake{},
	}
	for _, api := range apis {
		d.Resources = append(d.Resources, &metav1.APIResourceList{
			GroupVersion: api.GroupVersion().String(),
			APIResources: []metav1.APIResource{
				{
					Name: api.Resource,
					Kind: requestKind,
				},
			},
		})
	}
	return d
}

func testMigration(crd *apiextensionsv1.CustomResourceDefinition, apis ...schema.GroupVersionResource) (migration, *apiextensionsfake.Clientset, *dynamicfake.FakeDynamicClient) {
	definitions := apiextensionsfake.NewSimpleClientset(crd)
	requests := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	requests.PrependReactor("create", "*", func(action clienttesting.Action) (bool, runtime.Object, error) {
		create, ok := action.(clienttesting.CreateAction)
		if !ok {
			return true, nil, fmt.Errorf("expected a create action, got %T", action)
		}

		object, ok := create.GetObject().(*unstructured.Unstructured)
		if !ok {
			return true, nil, fmt.Errorf("expected an unstructured request, got %T", create.GetObject())
		}

		request := object.DeepCopy()
		request.SetUID("request-uid")
		setCondition(request, "Succeeded")
		if err := requests.Tracker().Create(action.GetResource(), request, action.GetNamespace()); err != nil {
			return true, nil, err
		}

		return true, request, nil
	})
	return migration{
		crdName:     crd.Name,
		definitions: definitions.ApiextensionsV1().CustomResourceDefinitions(),
		requests:    requests,
		discovery:   testDiscovery(apis...),
		interval:    time.Millisecond,
		recorder:    events.NewFakeRecorder(16),
	}, definitions, requests
}

func setCondition(request *unstructured.Unstructured, condition string) {
	request.Object["status"] = map[string]any{"conditions": []any{map[string]any{"type": condition, "status": "True"}}}
}

func seedRequest(
	t *testing.T,
	requests *dynamicfake.FakeDynamicClient,
	crd *apiextensionsv1.CustomResourceDefinition,
	api schema.GroupVersionResource,
	condition string,
) *unstructured.Unstructured {
	t.Helper()
	g := NewWithT(t)

	request, err := newRequest(crd, crd.Spec.Versions[len(crd.Spec.Versions)-1].Name, api)
	g.Expect(err).To(Succeed())
	request.SetUID("request-uid")
	if condition != "" {
		setCondition(request, condition)
	}
	g.Expect(requests.Tracker().Create(api, request, "")).To(Succeed())
	return request
}

func TestNoOp(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "current", true: "missing"}[missing], func(t *testing.T) {
			g := NewWithT(t)

			definitions := apiextensionsfake.NewSimpleClientset()
			crd := testCRD()
			crd.Status.StoredVersions = []string{"v2"}
			if !missing {
				g.Expect(definitions.Tracker().Add(crd)).To(Succeed())
			}
			// Nil discovery/request clients fail immediately if a no-op does extra work.
			recorder := events.NewFakeRecorder(8)
			m := migration{
				crdName:     crd.Name,
				definitions: definitions.ApiextensionsV1().CustomResourceDefinitions(),
				recorder:    recorder,
			}
			err := m.execute(t.Context())
			g.Expect(err).To(Succeed())
			g.Expect(m.request).To(BeNil())
			g.Expect(definitions.Actions()).To(HaveLen(1))
			g.Expect(definitions.Actions()[0].GetVerb()).To(Equal("get"))
			g.Expect(recorder.Events).To(HaveLen(1))
			g.Expect(<-recorder.Events).To(ContainSubstring("Normal " + EventReasonNotNeeded))
		})
	}
}

func TestInvalidCRD(t *testing.T) {
	tests := map[string]func(*apiextensionsv1.CustomResourceDefinition){
		"empty history":           func(c *apiextensionsv1.CustomResourceDefinition) { c.Status.StoredVersions = nil },
		"duplicate history":       func(c *apiextensionsv1.CustomResourceDefinition) { c.Status.StoredVersions = []string{"v2", "v2"} },
		"empty version":           func(c *apiextensionsv1.CustomResourceDefinition) { c.Status.StoredVersions = []string{"", "v2"} },
		"missing storage history": func(c *apiextensionsv1.CustomResourceDefinition) { c.Status.StoredVersions = []string{"v1"} },
		"two storage versions":    func(c *apiextensionsv1.CustomResourceDefinition) { c.Spec.Versions[0].Storage = true },
		"unserved storage":        func(c *apiextensionsv1.CustomResourceDefinition) { c.Spec.Versions[1].Served = false },
		"missing storage":         func(c *apiextensionsv1.CustomResourceDefinition) { c.Spec.Versions[1].Storage = false },
		"missing UID":             func(c *apiextensionsv1.CustomResourceDefinition) { c.UID = "" },
		"deleting":                func(c *apiextensionsv1.CustomResourceDefinition) { c.DeletionTimestamp = new(metav1.Now()) },
	}
	for name, modify := range tests {
		t.Run(name, func(t *testing.T) {
			g := NewWithT(t)

			crd := testCRD()
			modify(crd)
			m, definitions, requests := testMigration(crd, migrationAPIs[0])
			err := m.execute(t.Context())
			g.Expect(err).To(HaveOccurred())
			g.Expect(definitions.Actions()).To(HaveLen(1))
			g.Expect(requests.Actions()).To(BeEmpty())
		})
	}
}

func TestMigrateAPIs(t *testing.T) {
	for _, api := range migrationAPIs {
		t.Run(api.GroupVersion().String(), func(t *testing.T) {
			g := NewWithT(t)

			crd := testCRD()
			m, definitions, requests := testMigration(crd, api)
			err := m.execute(t.Context())
			g.Expect(err).To(Succeed())
			g.Expect(m.request.GetName()).NotTo(BeEmpty())
			current, err := definitions.ApiextensionsV1().CustomResourceDefinitions().Get(t.Context(), crd.Name, metav1.GetOptions{})
			g.Expect(err).To(Succeed())
			g.Expect(current.Status.StoredVersions).To(Equal([]string{"v2"}))
			g.Expect(current.Spec).To(Equal(crd.Spec))
			for _, action := range definitions.Actions() {
				if action.GetVerb() != "patch" {
					continue
				}
				patch, ok := action.(clienttesting.PatchAction)
				g.Expect(ok).To(BeTrue())
				g.Expect(patch.GetSubresource()).To(Equal("status"))
				g.Expect(patch.GetPatchType()).To(Equal(types.MergePatchType))
				g.Expect(string(patch.GetPatch())).To(MatchJSON(`{"metadata":{"resourceVersion":"12"},"status":{"storedVersions":["v2"]}}`))
			}
			for _, action := range requests.Actions() {
				g.Expect(action.GetResource().Resource).To(Equal("storageversionmigrations"))
				switch action.GetVerb() {
				case "create":
					create, ok := action.(clienttesting.CreateAction)
					g.Expect(ok).To(BeTrue())

					created, ok := create.GetObject().(*unstructured.Unstructured)
					g.Expect(ok).To(BeTrue())
					version, exists, err := unstructured.NestedString(created.Object, "spec", "resource", "version")
					g.Expect(err).To(Succeed())
					g.Expect(exists).To(Equal(api.Version == "v1alpha1"))
					if exists {
						g.Expect(version).To(Equal("v2"))
					}
				case "delete":
					deleted, ok := action.(clienttesting.DeleteAction)
					g.Expect(ok).To(BeTrue())
					g.Expect(*deleted.GetDeleteOptions().Preconditions.UID).To(Equal(types.UID("request-uid")))
				}
			}
			_, err = requests.Resource(api).Get(t.Context(), m.request.GetName(), metav1.GetOptions{})
			g.Expect(k8serr.IsNotFound(err)).To(BeTrue())
		})
	}
}

func TestRequestRecovery(t *testing.T) {
	tests := []struct {
		name      string
		condition string
		mismatch  bool
		wantError bool
	}{
		{
			name:      "success",
			condition: "Succeeded",
		},
		{
			name: "pending",
		},
		{
			name:      "failed",
			condition: "Failed",
			wantError: true,
		},
		{
			name:      "unmarked",
			condition: "Succeeded",
			mismatch:  true,
			wantError: true,
		},
		{
			name:      "generation mismatch",
			condition: "Succeeded",
			mismatch:  true,
			wantError: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)

			crd := testCRD()
			api := migrationAPIs[3]
			m, definitions, requests := testMigration(crd, api)
			request := seedRequest(t, requests, crd, api, test.condition)
			if test.mismatch {
				switch test.name {
				case "unmarked":
					request.SetAnnotations(nil)
				case "generation mismatch":
					resources.SetAnnotation(request, annotationPrefix+"crd-generation", "1")
				}
				g.Expect(requests.Tracker().Update(api, request, "")).To(Succeed())
			}
			reads := 0
			requests.PrependReactor("get", "*", func(action clienttesting.Action) (bool, runtime.Object, error) {
				if action.GetResource() == api {
					reads++
					if reads == 2 && test.condition == "" {
						setCondition(request, "Succeeded")
						if err := requests.Tracker().Update(api, request, ""); err != nil {
							return true, nil, err
						}

						return true, request.DeepCopy(), nil
					}
				}
				return false, nil, nil
			})
			err := m.execute(t.Context())
			if test.wantError {
				g.Expect(err).To(HaveOccurred())
				g.Expect(definitions.Actions()).To(HaveLen(1))
			} else {
				g.Expect(err).To(Succeed())
			}
			for _, action := range requests.Actions() {
				g.Expect(action.GetVerb()).NotTo(Equal("update"))
				g.Expect(action.GetVerb()).NotTo(Equal("patch"))
				if test.wantError {
					g.Expect(action.GetVerb()).NotTo(Equal("delete"))
				}
			}
		})
	}
}

func TestRequestFromPreviousGeneration(t *testing.T) {
	g := NewWithT(t)

	crd := testCRD()
	api := migrationAPIs[3]
	m, _, requests := testMigration(crd, api)
	previous := seedRequest(t, requests, crd, api, "Succeeded")

	// Model a storage version change and rollback on the same CRD.
	crd.Generation += 2
	_, err := m.definitions.Update(t.Context(), crd, metav1.UpdateOptions{})
	g.Expect(err).To(Succeed())

	g.Expect(m.execute(t.Context())).To(Succeed())
	g.Expect(m.request.GetName()).NotTo(Equal(previous.GetName()), "a new generation requires a fresh migration")
	g.Expect(resources.GetAnnotation(m.request, annotationPrefix+"crd-generation")).To(Equal("4"))

	retained, err := requests.Resource(api).Get(t.Context(), previous.GetName(), metav1.GetOptions{})
	g.Expect(err).To(Succeed())
	g.Expect(retained).To(Equal(previous), "the old successful request must not be resumed or deleted")
}

func TestSelectsOneAPI(t *testing.T) {
	g := NewWithT(t)

	crd := testCRD()
	m, _, requests := testMigration(crd, migrationAPIs...)
	previous := seedRequest(t, requests, crd, migrationAPIs[3], "Failed")
	err := m.execute(t.Context())
	g.Expect(err).To(Succeed())
	for _, action := range requests.Actions() {
		g.Expect(action.GetResource()).To(Equal(migrationAPIs[0]), "use only the first supported API throughout the attempt")
	}
	retained, err := requests.Tracker().Get(migrationAPIs[3], "", previous.GetName())
	g.Expect(err).To(Succeed())
	g.Expect(retained).To(Equal(previous), "requests in another group are left alone")
	// Discovery stops after the first supported version; no other group is queried.
	discovered, ok := m.discovery.(*discoveryfake.FakeDiscovery)
	g.Expect(ok).To(BeTrue())
	g.Expect(discovered.Actions()).To(HaveLen(1))
}

func TestFailurePreservesRequest(t *testing.T) {
	for _, stage := range []string{"conflict", "forbidden", "cleanup", "UID", "storage", "generation", "deleting", "request replaced", "request spec"} {
		t.Run(stage, func(t *testing.T) {
			g := NewWithT(t)

			crd := testCRD()
			api := migrationAPIs[0]
			m, definitions, requests := testMigration(crd, api)
			request := seedRequest(t, requests, crd, api, "Succeeded")
			definitions.PrependReactor("patch", "*", func(clienttesting.Action) (bool, runtime.Object, error) {
				switch stage {
				case "conflict":
					return true, nil, k8serr.NewConflict(apiextensionsv1.Resource("customresourcedefinitions"), crd.Name, errors.New("concurrent change"))
				case "forbidden":
					return true, nil, k8serr.NewForbidden(apiextensionsv1.Resource("customresourcedefinitions/status"), crd.Name, errors.New("denied"))
				default:
					return false, nil, nil
				}
			})
			requests.PrependReactor("delete", "*", func(clienttesting.Action) (bool, runtime.Object, error) {
				if stage == "cleanup" {
					return true, nil, errors.New("cleanup unavailable")
				}
				return false, nil, nil
			})
			reads := 0
			definitions.PrependReactor("get", "*", func(clienttesting.Action) (bool, runtime.Object, error) {
				reads++
				if reads != 2 {
					return false, nil, nil
				}
				changed := crd.DeepCopy()
				switch stage {
				case "UID":
					changed.UID = "replacement"
				case "storage":
					changed.Spec.Versions[0].Storage = true
					changed.Spec.Versions[1].Storage = false
				case "generation":
					changed.Generation += 2
				case "deleting":
					changed.DeletionTimestamp = new(metav1.Now())
				default:
					return false, nil, nil
				}
				return true, changed, nil
			})
			requestReads := 0
			requests.PrependReactor("get", "*", func(clienttesting.Action) (bool, runtime.Object, error) {
				requestReads++
				if requestReads != 2 {
					return false, nil, nil
				}
				changed := request.DeepCopy()
				switch stage {
				case "request replaced":
					changed.SetUID("replacement")
				case "request spec":
					changed.Object["spec"] = map[string]any{}
				default:
					return false, nil, nil
				}
				return true, changed, nil
			})
			err := m.execute(t.Context())
			g.Expect(err).To(HaveOccurred())
			current, err := definitions.ApiextensionsV1().CustomResourceDefinitions().Get(t.Context(), crd.Name, metav1.GetOptions{})
			g.Expect(err).To(Succeed())
			if stage == "cleanup" {
				g.Expect(current.Status.StoredVersions).To(Equal([]string{"v2"}))
			} else {
				g.Expect(current.Status.StoredVersions).To(Equal(crd.Status.StoredVersions))
			}
			_, err = requests.Tracker().Get(api, "", request.GetName())
			g.Expect(err).To(Succeed())
		})
	}
}

func TestDiscoveryErrors(t *testing.T) {
	for _, stage := range []string{"absent", "forbidden", "transport", "malformed"} {
		t.Run(stage, func(t *testing.T) {
			g := NewWithT(t)

			crd := testCRD()
			m, definitions, requests := testMigration(crd)
			d := testDiscovery(migrationAPIs[0])
			switch stage {
			case "absent":
				d.Resources = nil
			case "forbidden":
				d.PrependReactor("get", "*", func(clienttesting.Action) (bool, runtime.Object, error) {
					return true, nil, k8serr.NewForbidden(schema.GroupResource{
						Resource: "discovery",
					}, "", errors.New("denied"))
				})
			case "transport":
				d.PrependReactor("get", "*", func(clienttesting.Action) (bool, runtime.Object, error) {
					return true, nil, errors.New("connection failed")
				})
			case "malformed":
				d.Resources[0].APIResources[0].Namespaced = true
			}
			m.discovery = d
			err := m.execute(t.Context())
			g.Expect(err).To(HaveOccurred())
			g.Expect(definitions.Actions()).To(HaveLen(1))
			g.Expect(requests.Actions()).To(BeEmpty())
		})
	}
}

func TestConditions(t *testing.T) {
	for _, test := range []struct {
		name       string
		conditions any
		complete   bool
		failed     bool
	}{
		{
			name: "missing",
		},
		{
			name:       "unknown",
			conditions: []any{map[string]any{"type": "Succeeded", "status": "Unknown"}},
		},
		{
			name:       "false",
			conditions: []any{map[string]any{"type": "Succeeded", "status": "False"}},
		},
		{
			name:       "success",
			conditions: []any{map[string]any{"type": "Succeeded", "status": "True"}},
			complete:   true,
		},
		{
			name:       "failure wins",
			conditions: []any{map[string]any{"type": "Succeeded", "status": "True"}, map[string]any{"type": "Failed", "status": "True"}},
			failed:     true,
		},
		{
			name:       "invalid list",
			conditions: "invalid",
			failed:     true,
		},
		{
			name:       "invalid condition",
			conditions: []any{"invalid"},
			failed:     true,
		},
		{
			name:       "invalid status",
			conditions: []any{map[string]any{"type": "Succeeded", "status": "invalid"}},
			failed:     true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)

			request := &unstructured.Unstructured{
				Object: map[string]any{},
			}
			if test.conditions != nil {
				request.Object["status"] = map[string]any{"conditions": test.conditions}
			}
			complete, err := requestSucceeded(request)
			g.Expect(complete).To(Equal(test.complete))
			if test.failed {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).To(Succeed())
			}
		})
	}
}

func TestPendingDeadline(t *testing.T) {
	g := NewWithT(t)

	crd := testCRD()
	m, definitions, requests := testMigration(crd, migrationAPIs[0])
	request := seedRequest(t, requests, crd, migrationAPIs[0], "")
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	err := m.execute(ctx)
	g.Expect(err).To(MatchError(context.DeadlineExceeded))
	g.Expect(definitions.Actions()).To(HaveLen(1))
	_, err = requests.Tracker().Get(migrationAPIs[0], "", request.GetName())
	g.Expect(err).To(Succeed())
}

func TestStepEvents(t *testing.T) {
	for _, test := range []struct {
		name    string
		failure bool
		reasons []string
	}{
		{
			name: "success",
			reasons: []string{
				"Normal " + validatedReason,
				"Normal " + discoveredReason,
				"Normal " + succeededReason,
				"Normal " + cleanedUpReason,
			},
		},
		{
			name: "cleanup not found",
			reasons: []string{
				"Normal " + validatedReason,
				"Normal " + discoveredReason,
				"Normal " + succeededReason,
				"Normal " + cleanedUpReason,
			},
		},
		{
			name:    "no-op",
			reasons: []string{"Normal " + EventReasonNotNeeded},
		},
		{
			name:    "invalid history",
			failure: true,
			reasons: []string{"Warning " + failedReason + " " + EventActionValidate},
		},
		{
			name:    "missing API",
			failure: true,
			reasons: []string{
				"Normal " + validatedReason,
				"Warning " + failedReason + " " + discoverAction,
			},
		},
		{
			name:    "failed request",
			failure: true,
			reasons: []string{
				"Normal " + validatedReason,
				"Normal " + discoveredReason,
				"Warning " + failedReason + " " + runAction,
			},
		},
		{
			name:    "cleanup failure",
			failure: true,
			reasons: []string{
				"Normal " + validatedReason,
				"Normal " + discoveredReason,
				"Normal " + succeededReason,
				"Warning " + failedReason + " " + cleanupAction,
			},
		},
		{
			name:    "cleanup canceled",
			failure: true,
			reasons: []string{
				"Normal " + validatedReason,
				"Normal " + discoveredReason,
				"Normal " + succeededReason,
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			crd := testCRD()

			switch test.name {
			case "no-op":
				crd.Status.StoredVersions = []string{"v2"}
			case "invalid history":
				crd.Status.StoredVersions = nil
			}

			m, _, requests := testMigration(crd, migrationAPIs[0])
			recorder := events.NewFakeRecorder(8)
			recorder.Verbose = true
			m.recorder = recorder

			switch test.name {
			case "missing API":
				m.discovery = testDiscovery()
			case "failed request":
				seedRequest(t, requests, crd, migrationAPIs[0], "Failed")
			case "cleanup not found":
				requests.PrependReactor("delete", "*", func(action clienttesting.Action) (bool, runtime.Object, error) {
					return true, nil, k8serr.NewNotFound(action.GetResource().GroupResource(), "request")
				})
			case "cleanup failure":
				requests.PrependReactor("delete", "*", func(clienttesting.Action) (bool, runtime.Object, error) {
					return true, nil, errors.New("cleanup unavailable")
				})
			case "cleanup canceled":
				requests.PrependReactor("delete", "*", func(clienttesting.Action) (bool, runtime.Object, error) {
					return true, nil, context.Canceled
				})
			}

			err := m.execute(t.Context())
			if test.failure {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).To(Succeed())
			}

			g.Expect(recorder.Events).To(HaveLen(len(test.reasons)))
			for _, reason := range test.reasons {
				g.Expect(<-recorder.Events).To(ContainSubstring(reason))
			}
		})
	}
}

func TestShutdownIsQuiet(t *testing.T) {
	g := NewWithT(t)
	crd := testCRD()
	m, definitions, requests := testMigration(crd, migrationAPIs[0])
	recorder := events.NewFakeRecorder(8)
	m.recorder = recorder

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	g.Expect(m.execute(ctx)).To(MatchError(context.Canceled))
	g.Expect(recorder.Events).To(BeEmpty())
	g.Expect(definitions.Actions()).To(BeEmpty())
	g.Expect(requests.Actions()).To(BeEmpty())
}

func TestProductionClientCancellation(t *testing.T) {
	for _, stage := range []string{"CRD", "discovery", "request"} {
		t.Run(stage, func(t *testing.T) {
			g := NewWithT(t)

			crd := testCRD()
			reached := make(chan struct{})
			var once sync.Once

			crdPath := "/apis/apiextensions.k8s.io/v1/customresourcedefinitions/" + crd.Name
			discoveryPath := "/apis/storagemigration.k8s.io/v1"
			requestCollection := discoveryPath + "/storageversionmigrations"

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")

				var blocked bool
				switch stage {
				case "CRD":
					blocked = r.URL.Path == crdPath
				case "discovery":
					blocked = r.URL.Path == discoveryPath
				case "request":
					blocked = r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, requestCollection+"/")
				}

				if blocked {
					once.Do(func() { close(reached) })
					<-r.Context().Done()
					return
				}

				var object any
				switch r.URL.Path {
				case crdPath:
					object = crd
				case discoveryPath:
					object = testDiscovery(migrationAPIs[0]).Resources[0]
				case requestCollection:
					w.WriteHeader(http.StatusConflict)
					object = &metav1.Status{
						TypeMeta: metav1.TypeMeta{
							APIVersion: "v1",
							Kind:       "Status",
						},
						Status: metav1.StatusFailure,
						Reason: metav1.StatusReasonAlreadyExists,
						Code:   http.StatusConflict,
					}
				default:
					http.NotFound(w, r)
					return
				}

				if err := json.NewEncoder(w).Encode(object); err != nil {
					t.Error(err)
				}
			}))
			t.Cleanup(server.Close)

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			finished := make(chan error, 1)
			recorder := events.NewFakeRecorder(16)

			var group sync.WaitGroup
			group.Go(func() {
				err := Migrate(ctx, &rest.Config{
					Host: server.URL,
				}, crd.Name, recorder)
				finished <- err
			})
			t.Cleanup(func() {
				cancel()
				group.Wait()
			})

			g.Eventually(reached).WithContext(ctx).Should(BeClosed())
			cancel()

			var err error
			g.Eventually(finished).WithTimeout(time.Second).Should(Receive(&err))
			g.Expect(err).To(MatchError(context.Canceled))

			for len(recorder.Events) > 0 {
				g.Expect(<-recorder.Events).NotTo(ContainSubstring(corev1.EventTypeWarning))
			}
		})
	}
}
