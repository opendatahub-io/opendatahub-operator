package a

type Logger struct{}

func (l *Logger) Error(err error, msg string, keysAndValues ...interface{}) {}

func Test() {
	log := &Logger{}
	var err error

	log.Error(err, "reconciliation failed")
	log.Error(err, "reconciliation failed", "name", "foo", "namespace", "bar", "resourceKind", "Dashboard")

	log.Error(err, "failed in namespace foo") // want `message embeds resource name or namespace`
	log.Error(err, "Request.Name is missing") // want `message embeds resource name or namespace`

	log.Error(err, "failed", "Request.Name", "foo") // want `non-standard structured log key "Request.Name"; use "name"`
	log.Error(err, "failed", "ns", "foo") // want `non-standard structured log key "ns"; use "namespace"`

	log.Error(err, "failed", "name", "foo", "namespace", "bar") // want `missing required structured field "resourceKind"`

	log.Error(err, "failed", "name", "foo") //nolint:odhlog
}
