package main

import (
	"os"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/cli"
)

func main() {
	if err := cli.NewRootCommand().Execute(); err != nil {
		os.Exit(1)
	}
}
