package actions

import (
	fwactions "github.com/opendatahub-io/operator-actions-framework/controller/actions"
)

const ActionGroup = fwactions.ActionGroup

type Fn = fwactions.Fn

type Getter[T any] = fwactions.Getter[T]
