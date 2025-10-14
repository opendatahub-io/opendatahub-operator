package main

import (
	"fmt"
	"os"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/test-retry/pkg/cli"
)

func main() {
	rootCmd := cli.NewRootCommand()
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
