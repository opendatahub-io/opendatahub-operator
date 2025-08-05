package components

import (
	"embed"
)

//go:embed kueue/monitoring
//go:embed trainingoperator/monitoring
//go:embed trustyai/monitoring
//go:embed workbenches/monitoring
//go:embed codeflare/monitoring
//go:embed dashboard/monitoring
//go:embed datasciencepipelines/monitoring
//go:embed feastoperator/monitoring
//go:embed kserve/monitoring
//go:embed llamastackoperator/monitoring
//go:embed modelcontroller/monitoring
//go:embed modelmeshserving/monitoring
//go:embed modelregistry/monitoring
//go:embed ray/monitoring
var ComponentRulesFS embed.FS
