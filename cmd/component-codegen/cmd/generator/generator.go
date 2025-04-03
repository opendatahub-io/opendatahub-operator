package generator

import (
	"path/filepath"

	"github.com/sirupsen/logrus"
)

const (
	cmdDir          = "cmd/component-codegen"
	ApisDir         = "api/components/v1alpha1"
	Controllers     = "internal/controller/components"
	DscTypesPath    = "api/datasciencecluster/v1/datasciencecluster_types.go"
	templatesDir    = cmdDir + "/templates"
	mainFilePath    = "cmd/main.go"
	projectFilePath = "PROJECT"
)

type PathConfig struct {
	Suffix       string
	OutputPath   string
	TemplatePath string
}

func GenerateComponent(logger *logrus.Logger, componentName string) error {
	paths := []PathConfig{
		{"_types.go", filepath.Join(ApisDir), filepath.Join(templatesDir, "types.go.tmpl")},
		{".go", filepath.Join(Controllers, componentName), filepath.Join(templatesDir, "component_handler.go.tmpl")},
		{"_support.go", filepath.Join(Controllers, componentName), filepath.Join(templatesDir, "component_support.go.tmpl")},
		{"_controller_actions.go", filepath.Join(Controllers, componentName), filepath.Join(templatesDir, "component_controller_actions.go.tmpl")},
		{"_controller.go", filepath.Join(Controllers, componentName), filepath.Join(templatesDir, "component_controller.go.tmpl")},
	}

	for _, p := range paths {
		if err := generateFilesFromTemplate(logger, componentName, p); err != nil {
			return err
		}
	}

	dirs := []string{DscTypesPath, mainFilePath}
	for _, dir := range dirs {
		if err := addFieldsToStruct(logger, componentName, dir); err != nil {
			return err
		}
	}

	if err := addKubeBuilderRBAC(logger, componentName); err != nil {
		return err
	}

	return updateProjectFile(logger, componentName)
}
