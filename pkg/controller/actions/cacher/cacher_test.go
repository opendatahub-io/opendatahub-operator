package cacher_test

import (
	"context"
	"errors"
	"testing"

	"github.com/rs/xid"
	"github.com/stretchr/testify/mock"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/cacher"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"

	. "github.com/onsi/gomega"
)

type testCacher struct {
	mock.Mock
	cacher *cacher.Cacher[resources.UnstructuredList]
	rr     *types.ReconciliationRequest
	ctx    context.Context //nolint:containedctx
	r      resources.UnstructuredList
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
	return c
}

func (s *testCacher) hash(rr *types.ReconciliationRequest) ([]byte, error) {
	args := s.Called(rr)
	return args.Get(0).([]byte), args.Error(1) //nolint:errcheck,forcetypeassert
}

func (s *testCacher) render(ctx context.Context, rr *types.ReconciliationRequest) (resources.UnstructuredList, error) {
	args := s.Called(ctx, rr)
	return args.Get(0).(resources.UnstructuredList), args.Error(1) //nolint:errcheck,forcetypeassert
}

func newCacher(key cacher.CachingKeyFn) *cacher.Cacher[resources.UnstructuredList] {
	cacher := &cacher.Cacher[resources.UnstructuredList]{}
	cacher.SetKeyFn(key)
	return cacher
}
func TestCacherShouldRenderFirstRun(t *testing.T) {
	g := NewWithT(t)
	m := newTestCacher() // mock

	m.On("hash", m.rr).Return(newHash(), nil).Once()
	m.On("render", m.ctx, m.rr).Return(m.r, nil).Once()

	res, acted, err := m.cacher.Render(m.ctx, m.rr, m.render)

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(res).Should(Equal(m.r))
	g.Expect(acted).Should(BeTrue())

	m.AssertExpectations(t)
}
func TestCacherShouldNotRenderSecondRun(t *testing.T) {
	g := NewWithT(t)
	m := newTestCacher()

	m.On("hash", m.rr).Return(newHash(), nil).Twice()
	m.On("render", m.ctx, m.rr).Return(m.r, nil).Once()

	m.cacher.Render(m.ctx, m.rr, m.render) //nolint:errcheck
	res, acted, err := m.cacher.Render(m.ctx, m.rr, m.render)

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(res).Should(Equal(m.r))
	g.Expect(acted).Should(BeFalse())

	m.AssertExpectations(t)
}

func TestCacherShouldRenderDifferentKey(t *testing.T) {
	g := NewWithT(t)
	m := newTestCacher()

	m.
		On("hash", m.rr).Return(newHash(), nil).Once().
		On("hash", m.rr).Return(newHash(), nil).Once()
	m.On("render", m.ctx, m.rr).Return(m.r, nil).Twice()

	m.cacher.Render(m.ctx, m.rr, m.render) //nolint:errcheck
	res, acted, err := m.cacher.Render(m.ctx, m.rr, m.render)

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(res).Should(Equal(m.r))
	g.Expect(acted).Should(BeTrue())

	m.AssertExpectations(t)
}

func TestCacherShouldRenderAfterInvalidation(t *testing.T) {
	g := NewWithT(t)
	m := newTestCacher()

	m.On("hash", m.rr).Return(newHash(), nil).Twice()
	m.On("render", m.ctx, m.rr).Return(m.r, nil).Twice()

	m.cacher.Render(m.ctx, m.rr, m.render) //nolint:errcheck
	m.cacher.InvalidateCache()
	res, acted, err := m.cacher.Render(m.ctx, m.rr, m.render)

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(res).Should(Equal(m.r))
	g.Expect(acted).Should(BeTrue())

	m.AssertExpectations(t)
}

func TestCacherShouldRenderIfKeyUnset(t *testing.T) {
	g := NewWithT(t)
	m := newTestCacher()

	m.cacher.SetKeyFn(nil)

	m.On("render", m.ctx, m.rr).Return(m.r, nil).Twice()

	m.cacher.Render(m.ctx, m.rr, m.render) //nolint:errcheck
	res, acted, err := m.cacher.Render(m.ctx, m.rr, m.render)

	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(res).Should(Equal(m.r))
	g.Expect(acted).Should(BeTrue())

	m.AssertExpectations(t)
}

func TestCacherShouldErrorIfKeyError(t *testing.T) {
	g := NewWithT(t)
	m := newTestCacher()

	keyErr := errors.New("hashing error")

	m.On("hash", m.rr).Return(newHash(), keyErr).Once()

	res, acted, err := m.cacher.Render(m.ctx, m.rr, m.render)

	g.Expect(err).Should(HaveOccurred())
	g.Expect(res).Should(BeNil())
	g.Expect(acted).Should(BeFalse())

	m.AssertExpectations(t)
}

func TestCacherShouldErrorIfRenderError(t *testing.T) {
	g := NewWithT(t)
	m := newTestCacher()

	renderErr := errors.New("hashing error")

	m.On("hash", m.rr).Return(newHash(), nil).Once()
	m.On("render", m.ctx, m.rr).Return(cacher.Zero[resources.UnstructuredList](), renderErr).Once()

	res, acted, err := m.cacher.Render(m.ctx, m.rr, m.render)

	g.Expect(err).Should(HaveOccurred())
	g.Expect(res).Should(BeNil())
	g.Expect(acted).Should(BeFalse())

	m.AssertExpectations(t)
}

func TestCacherShouldErrorIfKeyEmpty(t *testing.T) {
	g := NewWithT(t)
	m := newTestCacher()

	m.On("hash", m.rr).Return([]byte{}, nil).Once()

	res, acted, err := m.cacher.Render(m.ctx, m.rr, m.render)

	g.Expect(err).Should(HaveOccurred())
	g.Expect(res).Should(BeNil())
	g.Expect(acted).Should(BeFalse())

	m.AssertExpectations(t)
}
