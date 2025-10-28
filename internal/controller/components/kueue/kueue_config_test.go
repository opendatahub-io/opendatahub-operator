//nolint:testpackage
package kueue

import (
	"testing"

	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

// --- Test: Full Configuration ---.
func TestCreateKueueConfigurationCR_FullConfiguration(t *testing.T) {
	const kueueConfig = `
apiVersion: config.kueue.x-k8s.io/v1beta1
kind: Configuration
health:
  healthProbeBindAddress: :8081
metrics:
  bindAddress: :8443
  enableClusterQueueResources: true
webhook:
  port: 9443
leaderElection:
  leaderElect: true
  resourceName: c1f6bfd2.kueue.x-k8s.io
controller:
  groupKindConcurrency:
    Job.batch: 5
    Pod: 5
    Workload.kueue.x-k8s.io: 5
    LocalQueue.kueue.x-k8s.io: 1
    Cohort.kueue.x-k8s.io: 1
    ClusterQueue.kueue.x-k8s.io: 1
    ResourceFlavor.kueue.x-k8s.io: 1
clientConnection:
  qps: 50
  burst: 100
waitForPodsReady:
  enable: true
  blockAdmission: false
integrations:
  frameworks:
  - "pod"
  - "deployment"
  - "statefulset"
  - "batch/job"
  - "ray.io/rayjob"
  - "kubeflow.org/mpijob"
  - "ray.io/rayjob"
  - "ray.io/raycluster"
  - "jobset.x-k8s.io/jobset"
  - "kubeflow.org/paddlejob"
  - "kubeflow.org/pytorchjob"
  - "kubeflow.org/tfjob"
  - "kubeflow.org/xgboostjob"
  - "workload.codeflare.dev/appwrapper"
  - "leaderworkerset.x-k8s.io/leaderworkerset"
manageJobsWithoutQueueName: true
fairSharing:
  enable: true
`
	const kueueCR = `
apiVersion: kueue.openshift.io/v1
kind: Kueue
metadata:
  name: cluster
  annotations:
    opendatahub.io/managed: "false"
spec:
  managementState: Managed
  config:
    integrations:
      frameworks:
        - BatchJob
        - Deployment
        - JobSet
        - LeaderWorkerSet
        - MPIJob
        - PaddleJob
        - Pod
        - PyTorchJob
        - RayCluster
        - RayJob
        - StatefulSet
        - TFJob
        - XGBoostJob
    workloadManagement:
      labelPolicy: None
    gangScheduling:
      policy: ByWorkload
      byWorkload:
        admission: Parallel
    preemption:
      preemptionPolicy: FairSharing
      fairSharing:
        enable: true
`
	runKueueCRTest(t, kueueConfig, kueueCR)
}

// --- Test: Minimal Configuration ---.
func TestCreateKueueConfigurationCR_MinimalConfiguration(t *testing.T) {
	const kueueConfig = `
apiVersion: config.kueue.x-k8s.io/v1beta1
kind: Configuration
integrations:
  frameworks:
  - "batch/job"
  - "ray.io/rayjob"
`
	const kueueCR = `
apiVersion: kueue.openshift.io/v1
kind: Kueue
metadata:
  name: cluster
  annotations:
    opendatahub.io/managed: "false"
spec:
  managementState: Managed
  config:
    integrations:
      frameworks:
        - BatchJob
        - Deployment
        - Pod
        - PyTorchJob
        - RayCluster
        - RayJob
        - StatefulSet
`
	runKueueCRTest(t, kueueConfig, kueueCR)
}

// --- Test: Empty Configuration ---.
func TestCreateKueueConfigurationCR_EmptyConfiguration(t *testing.T) {
	const kueueConfig = `{}`

	const kueueCR = `
apiVersion: kueue.openshift.io/v1
kind: Kueue
metadata:
  name: cluster
  annotations:
    opendatahub.io/managed: "false"
spec:
  managementState: Managed
  config:
    integrations:
      frameworks:
        - Deployment
        - Pod
        - PyTorchJob
        - RayCluster
        - RayJob
        - StatefulSet
`
	runKueueCRTest(t, kueueConfig, kueueCR)
}

// --- Test: No Configuration Exists ---.
func TestCreateKueueConfigurationCR_NoConfigmapExists(t *testing.T) {
	const kueueConfig = ""

	const kueueCR = `
apiVersion: kueue.openshift.io/v1
kind: Kueue
metadata:
  name: cluster
  annotations:
    opendatahub.io/managed: "false"
spec:
  managementState: Managed
  config:
    integrations:
      frameworks:
        - Deployment
        - Pod
        - PyTorchJob
        - RayCluster
        - RayJob
        - StatefulSet
`
	runKueueCRTest(t, kueueConfig, kueueCR)
}

// --- Test: Gang Scheduling Disabled ---.
func TestCreateKueueConfigurationCR_GangSchedulingDisabled(t *testing.T) {
	const kueueConfig = `
apiVersion: config.kueue.x-k8s.io/v1beta1
kind: Configuration
waitForPodsReady:
  enable: false
`
	const kueueCR = `
apiVersion: kueue.openshift.io/v1
kind: Kueue
metadata:
  name: cluster
  annotations:
    opendatahub.io/managed: "false"
spec:
  managementState: Managed
  config:
    integrations:
      frameworks:
        - Deployment
        - Pod
        - PyTorchJob
        - RayCluster
        - RayJob
        - StatefulSet
`
	runKueueCRTest(t, kueueConfig, kueueCR)
}

// --- Test: External Frameworks Configuration ---.
func TestCreateKueueConfigurationCR_ExternalFrameworksConfiguration(t *testing.T) {
	const kueueConfig = `
apiVersion: config.kueue.x-k8s.io/v1beta1
kind: Configuration
integrations:
  frameworks:
  - "batch/job"
  externalFrameworks:
  - "kubeflow.org/mpijob"
  - "ray.io/rayjob"
  labelKeys:
  - "custom.label/key1"
  - "custom.label/key2"
`
	const kueueCR = `
apiVersion: kueue.openshift.io/v1
kind: Kueue
metadata:
  name: cluster
  annotations:
    opendatahub.io/managed: "false"
spec:
  managementState: Managed
  config:
    integrations:
      frameworks:
        - BatchJob
        - Deployment
        - Pod
        - PyTorchJob
        - RayCluster
        - RayJob
        - StatefulSet
      externalFrameworks:
        - MPIJob
        - RayJob
      labelKeys:
        - custom.label/key1
        - custom.label/key2
`
	runKueueCRTest(t, kueueConfig, kueueCR)
}

func TestCreateKueueConfigurationCR_EmptyConfigmapContent(t *testing.T) {
	const kueueConfig = ""
	const kueueCR = `
apiVersion: kueue.openshift.io/v1
kind: Kueue
metadata:
  name: cluster
  annotations:
    opendatahub.io/managed: "false"
spec:
  managementState: Managed
  config:
    integrations:
      frameworks:
        - Deployment
        - Pod
        - PyTorchJob
        - RayCluster
        - RayJob
        - StatefulSet
`
	runKueueCRTest(t, kueueConfig, kueueCR)
}

// --- Test: Only Workload Management ---.
func TestCreateKueueConfigurationCR_OnlyWorkloadMgmt(t *testing.T) {
	const kueueConfig = `
apiVersion: config.kueue.x-k8s.io/v1beta1
kind: Configuration
manageJobsWithoutQueueName: true
`
	const kueueCR = `
apiVersion: kueue.openshift.io/v1
kind: Kueue
metadata:
  name: cluster
  annotations:
    opendatahub.io/managed: "false"
spec:
  managementState: Managed
  config:
    integrations:
      frameworks:
        - Deployment
        - Pod
        - PyTorchJob
        - RayCluster
        - RayJob
        - StatefulSet
    workloadManagement:
      labelPolicy: None
`
	runKueueCRTest(t, kueueConfig, kueueCR)
}

// --- Test: Only Gang Scheduling ---.
func TestCreateKueueConfigurationCR_OnlyGangScheduling(t *testing.T) {
	const kueueConfig = `
apiVersion: config.kueue.x-k8s.io/v1beta1
kind: Configuration
waitForPodsReady:
  enable: true
`
	const kueueCR = `
apiVersion: kueue.openshift.io/v1
kind: Kueue
metadata:
  name: cluster
  annotations:
    opendatahub.io/managed: "false"
spec:
  managementState: Managed
  config:
    integrations:
      frameworks:
        - Deployment
        - Pod
        - PyTorchJob
        - RayCluster
        - RayJob
        - StatefulSet
    gangScheduling:
      policy: ByWorkload
      byWorkload:
        admission: Parallel
`
	runKueueCRTest(t, kueueConfig, kueueCR)
}

// --- Test: Only Preemption ---.
func TestCreateKueueConfigurationCR_OnlyPreemption(t *testing.T) {
	const kueueConfig = `
apiVersion: config.kueue.x-k8s.io/v1beta1
kind: Configuration
fairSharing:
  enable: true
`
	const kueueCR = `
apiVersion: kueue.openshift.io/v1
kind: Kueue
metadata:
  name: cluster
  annotations:
    opendatahub.io/managed: "false"
spec:
  managementState: Managed
  config:
    integrations:
      frameworks:
        - Deployment
        - Pod
        - PyTorchJob
        - RayCluster
        - RayJob
        - StatefulSet
    preemption:
      preemptionPolicy: FairSharing
      fairSharing:
        enable: true
`
	runKueueCRTest(t, kueueConfig, kueueCR)
}

// --- Test: Gang Scheduling Block Admission ---.
func TestCreateKueueConfigurationCR_GangSchedulingBlockAdmission(t *testing.T) {
	const kueueConfig = `
apiVersion: config.kueue.x-k8s.io/v1beta1
kind: Configuration
waitForPodsReady:
  enable: true
  blockAdmission: true
  timeout: 30s
`
	const kueueCR = `
apiVersion: kueue.openshift.io/v1
kind: Kueue
metadata:
  name: cluster
  annotations:
    opendatahub.io/managed: "false"
spec:
  managementState: Managed
  config:
    integrations:
      frameworks:
        - Deployment
        - Pod
        - PyTorchJob
        - RayCluster
        - RayJob
        - StatefulSet
    gangScheduling:
      policy: ByWorkload
      byWorkload:
        admission: Sequential
        timeout: 30s
`
	runKueueCRTest(t, kueueConfig, kueueCR)
}

// --- Test: Integrations Unknown Duplicate ---.
func TestCreateKueueConfigurationCR_IntegrationsUnknownDuplicate(t *testing.T) {
	const kueueConfig = `
apiVersion: config.kueue.x-k8s.io/v1beta1
kind: Configuration
integrations:
  frameworks:
    - "batch/job"
    - "unknown"
    - "batch/job"
`
	const kueueCR = `
apiVersion: kueue.openshift.io/v1
kind: Kueue
metadata:
  name: cluster
  annotations:
    opendatahub.io/managed: "false"
spec:
  managementState: Managed
  config:
    integrations:
      frameworks:
        - BatchJob
        - Deployment
        - Pod
        - PyTorchJob
        - RayCluster
        - RayJob
        - StatefulSet
`
	runKueueCRTest(t, kueueConfig, kueueCR)
}

// --- Test: Only External Frameworks ---.
func TestCreateKueueConfigurationCR_OnlyExternalFrameworks(t *testing.T) {
	const kueueConfig = `
apiVersion: config.kueue.x-k8s.io/v1beta1
kind: Configuration
integrations:
  externalFrameworks:
    - "kubeflow.org/mpijob"
    - "ray.io/rayjob"
`
	const kueueCR = `
apiVersion: kueue.openshift.io/v1
kind: Kueue
metadata:
  name: cluster
  annotations:
    opendatahub.io/managed: "false"
spec:
  managementState: Managed
  config:
    integrations:
      frameworks:
        - Deployment
        - Pod
        - PyTorchJob
        - RayCluster
        - RayJob
        - StatefulSet
      externalFrameworks:
        - MPIJob
        - RayJob
`
	runKueueCRTest(t, kueueConfig, kueueCR)
}

// --- Test: Only Label Keys ---.
func TestCreateKueueConfigurationCR_OnlyLabelKeys(t *testing.T) {
	const kueueConfig = `
apiVersion: config.kueue.x-k8s.io/v1beta1
kind: Configuration
integrations:
  labelKeys:
    - "custom.label/key1"
    - "custom.label/key2"
`
	const kueueCR = `
apiVersion: kueue.openshift.io/v1
kind: Kueue
metadata:
  name: cluster
  annotations:
    opendatahub.io/managed: "false"
spec:
  managementState: Managed
  config:
    integrations:
      frameworks:
        - Deployment
        - Pod
        - PyTorchJob
        - RayCluster
        - RayJob
        - StatefulSet
      labelKeys:
        - custom.label/key1
        - custom.label/key2
`
	runKueueCRTest(t, kueueConfig, kueueCR)
}

func runKueueCRTest(t *testing.T, configMapYAML string, expectedCRYAML string) {
	t.Helper()

	g := NewWithT(t)
	ctx := t.Context()

	fakeClient, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	// Create DSCI in fake client so actions.ApplicationNamespace() can fetch it
	dsci := &dsciv2.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-dsci",
		},
		Spec: dsciv2.DSCInitializationSpec{
			ApplicationsNamespace: "test-namespace",
		},
	}
	g.Expect(fakeClient.Create(ctx, dsci)).Should(Succeed())

	rr := &odhtypes.ReconciliationRequest{
		Client:   fakeClient,
		Instance: &componentApi.Kueue{},
	}

	if configMapYAML != "" {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      KueueConfigMapName,
				Namespace: "test-namespace",
			},
			Data: map[string]string{
				KueueConfigMapEntry: configMapYAML,
			},
		}

		g.Expect(fakeClient.Create(ctx, cm)).Should(Succeed())
	}

	result, err := createKueueCR(ctx, rr)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(result).ShouldNot(BeNil())

	actualCRYAML, err := yaml.Marshal(result.Object)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(string(actualCRYAML)).To(MatchYAML(expectedCRYAML))
}

func TestCreateKueueConfigurationCR_InvalidYAML(t *testing.T) {
	const kueueConfig = `
apiVersion: config.kueue.x-k8s.io/v1beta1
kind: Configuration
invalid: yaml: content: [
`
	// This test does not use 'kueueCR' since it expects an error.
	g := NewWithT(t)
	ctx := t.Context()

	// Setup fake client with scheme
	s, err := scheme.New()
	g.Expect(err).ShouldNot(HaveOccurred())
	fakeClient := fake.NewClientBuilder().WithScheme(s).Build()

	// Create configmap with invalid YAML
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kueue-manager-config",
			Namespace: "test-namespace",
		},
		Data: map[string]string{
			"controller_manager_config.yaml": kueueConfig,
		},
	}
	g.Expect(fakeClient.Create(ctx, cm)).Should(Succeed())

	// Create reconciliation request
	dsci := &dsciv2.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-dsci",
		},
		Spec: dsciv2.DSCInitializationSpec{
			ApplicationsNamespace: "test-namespace",
		},
	}
	g.Expect(fakeClient.Create(ctx, dsci)).Should(Succeed())

	rr := &odhtypes.ReconciliationRequest{
		Client: fakeClient,
		Instance: &componentApi.Kueue{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-kueue",
			},
		},
	}

	// Call the function under test
	result, err := createKueueCR(ctx, rr)

	g.Expect(err).Should(HaveOccurred())
	g.Expect(result).Should(BeNil())
	g.Expect(err.Error()).Should(ContainSubstring("failed to lookup kueue manager config"))
}
