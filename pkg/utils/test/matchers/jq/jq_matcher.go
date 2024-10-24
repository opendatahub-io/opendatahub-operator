package jq

import (
	"fmt"

	"github.com/itchyny/gojq"
	"github.com/onsi/gomega/format"
	"github.com/onsi/gomega/types"
)

func Match(format string, args ...any) *Matcher {
	return &Matcher{
		expression: fmt.Sprintf(format, args...),
	}
}

var _ types.GomegaMatcher = &Matcher{}

type Matcher struct {
	expression       string
	firstFailurePath []interface{}
}

func (matcher *Matcher) Match(actual interface{}) (bool, error) {
	query, err := gojq.Parse(matcher.expression)
	if err != nil {
		return false, fmt.Errorf("unable to parse expression %s, %w", matcher.expression, err)
	}

	data, err := toType(actual)
	if err != nil {
		return false, err
	}

	it := query.Run(data)

	v, ok := it.Next()
	if !ok {
		return false, nil
	}

	if err, ok := v.(error); ok {
		return false, err
	}

	if match, ok := v.(bool); ok {
		return match, nil
	}

	return false, nil
}

func (matcher *Matcher) FailureMessage(actual interface{}) string {
	return formattedMessage(format.Message(fmt.Sprintf("%v", actual), "to match expression", matcher.expression), matcher.firstFailurePath)
}

func (matcher *Matcher) NegatedFailureMessage(actual interface{}) string {
	return formattedMessage(format.Message(fmt.Sprintf("%v", actual), "not to match expression", matcher.expression), matcher.firstFailurePath)
}
