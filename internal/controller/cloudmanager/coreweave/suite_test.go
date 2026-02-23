/*
Copyright 2026.

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

//nolint:testpackage
package coreweave

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	ccmv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/coreweave/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"
)

var (
	envTestClient client.Client
	envTestEnv    *envtest.Environment
)

var envTestScheme = runtime.NewScheme()

func TestMain(m *testing.M) {
	ctx, cancel := context.WithCancel(context.Background())

	logf.SetLogger(zap.New(zap.WriteTo(os.Stderr), zap.UseDevMode(true)))

	rootPath, pathErr := envtestutil.FindProjectRoot()
	if pathErr != nil {
		logf.Log.Error(pathErr, "failed to find project root")
		os.Exit(1)
	}

	envTestEnv = &envtest.Environment{
		CRDInstallOptions: envtest.CRDInstallOptions{
			Scheme: envTestScheme,
			Paths: []string{
				filepath.Join(rootPath, "config", "cloudmanager", "coreweave", "crd", "bases"),
			},
			ErrorIfPathMissing: true,
			CleanUpAfterUse:    false,
		},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := envTestEnv.Start()
	if err != nil {
		logf.Log.Error(err, "failed to start test environment")
		os.Exit(1)
	}

	utilruntime.Must(clientgoscheme.AddToScheme(envTestScheme))
	utilruntime.Must(ccmv1alpha1.AddToScheme(envTestScheme))

	envTestClient, err = client.New(cfg, client.Options{Scheme: envTestScheme})
	if err != nil {
		logf.Log.Error(err, "failed to create client")
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:         envTestScheme,
		LeaderElection: false,
		Metrics: ctrlmetrics.Options{
			BindAddress: "0",
		},
	})
	if err != nil {
		logf.Log.Error(err, "failed to create manager")
		os.Exit(1)
	}

	if err := NewReconciler(ctx, mgr); err != nil {
		logf.Log.Error(err, "failed to create reconciler")
		os.Exit(1)
	}

	go func() {
		if err := mgr.Start(ctx); err != nil {
			logf.Log.Error(err, "failed to start manager")
		}
	}()

	code := m.Run()

	cancel()
	if err := envTestEnv.Stop(); err != nil {
		logf.Log.Error(err, "failed to stop test environment")
	}

	os.Exit(code)
}
