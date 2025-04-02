package generator

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/sirupsen/logrus"
)

func updateProjectFile(logger *logrus.Logger, componentName string) error {
	newResourceEntry := fmt.Sprintf(`- api:
    crdVersion: v1alpha1
  controller: true
  domain: platform.opendatahub.io
  group: components
  kind: %s
  path: github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1
  version: v1alpha1`, componentName)

	data, err := os.ReadFile(projectFilePath)
	if err != nil {
		logger.Errorf("Failed to read file: %v", err)
		return err
	}
	content := string(data)

	re := regexp.MustCompile(`(?m)^- api:\n(?:\s+.*\n)*?\s+group: components(?:\n\s+.*)*`)
	matches := re.FindAllStringIndex(content, -1)

	insertPos := matches[len(matches)-1][1]

	var builder strings.Builder
	builder.WriteString(content[:insertPos])
	builder.WriteString("\n")
	builder.WriteString(newResourceEntry)
	builder.WriteString(content[insertPos:])

	err = os.WriteFile(projectFilePath, []byte(builder.String()), 0o644)
	if err != nil {
		logger.Errorf("Failed to write file: %v", err)
		return err
	}

	logger.Info("New component added to PROJECT file successfully!")
	return nil
}
