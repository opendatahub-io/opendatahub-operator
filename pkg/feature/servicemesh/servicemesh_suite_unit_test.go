package servicemesh_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestServiceMeshSetup(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Service Mesh setup unit tests")
}
