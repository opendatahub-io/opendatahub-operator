package main

import (
	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/cloudmanager/app"
	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/cloudmanager/azure"
	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/cloudmanager/coreweave"
	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/cloudmanager/eks"
)

func main() {
	app.AddCommand(azure.NewCmd())
	app.AddCommand(coreweave.NewCmd())
	app.AddCommand(eks.NewCmd())
	app.Execute()
}
