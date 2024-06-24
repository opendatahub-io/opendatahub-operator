package envtestutil

import (
	"context"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/gomega" //nolint:stylecheck // This is the standard for ginkgo and gomega.
)

// Cleaner is a struct to perform deletion of resources,
// enforcing removal of finalizers. Otherwise, deletion of namespaces wouldn't be possible.
// See: https://book.kubebuilder.io/reference/envtest.html#namespace-usage-limitation
// Based on https://github.com/kubernetes-sigs/controller-runtime/issues/880#issuecomment-749742403
type Cleaner struct {
	clientset         *kubernetes.Clientset
	client            client.Client
	timeout, interval time.Duration
}

func CreateCleaner(c client.Client, config *rest.Config, timeout, interval time.Duration) *Cleaner {
	k8sClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err)
	}

	return &Cleaner{
		clientset: k8sClient,
		client:    c,
		timeout:   timeout,
		interval:  interval,
	}
}

func (c *Cleaner) DeleteAll(objects ...client.Object) {
	for _, obj := range objects {
		obj := obj
		Expect(client.IgnoreNotFound(c.client.Delete(context.Background(), obj))).Should(Succeed())

		if ns, ok := obj.(*corev1.Namespace); ok {
			// Normally the kube-controller-manager would handle finalization
			// and garbage collection of namespaces, but with envtest, we aren't
			// running a kube-controller-manager. Instead, we're going to approximate
			// (poorly) the kube-controller-manager by explicitly deleting some
			// resources within the namespace and then removing the `kubernetes`
			// finalizer from the namespace resource, so it can finish deleting.
			// Note that any resources within the namespace that we don't
			// successfully delete could reappear if the namespace is ever
			// recreated with the same name.

			// Look up all namespaced resources under the discovery API
			_, apiResources, err := c.clientset.DiscoveryClient.ServerGroupsAndResources()
			Expect(err).ShouldNot(HaveOccurred())
			namespacedGVKs := make(map[string]schema.GroupVersionKind)
			for _, apiResourceList := range apiResources {
				defaultGV, err := schema.ParseGroupVersion(apiResourceList.GroupVersion)
				Expect(err).ShouldNot(HaveOccurred())
				for _, r := range apiResourceList.APIResources {
					if !r.Namespaced || strings.Contains(r.Name, "/") {
						// skip non-namespaced and subresources
						continue
					}
					gvk := schema.GroupVersionKind{
						Group:   defaultGV.Group,
						Version: defaultGV.Version,
						Kind:    r.Kind,
					}
					if r.Group != "" {
						gvk.Group = r.Group
					}
					if r.Version != "" {
						gvk.Version = r.Version
					}
					namespacedGVKs[gvk.String()] = gvk
				}
			}

			// Delete all namespaced resources in this namespace
			for _, gvk := range namespacedGVKs {
				var u unstructured.Unstructured
				u.SetGroupVersionKind(gvk)
				err := c.client.DeleteAllOf(context.Background(), &u, client.InNamespace(ns.Name))
				Expect(client.IgnoreNotFound(ignoreMethodNotAllowed(err))).ShouldNot(HaveOccurred())
			}

			Eventually(func() error {
				key := client.ObjectKeyFromObject(ns)

				if err := c.client.Get(context.Background(), key, ns); err != nil {
					return client.IgnoreNotFound(err)
				}
				// remove `kubernetes` finalizer
				const k8s = "kubernetes"
				finalizers := []corev1.FinalizerName{}
				for _, f := range ns.Spec.Finalizers {
					if f != k8s {
						finalizers = append(finalizers, f)
					}
				}
				ns.Spec.Finalizers = finalizers

				// We have to use the k8s.io/client-go library here to expose
				// ability to patch the /finalize subresource on the namespace
				_, err = c.clientset.CoreV1().Namespaces().Finalize(context.Background(), ns, metav1.UpdateOptions{})

				return err
			}).
				WithTimeout(c.timeout).
				WithPolling(c.interval).
				Should(Succeed())
		}

		Eventually(func() metav1.StatusReason {
			key := client.ObjectKeyFromObject(obj)
			if err := c.client.Get(context.Background(), key, obj); err != nil {
				return k8serr.ReasonForError(err)
			}

			return ""
		}, c.timeout, c.interval).Should(Equal(metav1.StatusReasonNotFound))
	}
}

func ignoreMethodNotAllowed(err error) error {
	if k8serr.ReasonForError(err) == metav1.StatusReasonMethodNotAllowed {
		return nil
	}

	return err
}
