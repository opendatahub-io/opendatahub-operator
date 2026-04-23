package main

import (
	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/cloudmanager/app"
	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/cloudmanager/azure"
	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/cloudmanager/coreweave"
)

func main() {
	app.AddCommand(azure.NewCmd())
	app.AddCommand(coreweave.NewCmd())
	app.Execute()
}
