package platformrelease_test

import (
	"testing"

	"github.com/blang/semver/v4"
	"github.com/operator-framework/api/pkg/lib/version"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/status/platformrelease"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"

	. "github.com/onsi/gomega"
)

func newReconciliationRequest(instance common.PlatformObject, skipDeploy bool) types.ReconciliationRequest {
	sv := semver.MustParse("2.5.0")

	return types.ReconciliationRequest{
		Instance:   instance,
		SkipDeploy: skipDeploy,
		Release:    common.Release{Version: version.OperatorVersion{Version: sv}},
	}
}

func TestPlatformReleasePostStatusFn(t *testing.T) {
	t.Run("should stamp platform release when happy and deploy not skipped", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		instance := &componentApi.Ray{ObjectMeta: metav1.ObjectMeta{Name: "default-ray"}}
		rr := newReconciliationRequest(instance, false)

		fn := platformrelease.NewPostStatusFn()
		err := fn(ctx, &rr, true)

		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(instance.GetReleaseStatus()).To(HaveValue(ConsistOf(
			common.ComponentRelease{Name: common.PlatformReleaseName, Version: "2.5.0"},
		)))
	})

	t.Run("should not stamp when not happy", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		instance := &componentApi.Ray{ObjectMeta: metav1.ObjectMeta{Name: "default-ray"}}
		rr := newReconciliationRequest(instance, false)

		fn := platformrelease.NewPostStatusFn()
		err := fn(ctx, &rr, false)

		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(instance.GetReleaseStatus()).To(HaveValue(BeEmpty()))
	})

	t.Run("should not stamp when deploy is skipped", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		instance := &componentApi.Ray{ObjectMeta: metav1.ObjectMeta{Name: "default-ray"}}
		rr := newReconciliationRequest(instance, true)

		fn := platformrelease.NewPostStatusFn()
		err := fn(ctx, &rr, true)

		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(instance.GetReleaseStatus()).To(HaveValue(BeEmpty()))
	})

	t.Run("should update existing platform release entry and move it to first position", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		instance := &componentApi.Ray{ObjectMeta: metav1.ObjectMeta{Name: "default-ray"}}
		instance.SetReleaseStatus([]common.ComponentRelease{
			{Name: "ray", Version: "1.0.0"},
			{Name: common.PlatformReleaseName, Version: "2.0.0"},
		})

		rr := newReconciliationRequest(instance, false)

		fn := platformrelease.NewPostStatusFn()
		err := fn(ctx, &rr, true)

		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(instance.GetReleaseStatus()).To(HaveValue(HaveExactElements(
			common.ComponentRelease{Name: common.PlatformReleaseName, Version: "2.5.0"},
			common.ComponentRelease{Name: "ray", Version: "1.0.0"},
		)))
	})

	t.Run("should prepend platform release when other releases exist", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		instance := &componentApi.Ray{ObjectMeta: metav1.ObjectMeta{Name: "default-ray"}}
		instance.SetReleaseStatus([]common.ComponentRelease{
			{Name: "ray", Version: "1.0.0"},
		})

		rr := newReconciliationRequest(instance, false)

		fn := platformrelease.NewPostStatusFn()
		err := fn(ctx, &rr, true)

		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(instance.GetReleaseStatus()).To(HaveValue(HaveExactElements(
			common.ComponentRelease{Name: common.PlatformReleaseName, Version: "2.5.0"},
			common.ComponentRelease{Name: "ray", Version: "1.0.0"},
		)))
	})
}
