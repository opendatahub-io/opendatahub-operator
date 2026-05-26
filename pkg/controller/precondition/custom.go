package precondition

// Custom creates a PreCondition from a caller-provided CheckFunc.
// Use this for component-specific precondition logic that does not fit
// the built-in types (MonitorCRD, MonitorOperator, etc.).
func Custom(check CheckFunc, opts ...Option) PreCondition {
	return newPreCondition(check, opts...)
}
