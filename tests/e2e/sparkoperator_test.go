package e2e_test

import (
	"fmt"
	"testing"

	"github.com/rs/xid"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
)

const (
	sparkVersion = "4.0.1"
	sparkImage   = "quay.io/opendatahub/data-processing:Spark-v" + sparkVersion
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

	// Use a unique name to avoid conflicts with previous test runs
	scheduledSparkAppName := "scheduled-spark-pi-" + xid.New().String()
	// Run in the applications namespace where spark-operator-spark SA exists
	namespace := tc.AppsNamespace

	t.Logf("Creating ScheduledSparkApplication %s in namespace %s", scheduledSparkAppName, namespace)
	scheduledSparkApp := tc.createScheduledSparkPiApplication(scheduledSparkAppName, namespace)

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(scheduledSparkApp),
		WithCustomErrorMsg("Failed to create ScheduledSparkApplication %s", scheduledSparkAppName),
	)

	var lastRunName string

	defer func() {
		if lastRunName != "" {
			t.Logf("Cleaning up SparkApplication %s", lastRunName)
			tc.DeleteResource(
				WithMinimalObject(gvk.SparkApplication, types.NamespacedName{Name: lastRunName, Namespace: namespace}),
				WithIgnoreNotFound(true),
				WithWaitForDeletion(true),
			)
		}

		t.Logf("Cleaning up ScheduledSparkApplication %s", scheduledSparkAppName)
		tc.DeleteResource(
			WithMinimalObject(gvk.ScheduledSparkApplication, types.NamespacedName{Name: scheduledSparkAppName, Namespace: namespace}),
			WithIgnoreNotFound(true),
			WithWaitForDeletion(true),
		)
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

	var found bool
	var err error

	lastRunName, found, err = unstructured.NestedString(scheduledApp.Object, "status", "lastRunName")
	require.NoError(t, err, "Failed to extract lastRunName from ScheduledSparkApplication status")
	require.True(t, found, "lastRunName not found in ScheduledSparkApplication status")

	t.Logf("ScheduledSparkApplication %s created SparkApplication: %s", scheduledSparkAppName, lastRunName)

	// Wait for the spawned SparkApplication to reach a terminal state
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

	var state string
	state, found, err = unstructured.NestedString(completedApp.Object, "status", "applicationState", "state")
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
