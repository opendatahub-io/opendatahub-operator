package e2e_test

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/rs/xid"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	ctrlcfg "sigs.k8s.io/controller-runtime/pkg/client/config"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"
)

const (
	sparkVersion       = "4.0.1"
	sparkImage         = "quay.io/opendatahub/data-processing:Spark-v" + sparkVersion
	pysparkClientImage = "quay.io/fedora/python-312:latest"
)

type SparkOperatorTestCtx struct {
	*ComponentTestCtx
}

func sparkOperatorTestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewComponentTestCtx(t, &componentApi.SparkOperator{})
	require.NoError(t, err)

	componentCtx := SparkOperatorTestCtx{
		ComponentTestCtx: ct,
	}

	// Define test cases.
	testCases := []TestCase{
		{"Validate component enabled", componentCtx.ValidateComponentEnabled},
		{"Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences},
		{"Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources},
		{"Validate component releases", componentCtx.ValidateComponentReleases},
		{"Validate SparkPi workload execution", componentCtx.ValidateSparkPiWorkload},
		{"Validate ScheduledSparkApplication workload execution", componentCtx.ValidateScheduledSparkPiWorkload},
		{"Validate SparkConnect server provisioning", componentCtx.ValidateSparkConnectServer},
		{"Validate resource deletion recovery", componentCtx.ValidateAllDeletionRecovery},
		{"Validate component disabled", componentCtx.ValidateComponentDisabled},
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}

// ValidateSparkPiWorkload validates that a SparkApplication can run successfully.
func (tc *SparkOperatorTestCtx) ValidateSparkPiWorkload(t *testing.T) {
	t.Helper()

	// Use a unique name to avoid conflicts with previous test runs
	sparkAppName := "spark-pi-" + xid.New().String()
	// Run in the applications namespace where spark-operator-spark SA exists
	namespace := tc.AppsNamespace

	t.Logf("Creating SparkApplication %s in namespace %s", sparkAppName, namespace)
	sparkApp := tc.createSparkPiApplication(sparkAppName, namespace)

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(sparkApp),
		WithCustomErrorMsg("Failed to create SparkApplication %s", sparkAppName),
	)

	// Cleanup SparkApplication after test
	defer func() {
		t.Logf("Cleaning up SparkApplication %s", sparkAppName)
		tc.DeleteResource(
			WithMinimalObject(gvk.SparkApplication, types.NamespacedName{Name: sparkAppName, Namespace: namespace}),
			WithIgnoreNotFound(true),
			WithWaitForDeletion(true),
		)
	}()

	t.Logf("Waiting for SparkApplication %s to complete", sparkAppName)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.SparkApplication, types.NamespacedName{Name: sparkAppName, Namespace: namespace}),
		WithCondition(
			jq.Match(`.status.applicationState.state == "COMPLETED"`),
		),
		WithCustomErrorMsg("SparkApplication %s did not complete successfully", sparkAppName),
		WithEventuallyTimeout(tc.TestTimeouts.defaultEventuallyTimeout),
		WithEventuallyPollingInterval(tc.TestTimeouts.defaultEventuallyPollInterval),
	)

	t.Logf("SparkApplication %s completed successfully", sparkAppName)
}

// ValidateScheduledSparkPiWorkload validates that a ScheduledSparkApplication can run successfully.
func (tc *SparkOperatorTestCtx) ValidateScheduledSparkPiWorkload(t *testing.T) {
	t.Helper()

	scheduledSparkAppName := "scheduled-spark-pi-" + xid.New().String()
	namespace := tc.AppsNamespace

	t.Logf("Creating ScheduledSparkApplication %s in namespace %s", scheduledSparkAppName, namespace)
	scheduledSparkApp := tc.createScheduledSparkPiApplication(scheduledSparkAppName, namespace)

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(scheduledSparkApp),
		WithCustomErrorMsg("Failed to create ScheduledSparkApplication %s", scheduledSparkAppName),
	)

	var lastRunName string

	defer func() {
		t.Logf("Cleaning up ScheduledSparkApplication %s", scheduledSparkAppName)
		tc.DeleteResource(
			WithMinimalObject(gvk.ScheduledSparkApplication, types.NamespacedName{Name: scheduledSparkAppName, Namespace: namespace}),
			WithIgnoreNotFound(true),
			WithWaitForDeletion(true),
		)

		if lastRunName != "" {
			t.Logf("Verifying cascade deletion of SparkApplication %s", lastRunName)
			tc.EnsureResourceGone(
				WithMinimalObject(gvk.SparkApplication, types.NamespacedName{Name: lastRunName, Namespace: namespace}),
				WithCustomErrorMsg("cascade deletion failed: SparkApplication %s still exists after deleting parent", lastRunName),
			)
		}
	}()

	t.Logf("Waiting for ScheduledSparkApplication %s to schedule a run", scheduledSparkAppName)
	scheduledApp := tc.EnsureResourceExists(
		WithMinimalObject(gvk.ScheduledSparkApplication, types.NamespacedName{Name: scheduledSparkAppName, Namespace: namespace}),
		WithCondition(
			jq.Match(`.status.lastRunName != null and .status.lastRunName != ""`),
		),
		WithCustomErrorMsg("ScheduledSparkApplication %s did not schedule a run", scheduledSparkAppName),
		WithEventuallyTimeout(tc.TestTimeouts.defaultEventuallyTimeout),
		WithEventuallyPollingInterval(tc.TestTimeouts.defaultEventuallyPollInterval),
	)

	lastRunName, found, err := unstructured.NestedString(scheduledApp.Object, "status", "lastRunName")
	require.NoError(t, err, "Failed to extract lastRunName from ScheduledSparkApplication status")
	require.True(t, found, "lastRunName not found in ScheduledSparkApplication status")

	t.Logf("ScheduledSparkApplication %s created SparkApplication: %s", scheduledSparkAppName, lastRunName)

	t.Logf("Suspending ScheduledSparkApplication %s to prevent additional runs", scheduledSparkAppName)
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.ScheduledSparkApplication, types.NamespacedName{Name: scheduledSparkAppName, Namespace: namespace}),
		WithMutateFunc(testf.Transform(`.spec.suspend = true`)),
	)

	t.Logf("Waiting for SparkApplication %s to reach terminal state", lastRunName)
	completedApp := tc.EnsureResourceExists(
		WithMinimalObject(gvk.SparkApplication, types.NamespacedName{Name: lastRunName, Namespace: namespace}),
		WithCondition(
			jq.Match(`.status.applicationState.state == "COMPLETED" or .status.applicationState.state == "FAILED"`),
		),
		WithCustomErrorMsg("SparkApplication %s (created by ScheduledSparkApplication) did not reach terminal state", lastRunName),
		WithEventuallyTimeout(tc.TestTimeouts.defaultEventuallyTimeout),
		WithEventuallyPollingInterval(tc.TestTimeouts.defaultEventuallyPollInterval),
	)

	state, found, err := unstructured.NestedString(completedApp.Object, "status", "applicationState", "state")
	require.NoError(t, err, "Failed to extract state from SparkApplication %s status", lastRunName)
	require.True(t, found, "state not found in SparkApplication %s status", lastRunName)
	require.Equal(t, "COMPLETED", state, "SparkApplication %s ended in %s state instead of COMPLETED", lastRunName, state)

	t.Logf("ScheduledSparkApplication %s successfully scheduled and executed SparkApplication %s", scheduledSparkAppName, lastRunName)
}

// createSparkPiApplication creates a SparkApplication CR for spark-pi test.
func (tc *SparkOperatorTestCtx) createSparkPiApplication(name, namespace string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "sparkoperator.k8s.io/v1beta2",
			"kind":       "SparkApplication",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
			},
			"spec": sparkPiSpec(),
		},
	}
}

func sparkPiSpec() map[string]any {
	return map[string]any{
		"type":                "Scala",
		"mode":                "cluster",
		"image":               sparkImage,
		"imagePullPolicy":     "IfNotPresent",
		"mainClass":           "org.apache.spark.examples.SparkPi",
		"mainApplicationFile": fmt.Sprintf("local:///opt/spark/examples/jars/spark-examples_2.13-%s.jar", sparkVersion),
		"arguments":           []any{"1000"},
		"sparkVersion":        sparkVersion,
		"restartPolicy": map[string]any{
			"type": "Never",
		},
		"sparkConf": map[string]any{
			"spark.driver.port":              "8080",
			"spark.driver.blockManager.port": "8082",
			"spark.blockManager.port":        "8081",
			"spark.port.maxRetries":          "0",
		},
		"volumes": []any{
			map[string]any{
				"name":     "spark-work-dir",
				"emptyDir": map[string]any{},
			},
		},
		"driver": map[string]any{
			"labels": map[string]any{
				"version": sparkVersion,
			},
			"cores":           int64(1),
			"coreLimit":       "1200m",
			"memory":          "512m",
			"serviceAccount":  "spark-operator-spark",
			"securityContext": map[string]any{},
			"volumeMounts": []any{
				map[string]any{
					"name":      "spark-work-dir",
					"mountPath": "/opt/spark/work-dir",
				},
			},
		},
		"executor": map[string]any{
			"labels": map[string]any{
				"version": sparkVersion,
			},
			"instances":       int64(1),
			"cores":           int64(1),
			"memory":          "512m",
			"securityContext": map[string]any{},
			"volumeMounts": []any{
				map[string]any{
					"name":      "spark-work-dir",
					"mountPath": "/opt/spark/work-dir",
				},
			},
		},
	}
}

func (tc *SparkOperatorTestCtx) createScheduledSparkPiApplication(name, namespace string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "sparkoperator.k8s.io/v1beta2",
			"kind":       "ScheduledSparkApplication",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]any{
				"schedule":                  "@every 30s",
				"concurrencyPolicy":         "Forbid",
				"successfulRunHistoryLimit": int64(1),
				"failedRunHistoryLimit":     int64(1),
				"template":                  sparkPiSpec(),
			},
		},
	}
}

// ValidateSparkConnectServer validates that a SparkConnect server can be provisioned
// and a separate PySpark client pod can connect and execute a query via the Spark Connect protocol.
func (tc *SparkOperatorTestCtx) ValidateSparkConnectServer(t *testing.T) {
	t.Helper()

	sparkConnectName := "spark-connect-" + xid.New().String()
	clientPodName := "pyspark-client-" + xid.New().String()
	namespace := tc.AppsNamespace
	containerName := "pyspark-client"

	t.Logf("Creating SparkConnect %s in namespace %s", sparkConnectName, namespace)
	sparkConnect := tc.createSparkConnectInstance(sparkConnectName, namespace)

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(sparkConnect),
		WithCustomErrorMsg("Failed to create SparkConnect %s", sparkConnectName),
	)

	defer func() {
		t.Logf("Cleaning up SparkConnect %s", sparkConnectName)
		tc.DeleteResource(
			WithMinimalObject(gvk.SparkConnect, types.NamespacedName{Name: sparkConnectName, Namespace: namespace}),
			WithIgnoreNotFound(true),
			WithWaitForDeletion(true),
		)
	}()

	t.Logf("Waiting for SparkConnect %s to reach Ready state", sparkConnectName)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.SparkConnect, types.NamespacedName{Name: sparkConnectName, Namespace: namespace}),
		WithCondition(
			jq.Match(`.status.state == "Ready"`),
		),
		WithCustomErrorMsg("SparkConnect %s did not reach Ready state", sparkConnectName),
		WithEventuallyTimeout(tc.TestTimeouts.defaultEventuallyTimeout),
		WithEventuallyPollingInterval(tc.TestTimeouts.defaultEventuallyPollInterval),
	)

	t.Logf("Creating PySpark client pod %s", clientPodName)
	clientPod := createPySparkClientPod(clientPodName, namespace)

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(clientPod),
		WithCustomErrorMsg("Failed to create PySpark client pod %s", clientPodName),
	)

	defer func() {
		t.Logf("Cleaning up PySpark client pod %s", clientPodName)
		tc.DeleteResource(
			WithMinimalObject(gvk.Pod, types.NamespacedName{Name: clientPodName, Namespace: namespace}),
			WithIgnoreNotFound(true),
			WithWaitForDeletion(true),
		)
	}()

	t.Logf("Waiting for PySpark client pod %s to be Running", clientPodName)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Pod, types.NamespacedName{Name: clientPodName, Namespace: namespace}),
		WithCondition(
			jq.Match(`.status.phase == "Running"`),
		),
		WithCustomErrorMsg("PySpark client pod %s is not Running", clientPodName),
		WithEventuallyTimeout(tc.TestTimeouts.defaultEventuallyTimeout),
		WithEventuallyPollingInterval(tc.TestTimeouts.defaultEventuallyPollInterval),
	)

	t.Logf("Installing Python dependencies in client pod %s", clientPodName)
	pipCmd := "pip install --quiet --disable-pip-version-check 'pyspark[connect]' pandas pyarrow grpcio grpcio-status zstandard"
	_, pipStderr, err := execInPod(namespace, clientPodName, containerName, []string{"sh", "-c", pipCmd})
	require.NoError(t, err, "pip install failed in pod %s: stderr=%s", clientPodName, pipStderr)

	connectURL := fmt.Sprintf("sc://%s-server.%s.svc.cluster.local:15002", sparkConnectName, namespace)
	pyScript := fmt.Sprintf(`
from pyspark.sql import SparkSession
spark = SparkSession.builder.remote("%s").getOrCreate()
df = spark.range(100).selectExpr("id", "id * 2 as doubled")
print("ROW_COUNT=" + str(df.count()))
spark.stop()
`, connectURL)

	t.Logf("Executing PySpark query from client pod %s against %s", clientPodName, connectURL)
	stdout, stderr, err := execInPod(namespace, clientPodName, containerName, []string{"python3", "-c", pyScript})
	require.NoError(t, err, "PySpark query failed in pod %s: stderr=%s", clientPodName, stderr)
	require.Contains(t, stdout, "ROW_COUNT=100", "Expected ROW_COUNT=100 in output, got: %s", stdout)

	t.Logf("SparkConnect %s successfully executed PySpark query from client pod (ROW_COUNT=100)", sparkConnectName)
}

// execInPod executes a command in a container and returns stdout, stderr, and any error.
func execInPod(namespace, podName, containerName string, command []string) (string, string, error) {
	config, err := ctrlcfg.GetConfig()
	if err != nil {
		return "", "", fmt.Errorf("failed to get Kubernetes config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return "", "", fmt.Errorf("failed to create clientset: %w", err)
	}

	req := clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: containerName,
			Command:   command,
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		return "", "", fmt.Errorf("failed to create SPDY executor: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	var stdout, stderr bytes.Buffer
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})

	return stdout.String(), stderr.String(), err
}

func (tc *SparkOperatorTestCtx) createSparkConnectInstance(name, namespace string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "sparkoperator.k8s.io/v1alpha1",
			"kind":       "SparkConnect",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]any{
				"sparkVersion": sparkVersion,
				"sparkConf": map[string]any{
					"spark.driver.blockManager.port": "7079",
				},
				"server": map[string]any{
					"template": map[string]any{
						"spec": map[string]any{
							"serviceAccount": "spark-operator-spark",
							"containers": []any{
								map[string]any{
									"name":  "spark-kubernetes-driver",
									"image": sparkImage,
									"resources": map[string]any{
										"requests": map[string]any{
											"cpu":    "1",
											"memory": "512Mi",
										},
										"limits": map[string]any{
											"cpu":    "1",
											"memory": "512Mi",
										},
									},
								},
							},
						},
					},
				},
				"executor": map[string]any{
					"instances": int64(1),
					"cores":     int64(1),
					"memory":    "512m",
				},
			},
		},
	}
}

func createPySparkClientPod(name, namespace string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]any{
				"containers": []any{
					map[string]any{
						"name":    "pyspark-client",
						"image":   pysparkClientImage,
						"command": []any{"sleep", "infinity"},
					},
				},
			},
		},
	}
}
