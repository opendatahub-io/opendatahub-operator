package deploy_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestStringSearch(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Check Params Suite")
}

var _ = Describe("Throw error if image still use old image registry", func() {

	var (
		tmpFile   *os.File
		filePath  = "/tmp/manifests/components/one"
		err       error
		paramsenv string
	)

	// create a temp params.env file
	BeforeEach(func() {
		err = os.MkdirAll(filePath, 0755)
		tmpFile, err = os.Create(filepath.Join(filePath, "params.env"))
		Expect(err).NotTo(HaveOccurred())

		// fake some lines
		paramsenv = `url=https://www.odh.rbac.com
myimagee=quay.io/opendatahub/repo:abc
namespace=opendatahub`
		_, err = tmpFile.WriteString(paramsenv)
		Expect(err).NotTo(HaveOccurred())

		err = tmpFile.Close()
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		err = os.Remove(tmpFile.Name())
		Expect(err).NotTo(HaveOccurred())
	})

	Context("when searching in params.env", func() {
		It("Should not throw error when string found", func() {
			err := deploy.CheckParams(filePath, []string{"abc"})
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should not throw error when all string are matched", func() {
			err := deploy.CheckParams(filePath, []string{"rbac", "abc"})
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should not throw error when emptry string provided(as env not found)", func() {
			err := deploy.CheckParams(filePath, []string{""})
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should throw error when string not found", func() {
			err := deploy.CheckParams(filePath, []string{"vw"})
			Expect(err).To(HaveOccurred())
		})

		It("Should throw error when multiple strings are not found", func() {
			err := deploy.CheckParams(filePath, []string{"vw", "123"})
			Expect(err).To(HaveOccurred())
		})
	})

})
