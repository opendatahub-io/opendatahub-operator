//nolint:testpackage
package modules

import (
	"reflect"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"

	. "github.com/onsi/gomega"
)

func newUnstructuredWithReleases(releases []any) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.Object = map[string]any{
		"status": map[string]any{
			"releases": releases,
		},
	}
	return u
}

func TestExtractReleases_Empty(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	u := &unstructured.Unstructured{Object: map[string]any{
		"status": map[string]any{},
	}}

	g.Expect(extractReleases(u)).Should(BeEmpty())
}

func TestExtractReleases_SingleEntry(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	u := newUnstructuredWithReleases([]any{
		map[string]any{
			"name":    "platform",
			"version": "3.5.0",
			"repoUrl": "https://github.com/example",
		},
	})

	releases := extractReleases(u)
	g.Expect(releases).Should(HaveLen(1))
	g.Expect(releases[0].Name).Should(Equal("platform"))
	g.Expect(releases[0].Version).Should(Equal("3.5.0"))
	g.Expect(releases[0].RepoURL).Should(Equal("https://github.com/example"))
}

func TestExtractReleases_MultipleEntries(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	u := newUnstructuredWithReleases([]any{
		map[string]any{
			"name":    "platform",
			"version": "3.5.0",
		},
		map[string]any{
			"name":    "serving",
			"version": "0.14.1",
			"repoUrl": "https://github.com/kserve",
		},
	})

	releases := extractReleases(u)
	g.Expect(releases).Should(HaveLen(2))
	g.Expect(releases[0].Name).Should(Equal("platform"))
	g.Expect(releases[1].Name).Should(Equal("serving"))
	g.Expect(releases[1].RepoURL).Should(Equal("https://github.com/kserve"))
}

func TestExtractReleases_SkipsEmptyName(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	u := newUnstructuredWithReleases([]any{
		map[string]any{
			"version": "1.0.0",
		},
		map[string]any{
			"name":    "valid",
			"version": "2.0.0",
		},
	})

	releases := extractReleases(u)
	g.Expect(releases).Should(HaveLen(1))
	g.Expect(releases[0].Name).Should(Equal("valid"))
}

func TestExtractReleases_SkipsMalformedEntries(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	u := newUnstructuredWithReleases([]any{
		"not-a-map",
		map[string]any{
			"name":    "valid",
			"version": "1.0.0",
		},
	})

	releases := extractReleases(u)
	g.Expect(releases).Should(HaveLen(1))
	g.Expect(releases[0].Name).Should(Equal("valid"))
}

func TestExtractReleases_MissingOptionalFields(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	u := newUnstructuredWithReleases([]any{
		map[string]any{
			"name": "minimal",
		},
	})

	releases := extractReleases(u)
	g.Expect(releases).Should(HaveLen(1))
	g.Expect(releases[0].Name).Should(Equal("minimal"))
	g.Expect(releases[0].Version).Should(BeEmpty())
	g.Expect(releases[0].RepoURL).Should(BeEmpty())
}

func TestPlatformReleaseVersion_Found(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	releases := []common.ComponentRelease{
		{Name: "serving", Version: "0.14.1"},
		{Name: "platform", Version: "3.5.0"},
	}

	g.Expect(platformReleaseVersion(releases)).Should(Equal("3.5.0"))
}

func TestPlatformReleaseVersion_NotFound(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	releases := []common.ComponentRelease{
		{Name: "serving", Version: "0.14.1"},
	}

	g.Expect(platformReleaseVersion(releases)).Should(BeEmpty())
}

func TestPlatformReleaseVersion_Nil(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	g.Expect(platformReleaseVersion(nil)).Should(BeEmpty())
}

func newReleaseHandler(kind string) *BaseHandler {
	return &BaseHandler{
		Config: ModuleConfig{
			GVK: schema.GroupVersionKind{Kind: kind},
		},
	}
}

func TestWriteDSCComponentStatus_Kserve_SetsReleases(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	dsc := &dscv2.DataScienceCluster{}
	releases := []common.ComponentRelease{
		{Name: "platform", Version: "3.5.0"},
		{Name: "serving", Version: "0.14.1"},
	}

	newReleaseHandler("Kserve").WriteDSCComponentStatus(dsc, true, releases)

	g.Expect(dsc.Status.Components.Kserve.ManagementState).Should(Equal(operatorv1.Managed))
	g.Expect(dsc.Status.Components.Kserve.KserveCommonStatus).ShouldNot(BeNil())
	g.Expect(dsc.Status.Components.Kserve.Releases).Should(HaveLen(2))
	g.Expect(dsc.Status.Components.Kserve.Releases[0].Name).Should(Equal("platform"))
	g.Expect(dsc.Status.Components.Kserve.Releases[1].Name).Should(Equal("serving"))
}

func TestWriteDSCComponentStatus_Kserve_ClearsReleases(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	dsc := &dscv2.DataScienceCluster{}
	newReleaseHandler("Kserve").WriteDSCComponentStatus(dsc, true, []common.ComponentRelease{
		{Name: "platform", Version: "3.5.0"},
	})
	g.Expect(dsc.Status.Components.Kserve.KserveCommonStatus).ShouldNot(BeNil())

	newReleaseHandler("Kserve").WriteDSCComponentStatus(dsc, false, nil)
	g.Expect(dsc.Status.Components.Kserve.ManagementState).Should(Equal(operatorv1.Removed))
	g.Expect(dsc.Status.Components.Kserve.KserveCommonStatus).ShouldNot(BeNil())
	g.Expect(dsc.Status.Components.Kserve.Releases).Should(BeNil())
}

func TestWriteDSCComponentStatus_AIGateway_SetsReleases(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	dsc := &dscv2.DataScienceCluster{}
	releases := []common.ComponentRelease{
		{Name: "platform", Version: "3.5.0"},
	}

	newReleaseHandler("AIGateway").WriteDSCComponentStatus(dsc, true, releases)

	g.Expect(dsc.Status.Components.AIGateway.ManagementState).Should(Equal(operatorv1.Managed))
	g.Expect(dsc.Status.Components.AIGateway.AIGatewayCommonStatus).ShouldNot(BeNil())
	g.Expect(dsc.Status.Components.AIGateway.Releases).Should(HaveLen(1))
	g.Expect(dsc.Status.Components.AIGateway.Releases[0].Name).Should(Equal("platform"))
}

func TestWriteDSCComponentStatus_AIGateway_ClearsReleases(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	dsc := &dscv2.DataScienceCluster{}
	newReleaseHandler("AIGateway").WriteDSCComponentStatus(dsc, true, []common.ComponentRelease{
		{Name: "platform", Version: "1.0.0"},
	})
	g.Expect(dsc.Status.Components.AIGateway.AIGatewayCommonStatus).ShouldNot(BeNil())

	newReleaseHandler("AIGateway").WriteDSCComponentStatus(dsc, false, nil)
	g.Expect(dsc.Status.Components.AIGateway.AIGatewayCommonStatus).ShouldNot(BeNil())
	g.Expect(dsc.Status.Components.AIGateway.Releases).Should(BeNil())
}

func TestWriteDSCComponentStatus_TypeWithoutReleases_NoOp(t *testing.T) {
	t.Parallel()

	dsc := &dscv2.DataScienceCluster{}
	newReleaseHandler("Dashboard").WriteDSCComponentStatus(dsc, true, []common.ComponentRelease{
		{Name: "platform", Version: "1.0.0"},
	})
}

func TestWriteDSCComponentStatus_UnknownKind_NoOp(t *testing.T) {
	t.Parallel()

	dsc := &dscv2.DataScienceCluster{}
	newReleaseHandler("NonExistent").WriteDSCComponentStatus(dsc, true, []common.ComponentRelease{
		{Name: "platform", Version: "1.0.0"},
	})
}

func TestWriteDSCComponentStatus_PreservesManagementState(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	dsc := &dscv2.DataScienceCluster{}
	dsc.Status.Components.Kserve.ManagementState = operatorv1.Managed

	newReleaseHandler("Kserve").WriteDSCComponentStatus(dsc, true, []common.ComponentRelease{
		{Name: "platform", Version: "3.5.0"},
	})

	g.Expect(dsc.Status.Components.Kserve.ManagementState).Should(Equal(operatorv1.Managed))
	g.Expect(dsc.Status.Components.Kserve.Releases).Should(HaveLen(1))
}

func TestWriteDSCComponentStatus_NilPointerAllocated(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	dsc := &dscv2.DataScienceCluster{}
	g.Expect(dsc.Status.Components.Kserve.KserveCommonStatus).Should(BeNil())

	newReleaseHandler("Kserve").WriteDSCComponentStatus(dsc, true, []common.ComponentRelease{
		{Name: "platform", Version: "3.5.0"},
	})

	g.Expect(dsc.Status.Components.Kserve.KserveCommonStatus).ShouldNot(BeNil())
	g.Expect(dsc.Status.Components.Kserve.Releases).Should(HaveLen(1))
}

func TestSetReleasesOnDSCField_SetsOnKserve(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	dsc := &dscv2.DataScienceCluster{}
	field := reflect.ValueOf(&dsc.Status.Components).Elem().FieldByName("Kserve")

	setReleasesOnDSCField(field, []common.ComponentRelease{
		{Name: "platform", Version: "3.5.0"},
	})

	g.Expect(dsc.Status.Components.Kserve.KserveCommonStatus).ShouldNot(BeNil())
	g.Expect(dsc.Status.Components.Kserve.Releases).Should(HaveLen(1))
}

func TestSetReleasesOnDSCField_ClearsReleasesPreservesPointer(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	dsc := &dscv2.DataScienceCluster{}
	field := reflect.ValueOf(&dsc.Status.Components).Elem().FieldByName("Kserve")

	setReleasesOnDSCField(field, []common.ComponentRelease{
		{Name: "platform", Version: "3.5.0"},
	})
	g.Expect(dsc.Status.Components.Kserve.KserveCommonStatus).ShouldNot(BeNil())

	setReleasesOnDSCField(field, nil)
	g.Expect(dsc.Status.Components.Kserve.KserveCommonStatus).ShouldNot(BeNil())
	g.Expect(dsc.Status.Components.Kserve.Releases).Should(BeNil())
}

func TestSetReleasesOnDSCField_NilPointerNilReleases_NoOp(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	dsc := &dscv2.DataScienceCluster{}
	field := reflect.ValueOf(&dsc.Status.Components).Elem().FieldByName("Kserve")

	setReleasesOnDSCField(field, nil)
	g.Expect(dsc.Status.Components.Kserve.KserveCommonStatus).Should(BeNil())
}

func TestSetReleasesOnDSCField_NoOpWithoutReleasesField(t *testing.T) {
	t.Parallel()

	dsc := &dscv2.DataScienceCluster{}
	field := reflect.ValueOf(&dsc.Status.Components).Elem().FieldByName("Dashboard")

	setReleasesOnDSCField(field, []common.ComponentRelease{
		{Name: "platform", Version: "1.0.0"},
	})
}
