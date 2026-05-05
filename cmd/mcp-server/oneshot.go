package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

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
