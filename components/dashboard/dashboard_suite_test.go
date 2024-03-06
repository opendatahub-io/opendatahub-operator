package dashboard_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestDashboardComponent(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "dashboard unit tests")
}
