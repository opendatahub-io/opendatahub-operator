package codeflare

type UpgradeBlockedError struct {
	CodeFlareCRPresent bool
}

func (e *UpgradeBlockedError) Error() string {
	return "CodeFlare internal CR present"
}
