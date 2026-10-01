package a

import "fmt"

type Logger struct{}

func (l *Logger) Error(err error, msg string, keysAndValues ...interface{}) {}

// notALogger has an Error method whose signature does not match the logr shape
// Error(error, string, ...any); its calls must never be flagged even when the
// arguments look like an embedded identity.
type notALogger struct{}

func (n *notALogger) Error(code int, msg string) {}

const nsMsg = "failed in namespace foo"

const nameKey = "Request.Name"

func Test() {
	log := &Logger{}
	var err error
	var ns string

	log.Error(err, "reconciliation failed")
	log.Error(err, "reconciliation failed", "name", "foo", "namespace", "bar", "resourceKind", "Dashboard")

	log.Error(err, "failed in namespace foo") // want `message embeds resource name or namespace`
	log.Error(err, "Request.Name is missing") // want `message embeds resource name or namespace`

	log.Error(err, nsMsg)                    // want `message embeds resource name or namespace`
	log.Error(err, "failed "+"in namespace") // want `message embeds resource name or namespace`

	log.Error(err, fmt.Sprintf("failed in namespace %s", ns)) // want `message embeds resource name or namespace`
	log.Error(err, fmt.Sprintf("reconcile of %s failed", ns))

	log.Error(err, "failed in namespace "+ns)

	log.Error(err, "failed", "Request.Name", "foo") // want `non-standard structured log key "Request.Name"; use "name"`
	log.Error(err, "failed", "ns", "foo")           // want `non-standard structured log key "ns"; use "namespace"`

	log.Error(err, "failed", "name", "foo", "namespace", "bar") // want `missing required structured field "resourceKind"`

	log.Error(err, "failed", "name", "foo") //nolint:odhlog

	log.Error(err, "failed", nameKey, "foo") // want `non-standard structured log key "Request.Name"; use "name"`

	log.Error(
		err,
		"failed in namespace foo", //nolint:odhlog
	)

	log.Error( //nolint:odhlog
		err,
		"failed in namespace foo", // want `message embeds resource name or namespace`
	)

	// Not a logr-style logger: the signature does not match Error(error,
	// string, ...any), so this is left unflagged despite the identity text.
	other := &notALogger{}
	other.Error(1, "failed in namespace foo")

	// nolint beside the offending key suppresses the key diagnostic.
	log.Error(err, "failed",
		"Request.Name", "foo", //nolint:odhlog
	)

	// nolint on the opening line does not suppress a diagnostic anchored at the
	// key line below it.
	log.Error(err, "failed", //nolint:odhlog
		"Request.Name", "foo", // want `non-standard structured log key "Request.Name"; use "name"`
	)

	// missing resourceKind is reported at the name key's position.
	log.Error(err, "failed",
		"name", "foo", // want `missing required structured field "resourceKind"`
	)

	// Expanded namespace / name detection.
	log.Error(err, fmt.Sprintf("failed for namespace %s", ns))  // want `message embeds resource name or namespace`
	log.Error(err, fmt.Sprintf("reconcile name=%s failed", ns)) // want `message embeds resource name or namespace`
	log.Error(err, fmt.Sprintf("pod named %s not found", ns))   // want `message embeds resource name or namespace`
	log.Error(err, "Request.Namespace lookup failed")           // want `message embeds resource name or namespace`
	// Prose that merely contains the words must not be flagged.
	log.Error(err, "namespace is required")
	log.Error(err, "invalid name format in spec")

	// Exact nolint directive matching: a different linter name, even one that
	// has odhlog as a prefix, must not suppress; a comma-separated list or a
	// bare //nolint must.
	log.Error(err, "failed in namespace foo") //nolint:odhlogfoo // want `message embeds resource name or namespace`
	log.Error(err, "failed in namespace foo") //nolint:gosec,odhlog
	log.Error(err, "failed in namespace foo") //nolint
	log.Error(err, "failed in namespace foo") // prose mentioning nolint:odhlog // want `message embeds resource name or namespace`
}
