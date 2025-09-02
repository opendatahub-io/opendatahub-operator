/*
Copyright 2023-2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package datasciencecluster_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/datasciencecluster"
)

// invalidManagerPanicSentinel is the centralized sentinel panic message used by InvalidManager
// to ensure consistent, order-agnostic test behavior regardless of which method is called first.
const invalidManagerPanicSentinel = "INVALID_MANAGER_SENTINEL_PANIC"

// InvalidManager is a deterministic stubbed manager that implements the manager.Manager interface
// and intentionally panics with a consistent sentinel to test error handling paths.
//
// This test uses a unique sentinel panic message to ensure reliable detection regardless
// of constructor call order. The panic contains the invalidManagerSentinel which
// is checked using gomega.ContainSubstring for robust assertion.
type InvalidManager struct {
	scheme     *runtime.Scheme
	httpClient *http.Client
}

// Compile-time interface conformance check to ensure InvalidManager implements manager.Manager.
var _ manager.Manager = (*InvalidManager)(nil)

// NewInvalidManager creates a new InvalidManager that will cause the reconciler to fail
// by panicking with a unique sentinel when GetClient() is called, providing a deterministic
// and robust way to test error paths regardless of constructor call order.
func NewInvalidManager() manager.Manager {
	return &InvalidManager{
		scheme: runtime.NewScheme(),
		httpClient: &http.Client{
			Timeout: 500 * time.Millisecond,
		},
	}
}

// GetClient panics with the sentinel to cause a consistent failure.
func (m *InvalidManager) GetClient() client.Client {
	panic(invalidManagerPanicSentinel + ": nil client")
}

// GetScheme returns an empty scheme that won't have required types.
func (m *InvalidManager) GetScheme() *runtime.Scheme {
	if m.scheme == nil {
		panic("InvalidManager: scheme not initialized")
	}
	return m.scheme
}

// GetRESTMapper panics with the sentinel to cause a consistent failure.
func (m *InvalidManager) GetRESTMapper() meta.RESTMapper {
	panic(invalidManagerPanicSentinel + ": nil REST mapper")
}

// GetConfig returns an invalid config that will fail client creation.
func (m *InvalidManager) GetConfig() *rest.Config {
	panic(invalidManagerPanicSentinel + ": invalid rest.Config")
}

// GetFieldIndexer panics with the sentinel to cause a consistent failure.
func (m *InvalidManager) GetFieldIndexer() client.FieldIndexer {
	panic(invalidManagerPanicSentinel + ": nil field indexer")
}

// GetEventRecorderFor panics with the sentinel to cause a consistent failure.
func (m *InvalidManager) GetEventRecorderFor(name string) record.EventRecorder {
	panic(invalidManagerPanicSentinel + ": nil event recorder")
}

// GetCache panics with the sentinel to cause a consistent failure.
func (m *InvalidManager) GetCache() cache.Cache {
	panic(invalidManagerPanicSentinel + ": nil cache")
}

// GetLogger returns a discard logger to avoid test dependency on global logger state.
func (m *InvalidManager) GetLogger() logr.Logger {
	return logr.Discard()
}

// Add panics with the sentinel to cause a consistent failure.
func (m *InvalidManager) Add(runnable manager.Runnable) error {
	panic(invalidManagerPanicSentinel + ": cannot add runnable")
}

// Elected returns a closed channel to simulate no election.
func (m *InvalidManager) Elected() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

// Start returns an error to simulate manager start failure.
func (m *InvalidManager) Start(ctx context.Context) error {
	return errors.New("invalid manager: cannot start")
}

// AddHealthzCheck panics with the sentinel to cause a consistent failure.
func (m *InvalidManager) AddHealthzCheck(name string, check healthz.Checker) error {
	panic(invalidManagerPanicSentinel + ": cannot add health check")
}

// AddMetricsServerExtraHandler panics with the sentinel to cause a consistent failure.
func (m *InvalidManager) AddMetricsServerExtraHandler(path string, handler http.Handler) error {
	panic(invalidManagerPanicSentinel + ": cannot add metrics handler")
}

// AddReadyzCheck panics with the sentinel to cause a consistent failure.
func (m *InvalidManager) AddReadyzCheck(name string, check healthz.Checker) error {
	panic(invalidManagerPanicSentinel + ": cannot add ready check")
}

// GetAPIReader panics with the sentinel to cause a consistent failure.
func (m *InvalidManager) GetAPIReader() client.Reader {
	panic(invalidManagerPanicSentinel + ": nil API reader")
}

// GetControllerOptions returns default options.
func (m *InvalidManager) GetControllerOptions() config.Controller {
	return config.Controller{}
}

// GetWebhookServer panics with the sentinel to cause a consistent failure.
func (m *InvalidManager) GetWebhookServer() webhook.Server {
	panic(invalidManagerPanicSentinel + ": nil webhook server")
}

// GetHTTPClient returns a shared HTTP client with a short timeout to prevent test hangs.
func (m *InvalidManager) GetHTTPClient() *http.Client {
	return m.httpClient
}

// SetFields returns an error to simulate injection failure.
func (m *InvalidManager) SetFields(_ interface{}) error {
	return errors.New("invalid manager: cannot set fields")
}

func TestInvalidManagerWithReconciler(t *testing.T) {
	t.Parallel()
	g := gomega.NewWithT(t)

	// Test that calling the reconciler with our InvalidManager causes a panic
	invalidMgr := NewInvalidManager()
	ctx := t.Context()

	// This should panic because GetClient() explicitly panics with our sentinel
	// invalidManagerPanicSentinel + ": nil client". We use a custom matcher to check
	// for the sentinel substring, making the test more robust against changes in
	// constructor call order
	g.Expect(func() {
		_ = datasciencecluster.NewDataScienceClusterReconcilerWithName(ctx, invalidMgr, "test-invalid-manager")
	}).To(gomega.PanicWith(gomega.ContainSubstring(invalidManagerPanicSentinel)))
}
