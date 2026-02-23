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

//nolint:testpackage
package azure

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ccmv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/azure/v1alpha1"

	. "github.com/onsi/gomega"
)

const (
	timeout  = 30 * time.Second
	interval = 250 * time.Millisecond
)

func TestAzureKubernetesEngine_CreateResource(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	ake := &ccmv1alpha1.AzureKubernetesEngine{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-azure-create",
		},
		Spec: ccmv1alpha1.AzureKubernetesEngineSpec{},
	}

	g.Expect(envTestClient.Create(ctx, ake)).Should(Succeed())
	t.Cleanup(func() {
		_ = envTestClient.Delete(ctx, ake)
	})

	created := &ccmv1alpha1.AzureKubernetesEngine{}
	g.Eventually(func(g Gomega) {
		err := envTestClient.Get(ctx, client.ObjectKeyFromObject(ake), created)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(created.Name).Should(Equal("test-azure-create"))
	}).WithTimeout(timeout).WithPolling(interval).Should(Succeed())
}

func TestAzureKubernetesEngine_StatusConditions(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	ake := &ccmv1alpha1.AzureKubernetesEngine{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-azure-conditions",
		},
		Spec: ccmv1alpha1.AzureKubernetesEngineSpec{},
	}

	g.Expect(envTestClient.Create(ctx, ake)).Should(Succeed())
	t.Cleanup(func() {
		_ = envTestClient.Delete(ctx, ake)
	})

	g.Eventually(func(g Gomega) {
		current := &ccmv1alpha1.AzureKubernetesEngine{}
		err := envTestClient.Get(ctx, client.ObjectKeyFromObject(ake), current)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(current.GetConditions()).ShouldNot(BeEmpty())
	}).WithTimeout(timeout).WithPolling(interval).Should(Succeed())
}

func TestAzureKubernetesEngine_DeleteResource(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	ake := &ccmv1alpha1.AzureKubernetesEngine{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-azure-delete",
		},
		Spec: ccmv1alpha1.AzureKubernetesEngineSpec{},
	}

	g.Expect(envTestClient.Create(ctx, ake)).Should(Succeed())
	g.Expect(envTestClient.Delete(ctx, ake)).Should(Succeed())

	g.Eventually(func(g Gomega) {
		err := envTestClient.Get(ctx, client.ObjectKeyFromObject(ake), &ccmv1alpha1.AzureKubernetesEngine{})
		g.Expect(err).Should(HaveOccurred())
	}).WithTimeout(timeout).WithPolling(interval).Should(Succeed())
}
