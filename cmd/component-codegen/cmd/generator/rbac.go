package generator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
)

func addKubeBuilderRBAC(logger *logrus.Logger, componentName string) error {
	lc := strings.ToLower(componentName)
	comments := []string{
		componentName,
		fmt.Sprintf("+kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=%ss,verbs=get;list;watch;create;update;patch;delete", lc),
		fmt.Sprintf("+kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=%ss/status,verbs=get;update;patch", lc),
		fmt.Sprintf("+kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=%ss/finalizers,verbs=update", lc),
	}

	fp := filepath.Join("internal/controller/datasciencecluster/kubebuilder_rbac.go")
	file, err := os.OpenFile(fp, os.O_APPEND|os.O_WRONLY, FilePerm)
	if err != nil {
		return fmt.Errorf("error opening file: %w", err)
	}
	defer file.Close()

	commentText := "\n"
	for _, comment := range comments {
		commentText += fmt.Sprintf("// %s\n", comment)
	}
	if _, err := file.WriteString(commentText); err != nil {
		return fmt.Errorf("error writing to file: %w", err)
	}

	logger.Infof("Successfully added RBAC markers to %s", fp)
	return nil
}
