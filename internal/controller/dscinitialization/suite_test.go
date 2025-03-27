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
	"path/filepath"
	"testing"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	routev1 "github.com/openshift/api/route/v1"
	templatev1 "github.com/openshift/api/template/v1"
	userv1 "github.com/openshift/api/user/v1"
	ofapi "github.com/operator-framework/api/pkg/operators/v1alpha1"
	ofapiv2 "github.com/operator-framework/api/pkg/operators/v2"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	dscictrl "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/dscinitialization"
	odhClient "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/client"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	cfg       *rest.Config
	k8sClient client.Client
	testEnv   *envtest.Environment
	gCtx      context.Context
	gCancel   context.CancelFunc
)

const (
	timeout  = 30 * time.Second // change this from original 20 to 30 because we often failed in post cleanup job
	interval = 250 * time.Millisecond
)

func TestDataScienceClusterInitialization(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Data Science Cluster Initialization Controller Suite")
}

var testScheme = runtime.NewScheme()

var _ = BeforeSuite(func() {
	// can't use suite's context as the manager should survive the function
	//nolint:fatcontext
	gCtx, gCancel = context.WithCancel(context.Background())

	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	rootPath, pathErr := envtestutil.FindProjectRoot()
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
	utilruntime.Must(dsciv1.AddToScheme(testScheme))
	utilruntime.Must(dscv1.AddToScheme(testScheme))
	utilruntime.Must(networkingv1.AddToScheme(testScheme))
	utilruntime.Must(rbacv1.AddToScheme(testScheme))
	utilruntime.Must(corev1.AddToScheme(testScheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(testScheme))
	utilruntime.Must(appsv1.AddToScheme(testScheme))
	utilruntime.Must(ofapi.AddToScheme(testScheme))
	utilruntime.Must(ofapiv2.AddToScheme(testScheme))
	utilruntime.Must(routev1.Install(testScheme))
	utilruntime.Must(userv1.Install(testScheme))
	utilruntime.Must(monitoringv1.AddToScheme(testScheme))
	utilruntime.Must(templatev1.Install(testScheme))
	utilruntime.Must(configv1.Install(testScheme))
	utilruntime.Must(serviceApi.AddToScheme(testScheme))
	utilruntime.Must(monitoringv1.AddToScheme(testScheme))
	// +kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: testScheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	odhClient, err := odhClient.NewFromConfig(cfg, k8sClient)
	Expect(err).NotTo(HaveOccurred())
	Expect(odhClient).NotTo(BeNil())

	webhookInstallOptions := &testEnv.WebhookInstallOptions
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:         testScheme,
		LeaderElection: false,
		Metrics: ctrlmetrics.Options{
			BindAddress: "0",
			CertDir:     webhookInstallOptions.LocalServingCertDir,
		},
	})

	Expect(err).NotTo(HaveOccurred())

	err = (&dscictrl.DSCInitializationReconciler{
		Client:   odhClient,
		Scheme:   testScheme,
		Recorder: mgr.GetEventRecorderFor("dscinitialization-controller"),
	}).SetupWithManager(gCtx, mgr)

	Expect(err).ToNot(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err = mgr.Start(gCtx)
		Expect(err).ToNot(HaveOccurred(), "Failed to run manager")
	}()
})

var _ = AfterSuite(func() {
	gCancel()
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})
