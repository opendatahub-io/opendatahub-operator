/*
Copyright 2023.

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
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/datasciencecluster"
)

// InvalidManager is a deterministic stubbed manager that implements the manager.Manager interface
// and intentionally returns errors to test error handling paths.
type InvalidManager struct {
	client client.Client
	scheme *runtime.Scheme
	mapper meta.RESTMapper
}

// NewInvalidManager creates a new InvalidManager that will cause the reconciler to fail
// during client creation, providing a deterministic way to test error paths.
func NewInvalidManager() manager.Manager { //nolint:ireturn // Test mock intentionally returns interface
	return &InvalidManager{
		client: nil, // Will cause panic when accessed
		scheme: runtime.NewScheme(),
		mapper: nil,
	}
}

// GetClient returns nil to cause a panic when the reconciler tries to access it.
func (m *InvalidManager) GetClient() client.Client {
	return nil // This will cause a panic in the reconciler
}

// GetScheme returns an empty scheme that won't have required types.
func (m *InvalidManager) GetScheme() *runtime.Scheme {
	return m.scheme
}

// GetRESTMapper returns nil to cause failures.
func (m *InvalidManager) GetRESTMapper() meta.RESTMapper { //nolint:ireturn // Test mock must return interface
	return m.mapper
}

// GetConfig returns an invalid config that will fail client creation.
func (m *InvalidManager) GetConfig() *rest.Config {
	return &rest.Config{
		Host: "https://invalid-host-that-does-not-exist:6443",
		TLSClientConfig: rest.TLSClientConfig{
			Insecure: true,
		},
		Timeout:  time.Nanosecond,
		Username: "invalid-user",
		Password: "invalid-password",
	}
}

// GetFieldIndexer returns nil to cause failures.
func (m *InvalidManager) GetFieldIndexer() client.FieldIndexer { //nolint:ireturn // Test mock must return interface
	return nil
}

// GetEventRecorderFor returns nil to cause failures.
func (m *InvalidManager) GetEventRecorderFor(name string) record.EventRecorder { //nolint:ireturn // Test mock must return interface
	return nil
}

// GetCache returns nil to cause failures.
func (m *InvalidManager) GetCache() cache.Cache { //nolint:ireturn // Test mock must return interface
	return nil
}

// GetLogger returns a basic logger.
func (m *InvalidManager) GetLogger() logr.Logger {
	return logf.Log
}

// Add returns an error to simulate manager failure.
func (m *InvalidManager) Add(runnable manager.Runnable) error {
	return errors.New("invalid manager: cannot add runnable")
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

// AddHealthzCheck returns an error to simulate health check failure.
func (m *InvalidManager) AddHealthzCheck(name string, check healthz.Checker) error {
	return errors.New("invalid manager: cannot add health check")
}

// AddMetricsServerExtraHandler returns an error to simulate metrics failure.
func (m *InvalidManager) AddMetricsServerExtraHandler(name string, handler http.Handler) error {
	return errors.New("invalid manager: cannot add metrics handler")
}

// AddReadyzCheck returns an error to simulate ready check failure.
func (m *InvalidManager) AddReadyzCheck(name string, check healthz.Checker) error {
	return errors.New("invalid manager: cannot add ready check")
}

// GetAPIReader returns nil to cause failures.
func (m *InvalidManager) GetAPIReader() client.Reader { //nolint:ireturn // Test mock must return interface
	return nil
}

// GetControllerOptions returns default options.
func (m *InvalidManager) GetControllerOptions() config.Controller {
	return config.Controller{SkipNameValidation: ptr.To(true)}
}

// GetHTTPClient returns a basic HTTP client.
func (m *InvalidManager) GetHTTPClient() *http.Client {
	return &http.Client{}
}

// GetWebhookServer returns nil to cause failures.
func (m *InvalidManager) GetWebhookServer() webhook.Server { //nolint:ireturn // Test mock must return interface
	return nil
}

func TestInvalidManagerWithReconciler(t *testing.T) {
	g := gomega.NewWithT(t)

	// Test that calling the reconciler with our InvalidManager causes a panic
	invalidMgr := NewInvalidManager()
	ctx := t.Context()

	// This should panic because GetClient() returns nil
	g.Expect(func() {
		_ = datasciencecluster.NewDataScienceClusterReconcilerWithName(ctx, invalidMgr, "test-invalid-manager")
	}).To(gomega.Panic())
}
