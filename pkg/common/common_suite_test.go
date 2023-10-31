package common_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestK8sNamingHelpers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Common k8s naming func unit tests")
}
