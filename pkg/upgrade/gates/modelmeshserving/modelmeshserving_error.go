package modelmeshserving

type UpgradeBlockedError struct{}

func (e *UpgradeBlockedError) Error() string {
	return "ModelMeshServing internal CR present"
}
