package generator

import (
	"os"
	"os/exec"

	"github.com/sirupsen/logrus"
)

// runMakeCommand executes necessary Makefile commands for updating generated code and manifests.
func RunMakeCommand(logger *logrus.Logger) error {
	logger.Info("Running make command...")
	cmd := exec.Command("make", "generate", "manifests", "api-docs", "bundle", "build")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		logger.Errorf("Make command failed: %v", err)
		return err
	}
	logger.Info("Make command completed successfully.")
	return nil
}
