package servicemesh_test

import (
	"os"
	"path/filepath"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/servicemesh"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Overwriting gateway name in env file", func() {

	It("should replace gateway name in the file", func() {
		namespace := "test-namespace"
		path := createTempEnvFile()
		Expect(servicemesh.OverwriteIstioGatewayVar(namespace, path)).To(Succeed())

		updatedContents, err := os.ReadFile(filepath.Join(path, "ossm.env"))
		Expect(err).NotTo(HaveOccurred())

		Expect(string(updatedContents)).To(ContainSubstring("ISTIO_GATEWAY=test-namespace/odh-gateway"))
	})

	It("should fail if the file does not exist", func() {
		Expect(servicemesh.OverwriteIstioGatewayVar("test-namespace", "wrong_directory")).To(Not(Succeed()))
	})

	It("should not modify other text in the file", func() {
		namespace := "test-namespace"
		path := createTempEnvFile()

		Expect(servicemesh.OverwriteIstioGatewayVar(namespace, path)).To(Succeed())

		updatedContents, err := os.ReadFile(filepath.Join(path, "ossm.env"))
		Expect(err).NotTo(HaveOccurred())

		Expect(string(updatedContents)).To(ContainSubstring("AnotherSetting=value"))
	})
})

const testContent = `
ISTIO_GATEWAY=default-namespace/odh-gateway
AnotherSetting=value`

func createTempEnvFile() string {
	var err error

	tempDir := GinkgoT().TempDir()

	tempFilePath := filepath.Join(tempDir, "ossm.env")
	tempFile, err := os.Create(tempFilePath)
	Expect(err).NotTo(HaveOccurred())

	_, err = tempFile.WriteString(testContent)
	Expect(err).NotTo(HaveOccurred())
	defer tempFile.Close()

	return tempDir
}
