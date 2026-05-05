package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/server"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client/config"

	"github.com/opendatahub-io/opendatahub-operator/pkg/clusterhealth"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/failureclassifier"
)

// DiagnoseReport combines the cluster health report with failure classification
// for CI artifact attachment.
type DiagnoseReport struct {
	Report         *clusterhealth.Report                    `json:"report"`
	Classification *failureclassifier.FailureClassification `json:"classification"`
	TestName       string                                   `json:"testName"`
	Error          string                                   `json:"error,omitempty"`
}

var redactPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(Authorization:\s*Bearer\s+)[^\s]+`),
	regexp.MustCompile(`(?i)(password[=:\s]+)[^\s&"']+`),
	regexp.MustCompile(`(?i)(token[=:\s]+)[^\s&"']+`),
	regexp.MustCompile(`(?i)(secret[=:\s]+)[^\s&"']+`),
	regexp.MustCompile(`(?i)(api[_-]?key[=:\s]+)[^\s&"']+`),
	regexp.MustCompile(`(?i)(access[_-]?key[=:\s]+)[^\s&"']+`),
}

func redactString(s string) string {
	for _, p := range redactPatterns {
		s = p.ReplaceAllString(s, "${1}[REDACTED]")
	}
	return s
}

func redactEvidence(fc *failureclassifier.FailureClassification) {
	for i, e := range fc.Evidence {
		fc.Evidence[i] = redactString(e)
	}
}

func main() {
	log.SetOutput(os.Stderr)

	oneShot := flag.Bool("one-shot", false, "Run diagnostics once (health check + failure classification), output JSON, and exit.")
	testName := flag.String("test-name", "ci-diagnose", "Test name for failure classification (used with --one-shot)")
	flag.Parse()

	kubeConfig, err := ctrl.GetConfig()
	if err != nil {
		log.Fatalf("mcp-server: kubeconfig: %v", err)
	}

	kubeClient, err := client.New(kubeConfig, client.Options{
		Scheme: clientgoscheme.Scheme,
	})
	if err != nil {
		log.Fatalf("mcp-server: kube client: %v", err)
	}

	if *oneShot {
		os.Exit(runOneShot(kubeClient, *testName))
	}

	clientsetCfg := rest.CopyConfig(kubeConfig)
	if clientsetCfg.Timeout == 0 {
		clientsetCfg.Timeout = 30 * time.Second
	}
	clientset, err := kubernetes.NewForConfig(clientsetCfg)
	if err != nil {
		log.Fatalf("mcp-server: clientset: %v", err)
	}

	s := server.NewMCPServer("opendatahub-health", "0.1.0")

	registerPlatformHealth(s, kubeClient)
	registerOperatorDependencies(s, kubeClient)
	registerDescribeResource(s, kubeClient)
	registerRecentEvents(s, kubeClient)
	registerClassifyFailure(s, kubeClient)
	registerComponentStatus(s, kubeClient)
	registerPodLogs(s, clientset)

	if err := server.ServeStdio(s); err != nil {
		log.Fatalf("mcp-server: serve: %v", err)
	}
}

func runOneShot(kubeClient client.Client, testName string) int {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cfg := clusterhealth.Config{
		Client: kubeClient,
		Operator: clusterhealth.OperatorConfig{
			Namespace: getEnvDefault(envOperatorNamespace, defaultOperatorNS),
			Name:      getEnvDefault(envOperatorDeployment, defaultOperatorDeploy),
		},
		Namespaces: clusterhealth.NamespaceConfig{
			Apps:       getEnvDefault(envApplicationsNamespace, defaultAppsNS),
			Monitoring: getEnvDefault(envMonitoringNamespace, defaultMonitoringNS),
			Extra:      []string{"kube-system"},
		},
		DSCI: types.NamespacedName{Name: "default-dsci"},
		DSC:  types.NamespacedName{Name: "default-dsc"},
	}

	report, err := clusterhealth.Run(ctx, cfg)

	var fc failureclassifier.FailureClassification
	var diagErr string

	if err != nil {
		log.Printf("ERROR: Failed to collect diagnostics: %v", redactString(err.Error()))
		fc = failureclassifier.Classify(nil)
		fc.Evidence = append(fc.Evidence, fmt.Sprintf("clusterhealth.Run error: %v", err))
		diagErr = redactString(err.Error())
	} else {
		logReportToStderr(report)
		fc = failureclassifier.Classify(report)
	}

	redactEvidence(&fc)
	emitJSON(DiagnoseReport{
		Report:         report,
		Classification: &fc,
		TestName:       testName,
		Error:          diagErr,
	})

	log.Printf("Classification for %s: %s/%s (code=%d, confidence=%s)",
		testName, fc.Category, fc.Subcategory, fc.ErrorCode, fc.Confidence)
	if len(fc.Evidence) > 0 {
		log.Printf("  Evidence: %s", strings.Join(fc.Evidence, "; "))
	}

	if err != nil || !report.Healthy() {
		return 1
	}
	return 0
}

func emitJSON(dr DiagnoseReport) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(dr); err != nil {
		log.Fatalf("mcp-server: json: %v", err)
	}
}

func logReportToStderr(report *clusterhealth.Report) {
	log.Print(redactString(report.PrettyPrint(true)))
}
