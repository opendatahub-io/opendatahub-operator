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

package webhook_test

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"path/filepath"
	"testing"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	ctrlwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/codeflare"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/dashboard"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/datasciencepipelines"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/kserve"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/modelmeshserving"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/modelregistry"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/ray"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/trustyai"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/workbenches"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/webhook"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	namespace = "webhook-test-ns"
	nameBase  = "webhook-test"
	mrNS      = "model-registry-namespace"
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var cfg *rest.Config
var k8sClient client.Client
var testEnv *envtest.Environment
var gCtx context.Context
var gCancel context.CancelFunc

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Webhook Suite")
}

var _ = BeforeSuite(func() {
	// can't use suite's context as the manager should survive the function
	gCtx, gCancel = context.WithCancel(context.Background())

	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: false,
		WebhookInstallOptions: envtest.WebhookInstallOptions{
			Paths: []string{filepath.Join("..", "..", "config", "webhook")},
		},
	}

	var err error
	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	scheme := runtime.NewScheme()
	// DSCI
	err = dsciv1.AddToScheme(scheme)
	Expect(err).NotTo(HaveOccurred())
	// DSC
	err = dscv1.AddToScheme(scheme)
	Expect(err).NotTo(HaveOccurred())
	// Webhook
	err = admissionv1beta1.AddToScheme(scheme)
	Expect(err).NotTo(HaveOccurred())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	// start webhook server using Manager
	webhookInstallOptions := &testEnv.WebhookInstallOptions
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:         scheme,
		LeaderElection: false,
		Metrics: ctrlmetrics.Options{
			BindAddress: "0",
			CertDir:     webhookInstallOptions.LocalServingCertDir,
		},
		WebhookServer: ctrlwebhook.NewServer(ctrlwebhook.Options{
			Port:    webhookInstallOptions.LocalServingPort,
			TLSOpts: []func(*tls.Config){func(config *tls.Config) {}},
			Host:    webhookInstallOptions.LocalServingHost,
			CertDir: webhookInstallOptions.LocalServingCertDir,
		}),
	})
	Expect(err).NotTo(HaveOccurred())

	(&webhook.OpenDataHubValidatingWebhook{
		Client:  mgr.GetClient(),
		Decoder: admission.NewDecoder(mgr.GetScheme()),
	}).SetupWithManager(mgr)

	(&webhook.DSCDefaulter{}).SetupWithManager(mgr)

	// +kubebuilder:scaffold:webhook

	go func() {
		defer GinkgoRecover()
		err = mgr.Start(gCtx)
		Expect(err).NotTo(HaveOccurred())
	}()

	// wait for the webhook server to get ready
	dialer := &net.Dialer{Timeout: time.Second}
	addrPort := fmt.Sprintf("%s:%d", webhookInstallOptions.LocalServingHost, webhookInstallOptions.LocalServingPort)
	Eventually(func() error {
		conn, err := tls.DialWithDialer(dialer, "tcp", addrPort, &tls.Config{InsecureSkipVerify: true}) //nolint:gosec
		if err != nil {
			return err
		}
		conn.Close()
		return nil
	}).Should(Succeed())

})

var _ = AfterSuite(func() {
	gCancel()

	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

var _ = Describe("DSC/DSCI validating webhook", func() {
	It("Should not have more than one DSCI instance in the cluster", func(ctx context.Context) {
		desiredDsci := newDSCI(nameBase + "-dsci-1")
		Expect(k8sClient.Create(ctx, desiredDsci)).Should(Succeed())
		desiredDsci2 := newDSCI(nameBase + "-dsci-2")
		Expect(k8sClient.Create(ctx, desiredDsci2)).ShouldNot(Succeed())
		Expect(clearInstance(ctx, desiredDsci)).Should(Succeed())
	})

	It("Should block creation of second DSC instance", func(ctx context.Context) {
		dscSpec1 := newDSC(nameBase+"-dsc-1", namespace)
		Expect(k8sClient.Create(ctx, dscSpec1)).Should(Succeed())
		dscSpec2 := newDSC(nameBase+"-dsc-2", namespace)
		Expect(k8sClient.Create(ctx, dscSpec2)).ShouldNot(Succeed())
		Expect(clearInstance(ctx, dscSpec1)).Should(Succeed())
	})

	It("Should block deletion of DSCI instance when DSC instance exist", func(ctx context.Context) {
		dscInstance := newDSC(nameBase+"-dsc-1", "webhook-test-namespace")
		Expect(k8sClient.Create(ctx, dscInstance)).Should(Succeed())
		dsciInstance := newDSCI(nameBase + "-dsci-1")
		Expect(k8sClient.Create(ctx, dsciInstance)).Should(Succeed())
		Expect(k8sClient.Delete(ctx, dsciInstance)).ShouldNot(Succeed())
		Expect(clearInstance(ctx, dscInstance)).Should(Succeed())
		Expect(clearInstance(ctx, dsciInstance)).Should(Succeed())
	})

	It("Should allow deletion of DSCI instance when DSC instance does not exist", func(ctx context.Context) {
		dscInstance := newDSC(nameBase+"-dsc-1", "webhook-test-namespace")
		Expect(k8sClient.Create(ctx, dscInstance)).Should(Succeed())
		dsciInstance := newDSCI(nameBase + "-dsci-1")
		Expect(k8sClient.Create(ctx, dsciInstance)).Should(Succeed())
		Expect(k8sClient.Delete(ctx, dscInstance)).Should(Succeed())
		Expect(k8sClient.Delete(ctx, dsciInstance)).Should(Succeed())
	})

})

// mutating webhook tests for model registry.
var _ = Describe("DSC mutating webhook", func() {
	It("Should use defaults for DSC if empty string for MR namespace when MR is enabled", func(ctx context.Context) {
		dscInstance := newMRDSC1(nameBase+"-dsc-mr1", "", operatorv1.Managed)
		Expect(k8sClient.Create(ctx, dscInstance)).Should(Succeed())
		Expect(dscInstance.Spec.Components.ModelRegistry.RegistriesNamespace).
			Should(Equal(modelregistry.DefaultModelRegistriesNamespace))
		Expect(clearInstance(ctx, dscInstance)).Should(Succeed())
	})

	It("Should create DSC if no MR is set (for upgrade case)", func(ctx context.Context) {
		dscInstance := newMRDSC2(nameBase + "-dsc-mr2")
		Expect(k8sClient.Create(ctx, dscInstance)).Should(Succeed())
		Expect(clearInstance(ctx, dscInstance)).Should(Succeed())
	})
})

func clearInstance(ctx context.Context, instance client.Object) error {
	return k8sClient.Delete(ctx, instance)
}

func newDSCI(appName string) *dsciv1.DSCInitialization {
	monitoringNS := "monitoring-namespace"
	return &dsciv1.DSCInitialization{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DSCInitialization",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      appName,
			Namespace: namespace,
		},
		Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: namespace,
			Monitoring: dsciv1.Monitoring{
				Namespace:       monitoringNS,
				ManagementState: operatorv1.Managed,
			},
			TrustedCABundle: &dsciv1.TrustedCABundleSpec{
				ManagementState: operatorv1.Managed,
			},
		},
	}
}
func newDSC(name string, namespace string) *dscv1.DataScienceCluster {
	return &dscv1.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: dscv1.DataScienceClusterSpec{
			Components: dscv1.Components{
				Dashboard: dashboard.Dashboard{
					Component: components.Component{
						ManagementState: operatorv1.Removed,
					},
				},
				Workbenches: workbenches.Workbenches{
					Component: components.Component{
						ManagementState: operatorv1.Removed,
					},
				},
				ModelMeshServing: modelmeshserving.ModelMeshServing{
					Component: components.Component{
						ManagementState: operatorv1.Removed,
					},
				},
				DataSciencePipelines: datasciencepipelines.DataSciencePipelines{
					Component: components.Component{
						ManagementState: operatorv1.Removed,
					},
				},
				Kserve: kserve.Kserve{
					Component: components.Component{
						ManagementState: operatorv1.Removed,
					},
				},
				CodeFlare: codeflare.CodeFlare{
					Component: components.Component{
						ManagementState: operatorv1.Removed,
					},
				},
				Ray: ray.Ray{
					Component: components.Component{
						ManagementState: operatorv1.Removed,
					},
				},
				TrustyAI: trustyai.TrustyAI{
					Component: components.Component{
						ManagementState: operatorv1.Removed,
					},
				},
				ModelRegistry: modelregistry.ModelRegistry{
					Component: components.Component{
						ManagementState: operatorv1.Removed,
					},
				},
			},
		},
	}
}

func newMRDSC1(name string, mrNamespace string, state operatorv1.ManagementState) *dscv1.DataScienceCluster {
	return &dscv1.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "appNS",
		},
		Spec: dscv1.DataScienceClusterSpec{
			Components: dscv1.Components{
				ModelRegistry: modelregistry.ModelRegistry{
					Component: components.Component{
						ManagementState: state,
					},
					RegistriesNamespace: mrNamespace,
				},
			},
		},
	}
}

func newMRDSC2(name string) *dscv1.DataScienceCluster {
	return &dscv1.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "appNS",
		},
		Spec: dscv1.DataScienceClusterSpec{
			Components: dscv1.Components{
				Workbenches: workbenches.Workbenches{
					Component: components.Component{
						ManagementState: operatorv1.Removed,
					},
				},
			},
		},
	}
}
