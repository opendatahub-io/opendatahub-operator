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

	dsc "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	dsci "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/codeflare"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/dashboard"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/datasciencepipelines"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/kserve"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/modelmeshserving"
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
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var cfg *rest.Config
var k8sClient client.Client
var testEnv *envtest.Environment
var ctx context.Context
var cancel context.CancelFunc

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Webhook Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

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
	err = dsci.AddToScheme(scheme)
	Expect(err).NotTo(HaveOccurred())
	// DSC
	err = dsc.AddToScheme(scheme)
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
		Scheme:             scheme,
		Host:               webhookInstallOptions.LocalServingHost,
		Port:               webhookInstallOptions.LocalServingPort,
		CertDir:            webhookInstallOptions.LocalServingCertDir,
		LeaderElection:     false,
		MetricsBindAddress: "0",
	})
	Expect(err).NotTo(HaveOccurred())

	(&webhook.OpenDataHubWebhook{}).SetupWithManager(mgr)

	//+kubebuilder:scaffold:webhook

	go func() {
		defer GinkgoRecover()
		err = mgr.Start(ctx)
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
	cancel()
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

var _ = Describe("DSC/DSCI webhook", func() {
	It("Should not have more than one DSCI instance in the cluster", func() {
		desiredDsci := newDSCI(nameBase + "-dsci-1")
		Expect(k8sClient.Create(context.Background(), desiredDsci)).Should(Succeed())
		desiredDsci2 := newDSCI(nameBase + "-dsci-2")
		Expect(k8sClient.Create(context.Background(), desiredDsci2)).ShouldNot(Succeed())
	})

	It("Should block creation of second DSC instance", func() {
		dscSpec := newDSC(nameBase+"-dsc-1", namespace)
		Expect(k8sClient.Create(context.Background(), dscSpec)).Should(Succeed())
		dscSpec = newDSC(nameBase+"-dsc-2", namespace)
		Expect(k8sClient.Create(context.Background(), dscSpec)).ShouldNot(Succeed())
	})
})

func newDSCI(appName string) *dsci.DSCInitialization {
	monitoringNS := "monitoring-namespace"
	return &dsci.DSCInitialization{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DSCInitialization",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      appName,
			Namespace: namespace,
		},
		Spec: dsci.DSCInitializationSpec{
			ApplicationsNamespace: namespace,
			Monitoring: dsci.Monitoring{
				Namespace:       monitoringNS,
				ManagementState: operatorv1.Managed,
			},
			TrustedCABundle: dsci.TrustedCABundleSpec{
				ManagementState: operatorv1.Managed,
			},
		},
	}
}
func newDSC(name string, namespace string) *dsc.DataScienceCluster {
	return &dsc.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: dsc.DataScienceClusterSpec{
			Components: dsc.Components{
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
			},
		},
	}
}
