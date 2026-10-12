package testf_test

import (
	"context"
	"testing"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

func TestWarningRecorder(t *testing.T) {
	g := NewWithT(t)
	recorder := testf.NewWarningRecorder()

	recorder.HandleWarningHeaderWithContext(context.Background(), 299, "", "warning")
	messages := recorder.Messages()
	messages[0] = "changed"

	g.Expect(recorder.Messages()).To(Equal([]string{"warning"}))
}
