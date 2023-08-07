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
	timeout  = 5 * time.Second
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

	It("should migrate single namespace", func() {
		// given
		dataScienceNs := createDataScienceProject("dsp-01")
		regularNs := createNamespace("non-dsp")
		Expect(cli.Create(context.Background(), dataScienceNs)).To(Succeed())
		Expect(cli.Create(context.Background(), regularNs)).To(Succeed())
		defer objectCleaner.DeleteAll(dataScienceNs, regularNs)

		// when
		Expect(ossmInstaller.MigrateDataScienceProjects()).ToNot(HaveOccurred())

		// then
		Eventually(findMigratedNamespaces, timeout, interval).Should(
			And(
				HaveLen(1),
				ContainElement("dsp-01"),
			),
		)
	})

	It("should not migrate any non-datascience namespace", func() {
		// given
		regularNs := createNamespace("non-dsp")
		Expect(cli.Create(context.Background(), regularNs)).To(Succeed())
		defer objectCleaner.DeleteAll(regularNs)

		// when
		Expect(ossmInstaller.MigrateDataScienceProjects()).ToNot(HaveOccurred())

		// then
		Consistently(findMigratedNamespaces, timeout, interval).Should(BeEmpty()) // we can't wait forever, but this should be good enough
	})

	It("should migrate multiple namespaces", func() {
		// given
		dataScienceNs01 := createDataScienceProject("dsp-01")
		dataScienceNs02 := createDataScienceProject("dsp-02")
		dataScienceNs03 := createDataScienceProject("dsp-03")
		regularNs := createNamespace("non-dsp")
		Expect(cli.Create(context.Background(), dataScienceNs01)).To(Succeed())
		Expect(cli.Create(context.Background(), dataScienceNs02)).To(Succeed())
		Expect(cli.Create(context.Background(), dataScienceNs03)).To(Succeed())
		Expect(cli.Create(context.Background(), regularNs)).To(Succeed())
		defer objectCleaner.DeleteAll(dataScienceNs01, dataScienceNs02, dataScienceNs03, regularNs)

		// when
		Expect(ossmInstaller.MigrateDataScienceProjects()).ToNot(HaveOccurred())

		// then
		Eventually(findMigratedNamespaces, timeout, interval).Should(
			And(
				HaveLen(3),
				ContainElements("dsp-01", "dsp-02", "dsp-03"),
			),
		)
	})

})

func createDataScienceProject(name string) *v1.Namespace {
	namespace := createNamespace(name)
	namespace.Labels = map[string]string{
		"opendatahub.io/dashboard": "true",
	}
	return namespace
}

func createNamespace(name string) *v1.Namespace {
	return &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

func findMigratedNamespaces() []string {
	namespaces := &v1.NamespaceList{}
	var ns []string
	if err := cli.List(context.Background(), namespaces); err != nil && !errors.IsNotFound(err) {
		Fail(err.Error())
	}
	for _, namespace := range namespaces.Items {
		if _, ok := namespace.ObjectMeta.Annotations["opendatahub.io/service-mesh"]; ok {
			ns = append(ns, namespace.Name)
		}
	}
	return ns
}
