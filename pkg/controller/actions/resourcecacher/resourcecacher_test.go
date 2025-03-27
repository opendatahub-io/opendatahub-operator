package resourcecacher_test

import (
	"context"
	"errors"
	"testing"

	"github.com/rs/xid"
	"github.com/stretchr/testify/mock"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/cacher"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/resourcecacher"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"

	. "github.com/onsi/gomega"
)

type testCacher struct {
	mock.Mock
	cacher    *resourcecacher.ResourceCacher
	rr        *types.ReconciliationRequest
	ctx       context.Context //nolint:containedctx
	r         resources.UnstructuredList
	doubleRes resources.UnstructuredList
}

func newHash() []byte {
	return xid.New().Bytes()
}

func newResources() []unstructured.Unstructured {
	return []unstructured.Unstructured{
		{
			Object: map[string]any{
				xid.New().String(): xid.New().String(),
			},
		},
	}
}

func newTestCacher() *testCacher {
	c := &testCacher{
		ctx: context.Background(),
		r:   newResources(),
		rr:  &types.ReconciliationRequest{},
	}
	c.cacher = newCacher(c.hash)
	c.rr.Instance = &componentApi.Dashboard{}
	c.doubleRes = append(c.doubleRes, c.r[0], c.r[0])

	return c
}

func (s *testCacher) resetGenerated() {
	s.rr.Generated = false
}

func (s *testCacher) setResources(r []unstructured.Unstructured) {
	s.rr.Resources = r
}

func (s *testCacher) setDevFlags(f *common.DevFlags) {
	d := s.rr.Instance.(*componentApi.Dashboard) //nolint:errcheck,forcetypeassert
	d.Spec.DevFlags = f
}

func (s *testCacher) GetDevFlags() common.DevFlags {
	args := s.Called()
	return args.Get(0).(common.DevFlags) //nolint:errcheck,forcetypeassert
}

func (s *testCacher) hash(rr *types.ReconciliationRequest) ([]byte, error) {
	args := s.Called(rr)
	return args.Get(0).([]byte), args.Error(1) //nolint:errcheck,forcetypeassert
}

func (s *testCacher) render(ctx context.Context, rr *types.ReconciliationRequest) (resources.UnstructuredList, error) {
	args := s.Called(ctx, rr)
	return args.Get(0).(resources.UnstructuredList), args.Error(1) //nolint:errcheck,forcetypeassert
}

func newCacher(key cacher.CachingKeyFn) *resourcecacher.ResourceCacher {
	cacher := &resourcecacher.ResourceCacher{}
	cacher.SetKeyFn(key)
	return cacher
}
func TestCacherShouldRenderFirstRun(t *testing.T) {
	g := NewWithT(t)
	m := newTestCacher() // mock

	m.On("hash", m.rr).Return(newHash(), nil).Once()
	m.On("render", m.ctx, m.rr).Return(m.r, nil).Once()

	err := m.cacher.Render(m.ctx, m.rr, m.render)

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(m.rr.Resources).Should(BeEquivalentTo(m.r))
	g.Expect(m.rr.Generated).Should(BeTrue())

	m.AssertExpectations(t)
}
func TestCacherShouldNotRenderSecondRun(t *testing.T) {
	g := NewWithT(t)
	m := newTestCacher()

	m.On("hash", m.rr).Return(newHash(), nil).Twice()
	m.On("render", m.ctx, m.rr).Return(m.r, nil).Once()

	_ = m.cacher.Render(m.ctx, m.rr, m.render)
	m.resetGenerated()
	err := m.cacher.Render(m.ctx, m.rr, m.render)

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(m.rr.Resources).Should(BeEquivalentTo(m.doubleRes))
	g.Expect(m.rr.Generated).Should(BeFalse())

	m.AssertExpectations(t)
}

func TestCacherShouldRenderDifferentKey(t *testing.T) {
	g := NewWithT(t)
	m := newTestCacher()

	m.
		On("hash", m.rr).Return(newHash(), nil).Once().
		On("hash", m.rr).Return(newHash(), nil).Once()
	m.On("render", m.ctx, m.rr).Return(m.r, nil).Twice()

	_ = m.cacher.Render(m.ctx, m.rr, m.render)
	m.resetGenerated()
	err := m.cacher.Render(m.ctx, m.rr, m.render)

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(m.rr.Resources).Should(BeEquivalentTo(m.doubleRes))
	g.Expect(m.rr.Generated).Should(BeTrue())

	m.AssertExpectations(t)
}

func TestCacherShouldRenderIfKeyUnset(t *testing.T) {
	g := NewWithT(t)
	m := newTestCacher()

	m.cacher.SetKeyFn(nil)

	m.On("render", m.ctx, m.rr).Return(m.r, nil).Twice()

	_ = m.cacher.Render(m.ctx, m.rr, m.render)
	m.resetGenerated()
	err := m.cacher.Render(m.ctx, m.rr, m.render)

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(m.rr.Resources).Should(BeEquivalentTo(m.doubleRes))
	g.Expect(m.rr.Generated).Should(BeTrue())

	m.AssertExpectations(t)
}

func TestCacherShouldErrorIfKeyError(t *testing.T) {
	g := NewWithT(t)
	m := newTestCacher()

	keyErr := errors.New("hashing error")

	m.On("hash", m.rr).Return(newHash(), keyErr).Once()

	err := m.cacher.Render(m.ctx, m.rr, m.render)

	g.Expect(err).Should(HaveOccurred())
	g.Expect(m.rr.Resources).Should(BeEmpty())
	g.Expect(m.rr.Generated).Should(BeFalse())

	m.AssertExpectations(t)
}

func TestCacherShouldErrorIfRenderError(t *testing.T) {
	g := NewWithT(t)
	m := newTestCacher()

	renderErr := errors.New("hashing error")

	m.On("hash", m.rr).Return(newHash(), nil).Once()
	m.On("render", m.ctx, m.rr).Return(cacher.Zero[resources.UnstructuredList](), renderErr).Once()

	err := m.cacher.Render(m.ctx, m.rr, m.render)

	g.Expect(err).Should(HaveOccurred())
	g.Expect(m.rr.Resources).Should(BeEmpty())
	g.Expect(m.rr.Generated).Should(BeFalse())

	m.AssertExpectations(t)
}

func TestCacherShouldRenderIfDevFlags(t *testing.T) {
	g := NewWithT(t)
	m := newTestCacher()
	devFlags := common.DevFlags{}

	m.On("hash", m.rr).Return(newHash(), nil).Twice()
	m.On("render", m.ctx, m.rr).Return(m.r, nil).Twice()

	m.setDevFlags(&devFlags)
	_ = m.cacher.Render(m.ctx, m.rr, m.render)
	m.resetGenerated()
	err := m.cacher.Render(m.ctx, m.rr, m.render)

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(m.rr.Resources).Should(BeEquivalentTo(m.doubleRes))
	g.Expect(m.rr.Generated).Should(BeTrue())

	m.AssertExpectations(t)
}
func TestCacherShouldAddResources(t *testing.T) {
	g := NewWithT(t)
	m := newTestCacher()

	orig := append(newResources(), newResources()...)
	m.setResources(orig)
	expected := append([]unstructured.Unstructured{}, orig...)
	expected = append(expected, m.r...)

	m.On("hash", m.rr).Return(newHash(), nil).Once()
	m.On("render", m.ctx, m.rr).Return(m.r, nil).Once()

	err := m.cacher.Render(m.ctx, m.rr, m.render)

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(m.rr.Resources).Should(BeEquivalentTo(expected))

	m.AssertExpectations(t)
}
