package yq

import (
	"github.com/mikefarah/yq/v4/pkg/yqlib"
	"gopkg.in/op/go-logging.v1"
)

//nolint:gochecknoinits
func init() {
	logging.SetBackend(&nullLogger{})
	yqlib.InitExpressionParser()
}
