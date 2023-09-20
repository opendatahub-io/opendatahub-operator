package cluster_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestClusterHelpers(t *testing.T) {
	RegisterFailHandler(Fail)
	// for integration tests see tests/integration directory
	RunSpecs(t, "Cluster helper funcs unit tests")
}
