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

package dscinitialization_test

import (
	"context"
	"k8s.io/apimachinery/pkg/runtime"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	util "github.com/opendatahub-io/opendatahub-operator/v2/controllers/test"

	dscinitializationv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	dsci "github.com/opendatahub-io/opendatahub-operator/v2/controllers/dscinitialization"
	routev1 "github.com/openshift/api/route/v1"
	userv1 "github.com/openshift/api/user/v1"
	ofapi "github.com/operator-framework/api/pkg/operators/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	authv1 "k8s.io/api/rbac/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	cfg       *rest.Config
	k8sClient client.Client
	testEnv   *envtest.Environment
	ctx       context.Context
	cancel    context.CancelFunc
)

const (
	timeout  = 20 * time.Second
	interval = 250 * time.Millisecond
)

func TestDataScienceClusterInitialization(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Data Science Cluster Initialization Controller Suite")
}

var testScheme = runtime.NewScheme()

var _ = BeforeSuite(func() {
	ctx, cancel = context.WithCancel(context.TODO())
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	By("bootstrapping test environment")
	rootPath, pathErr := util.FindProjectRoot()
	Expect(pathErr).ToNot(HaveOccurred(), pathErr)

	testEnv = &envtest.Environment{
		CRDInstallOptions: envtest.CRDInstallOptions{
			Scheme: testScheme,
			Paths: []string{
				filepath.Join(rootPath, "config", "crd", "bases"),
				filepath.Join(rootPath, "config", "crd", "external"),
			},
			ErrorIfPathMissing: true,
			CleanUpAfterUse:    false,
		},
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	utilruntime.Must(clientgoscheme.AddToScheme(testScheme))
	utilruntime.Must(dscinitializationv1.AddToScheme(testScheme))
	utilruntime.Must(netv1.AddToScheme(testScheme))
	utilruntime.Must(authv1.AddToScheme(testScheme))
	utilruntime.Must(corev1.AddToScheme(testScheme))
	utilruntime.Must(apiextv1.AddToScheme(testScheme))
	utilruntime.Must(appsv1.AddToScheme(testScheme))
	utilruntime.Must(ofapi.AddToScheme(testScheme))
	utilruntime.Must(routev1.Install(testScheme))
	utilruntime.Must(userv1.Install(testScheme))
	//+kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: testScheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:             testScheme,
		LeaderElection:     false,
		MetricsBindAddress: "0",
	})

	Expect(err).NotTo(HaveOccurred())

	err = (&dsci.DSCInitializationReconciler{
		Client:   k8sClient,
		Scheme:   testScheme,
		Log:      ctrl.Log.WithName("controllers").WithName("DSCInitialization"),
		Recorder: mgr.GetEventRecorderFor("dscinitialization-controller"),
	}).SetupWithManager(mgr)

	Expect(err).ToNot(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err = mgr.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "Failed to run manager")
	}()
})

var _ = AfterSuite(func() {
	cancel()
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})
