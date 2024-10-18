package envtestutil

import (
	"fmt"

	utilrand "k8s.io/apimachinery/pkg/util/rand"
)

const (
	maxNameLength          = 63
	randomLength           = 5
	maxGeneratedNameLength = maxNameLength - randomLength
)

func AppendRandomNameTo(base string) string {
	if len(base) > maxGeneratedNameLength {
		base = base[:maxGeneratedNameLength]
	}
	return fmt.Sprintf("%s-%s", base, utilrand.String(randomLength))
}
