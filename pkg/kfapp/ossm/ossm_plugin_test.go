package ossm_test

import (
	"context"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/opendatahub-io/opendatahub-operator/pkg/kfapp/ossm"
	"github.com/opendatahub-io/opendatahub-operator/pkg/kfapp/ossm/test/testenv"
	"github.com/opendatahub-io/opendatahub-operator/pkg/kfconfig"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"time"
)

const (
	timeout  = 1 * time.Minute
	interval = 250 * time.Millisecond
)

var _ = When("Migrating Data Science Projects", func() {

	var (
		objectCleaner *testenv.Cleaner
		ossmInstaller *ossm.OssmInstaller
	)

	BeforeEach(func() {
		ossmInstaller = ossm.NewOssmInstaller(&kfconfig.KfConfig{}, envTest.Config)
		objectCleaner = testenv.CreateCleaner(cli, envTest.Config, timeout, interval)
	})

	It("should find one namespace to migrate", func() {
		// given
		dataScienceNs := createDSProject("dsp-01")
		regularNs := createNs("non-dsp")
		Expect(cli.Create(context.Background(), dataScienceNs)).To(Succeed())
		Expect(cli.Create(context.Background(), regularNs)).To(Succeed())
		defer objectCleaner.DeleteAll(dataScienceNs, regularNs)

		// when
		Expect(ossmInstaller.MigrateDSProjects()).ToNot(HaveOccurred())

		// then
		Eventually(func() []v1.Namespace {
			namespaces := &v1.NamespaceList{}
			var ns []v1.Namespace
			if err := cli.List(context.Background(), namespaces); err != nil && !errors.IsNotFound(err) {
				Fail(err.Error())
			}
			for _, namespace := range namespaces.Items {
				if _, ok := namespace.ObjectMeta.Annotations["opendatahub.io/service-mesh"]; ok {
					ns = append(ns, namespace)
				}
			}
			return ns
		}, timeout, interval).Should(HaveLen(1))
	})

})

func createDSProject(name string) *v1.Namespace {
	namespace := createNs(name)
	namespace.Labels = map[string]string{
		"opendatahub.io/dashboard": "true",
	}
	return namespace
}

func createNs(name string) *v1.Namespace {
	return &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}
