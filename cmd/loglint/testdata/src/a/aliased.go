package a

import f "fmt"

// TestAliased exercises fmt imported under an alias: the analyzer must resolve
// the callee through type information, not by matching the identifier "fmt".
func TestAliased() {
	log := &Logger{}
	var err error
	var ns string

	log.Error(err, f.Sprintf("failed in namespace %s", ns)) // want `message embeds resource name or namespace`
	log.Error(err, f.Sprintf("reconcile of %s failed", ns))
}
