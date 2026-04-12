package generator

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"github.com/sirupsen/logrus"
)

type TemplateData struct {
	Component string
}

func generateFilesFromTemplate(logger *logrus.Logger, componentName string, p PathConfig) error {
	validComponentName, err := regexp.MatchString(`^[A-Za-z][A-Za-z0-9]*$`, componentName)
	if err != nil {
		return fmt.Errorf("error validating component name: %w", err)
	}
	if !validComponentName {
		return fmt.Errorf("invalid component name: %s", componentName)
	}

	suffix := p.Suffix
	outputPath := strings.ToLower(p.OutputPath)
	templatePath := p.TemplatePath
	componentFileName := strings.ToLower(componentName) + suffix
	op := filepath.Join(outputPath, componentFileName)

	cleanOutputPath := filepath.Clean(outputPath)
	cleanOp := filepath.Clean(op)
	relPath, err := filepath.Rel(cleanOutputPath, cleanOp)
	if err != nil {
		return fmt.Errorf("error resolving output path: %w", err)
	}
	if relPath == ".." || strings.HasPrefix(relPath, ".."+string(filepath.Separator)) {
		return fmt.Errorf("invalid output path: %s", op)
	}

	if fileExists(op) {
		logger.Warnf("File already exists: %s", op)
		return nil
	}

	content, err := os.ReadFile(templatePath)
	if err != nil {
		return fmt.Errorf("error reading template: %w", err)
	}

	funcMap := template.FuncMap{"lowercase": strings.ToLower}
	tmpl, err := template.New("template").Funcs(funcMap).Parse(string(content))
	if err != nil {
		return fmt.Errorf("error parsing template: %w", err)
	}

	templateData := TemplateData{Component: componentName}

	var generatedContent strings.Builder
	if err := tmpl.Execute(&generatedContent, templateData); err != nil {
		return fmt.Errorf("error executing template: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(op), os.ModePerm); err != nil {
		return fmt.Errorf("error creating directory: %w", err)
	}
	if err := os.WriteFile(op, []byte(generatedContent.String()), FilePerm); err != nil {
		return fmt.Errorf("error writing file: %w", err)
	}

	logger.Infof("Generated %s in %s", componentFileName, op)
	return nil
}
