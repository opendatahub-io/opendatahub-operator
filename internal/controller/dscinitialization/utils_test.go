//nolint:testpackage
package dscinitialization

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func TestCreateAppNamespaceAddsClusterMonitoringLabelForSelfManagedRhoai(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)

	cli, err := fakeclient.New()
	g.Expect(err).NotTo(HaveOccurred())
	reconciler := &DSCInitializationReconciler{Client: cli}

	g.Expect(reconciler.createAppNamespace(t.Context(), "redhat-ods-applications", cluster.SelfManagedRhoai)).To(Succeed())

	namespace := &corev1.Namespace{}
	g.Expect(cli.Get(t.Context(), client.ObjectKey{Name: "redhat-ods-applications"}, namespace)).To(Succeed())
	g.Expect(namespace.Labels[labels.ClusterMonitoring]).To(Equal(labels.True))
}

func TestCreateAppNamespaceReconcilesClusterMonitoringLabel(t *testing.T) {
	g := NewWithT(t)

	applicationsNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "redhat-ods-applications",
			Labels: map[string]string{
				labels.ClusterMonitoring: "false",
			},
		},
	}
	cli, err := fakeclient.New(fakeclient.WithObjects(applicationsNamespace))
	g.Expect(err).NotTo(HaveOccurred())
	reconciler := &DSCInitializationReconciler{Client: cli}

	g.Expect(reconciler.createAppNamespace(t.Context(), applicationsNamespace.Name, cluster.SelfManagedRhoai)).To(Succeed())

	g.Expect(cli.Get(t.Context(), client.ObjectKeyFromObject(applicationsNamespace), applicationsNamespace)).To(Succeed())
	g.Expect(applicationsNamespace.Labels[labels.ClusterMonitoring]).To(Equal(labels.True))
}

func TestCreateAppNamespaceDoesNotAddClusterMonitoringLabelForOpenDataHub(t *testing.T) {
	g := NewWithT(t)

	cli, err := fakeclient.New()
	g.Expect(err).NotTo(HaveOccurred())
	reconciler := &DSCInitializationReconciler{Client: cli}

	g.Expect(reconciler.createAppNamespace(t.Context(), "redhat-ods-applications", cluster.OpenDataHub)).To(Succeed())

	namespace := &corev1.Namespace{}
	g.Expect(cli.Get(t.Context(), client.ObjectKey{Name: "redhat-ods-applications"}, namespace)).To(Succeed())
	g.Expect(namespace.Labels).NotTo(HaveKey(labels.ClusterMonitoring))
}
