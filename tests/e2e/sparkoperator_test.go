package e2e_test

import (
	"testing"

	"github.com/rs/xid"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
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
		WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
		WithEventuallyPollingInterval(tc.TestTimeouts.defaultEventuallyPollInterval),
	)

	t.Logf("SparkApplication %s completed successfully", sparkAppName)
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
			"spec": map[string]any{
				"type":                "Scala",
				"mode":                "cluster",
				"image":               "quay.io/opendatahub/data-processing:Spark-v4.0.1",
				"imagePullPolicy":     "IfNotPresent",
				"mainClass":           "org.apache.spark.examples.SparkPi",
				"mainApplicationFile": "local:///opt/spark/examples/jars/spark-examples_2.13-4.0.1.jar",
				"arguments":           []any{"1000"},
				"sparkVersion":        "4.0.1",
				"restartPolicy": map[string]any{
					"type": "Never",
				},
				"sparkConf": map[string]any{
					"spark.driver.port":              "8080",
					"spark.driver.blockManager.port": "8082",
					"spark.blockManager.port":        "8081",
					"spark.port.maxRetries":          "0",
				},
				// Volume for OpenShift compatibility - provides writable work directory
				"volumes": []any{
					map[string]any{
						"name":     "spark-work-dir",
						"emptyDir": map[string]any{},
					},
				},
				"driver": map[string]any{
					"labels": map[string]any{
						"version": "4.0.1",
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
						"version": "4.0.1",
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
			},
		},
	}
}
