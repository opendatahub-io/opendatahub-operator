package testf

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/onsi/gomega"
	"github.com/onsi/gomega/matchers"
	"github.com/onsi/gomega/types"
)

type Mode int

const (
	eventually Mode = iota
	consistently
)

type EventuallyValue[T any] struct {
	ctx context.Context
	g   *gomega.WithT
	f   func(context.Context) (T, error)
}

func (e *EventuallyValue[T]) Get() (T, error) {
	v, err := e.f(e.ctx)

	var pse gomega.PollingSignalError
	if errors.As(err, &pse) {
		if ue := errors.Unwrap(err); ue != nil {
			err = ue
		}
	}

	return v, err
}

func (e *EventuallyValue[T]) Eventually(args ...interface{}) *Assertion[T] {
	return &Assertion[T]{
		ctx:     e.ctx,
		g:       e.g,
		f:       e.f,
		args:    args,
		m:       eventually,
		timeout: e.g.DurationBundle.EventuallyTimeout,
		polling: e.g.DurationBundle.EventuallyPollingInterval,
	}
}

func (e *EventuallyValue[T]) Consistently(args ...interface{}) *Assertion[T] {
	return &Assertion[T]{
		ctx:     e.ctx,
		g:       e.g,
		f:       e.f,
		args:    args,
		m:       consistently,
		timeout: e.g.DurationBundle.ConsistentlyDuration,
		polling: e.g.DurationBundle.ConsistentlyPollingInterval,
	}
}

type Assertion[T any] struct {
	ctx  context.Context
	g    *gomega.WithT
	f    func(context.Context) (T, error)
	args []interface{}

	m Mode

	timeout time.Duration
	polling time.Duration
}

func (a *Assertion[T]) WithTimeout(interval time.Duration) *Assertion[T] {
	a.timeout = interval
	return a
}

func (a *Assertion[T]) WithPolling(interval time.Duration) *Assertion[T] {
	a.polling = interval
	return a
}

func (a *Assertion[T]) WithContext(ctx context.Context) *Assertion[T] {
	a.ctx = ctx
	return a
}

func (a *Assertion[T]) build(f interface{}) gomega.AsyncAssertion {
	var aa gomega.AsyncAssertion

	switch a.m {
	case eventually:
		aa = a.g.Eventually(f, a.args...)
	case consistently:
		aa = a.g.Consistently(f, a.args...)
	default:
		panic("unsupported mode")
	}

	aa = aa.WithContext(a.ctx)

	if a.timeout > 0 {
		aa = aa.WithTimeout(a.timeout)
	}
	if a.polling > 0 {
		aa = aa.WithPolling(a.polling)
	}

	return aa
}

//nolint:dupl
func (a *Assertion[T]) Should(matcher types.GomegaMatcher, optionalDescription ...interface{}) T {
	var res atomic.Value
	var wrapper interface{}

	switch matcher.(type) {
	case *matchers.SucceedMatcher:
		wrapper = func(ctx context.Context) error {
			v, err := a.f(ctx)
			res.Store(v)

			return err
		}
	case *matchers.MatchErrorMatcher:
		wrapper = func(ctx context.Context) error {
			v, err := a.f(ctx)
			res.Store(v)

			return err
		}
	default:
		wrapper = func(ctx context.Context) (T, error) {
			v, err := a.f(ctx)
			res.Store(v)

			return v, err
		}
	}

	a.build(wrapper).Should(matcher, optionalDescription...)

	//nolint:forcetypeassert,errcheck
	return res.Load().(T)
}

//nolint:dupl
func (a *Assertion[T]) ShouldNot(matcher types.GomegaMatcher, optionalDescription ...interface{}) T {
	var res atomic.Value
	var wrapper interface{}

	switch matcher.(type) {
	case *matchers.SucceedMatcher:
		wrapper = func(ctx context.Context) error {
			v, err := a.f(ctx)
			res.Store(v)

			return err
		}
	case *matchers.MatchErrorMatcher:
		wrapper = func(ctx context.Context) error {
			v, err := a.f(ctx)
			res.Store(v)

			return err
		}
	default:
		wrapper = func(ctx context.Context) (T, error) {
			v, err := a.f(ctx)
			res.Store(v)

			return v, err
		}
	}

	a.build(wrapper).ShouldNot(matcher, optionalDescription...)

	//nolint:forcetypeassert,errcheck
	return res.Load().(T)
}

type EventuallyErr struct {
	ctx context.Context
	g   *gomega.WithT
	f   func(context.Context) error
}

func (e *EventuallyErr) Get() error {
	err := e.f(e.ctx)

	var pse gomega.PollingSignalError
	if errors.As(err, &pse) {
		if ue := errors.Unwrap(err); ue != nil {
			err = ue
		}
	}

	return err
}

func (e *EventuallyErr) Eventually() types.AsyncAssertion {
	return e.g.Eventually(e.ctx, e.f).
		WithContext(e.ctx).
		WithTimeout(e.g.DurationBundle.EventuallyTimeout).
		WithPolling(e.g.DurationBundle.EventuallyPollingInterval)
}

func (e *EventuallyErr) Consistently() types.AsyncAssertion {
	return e.g.Consistently(e.ctx, e.f).
		WithContext(e.ctx).
		WithTimeout(e.g.DurationBundle.ConsistentlyDuration).
		WithPolling(e.g.DurationBundle.ConsistentlyPollingInterval)
}
