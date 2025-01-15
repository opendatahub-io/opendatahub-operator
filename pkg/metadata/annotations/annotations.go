package annotations

// ManagedByODHOperator is used to denote if a resource/component should be reconciled - when true, reconcile.
const ManagedByODHOperator = "opendatahub.io/managed"

// trust CA bundler.
const InjectionOfCABundleAnnotatoion = "security.opendatahub.io/inject-trusted-ca-bundle"

// secret generator.
const (
	SecretNameAnnotation        = "secret-generator.opendatahub.io/name"
	SecretTypeAnnotation        = "secret-generator.opendatahub.io/type"
	SecretLengthAnnotation      = "secret-generator.opendatahub.io/complexity"
	SecretOauthClientAnnotation = "secret-generator.opendatahub.io/oauth-client-route"
)

// ManagementStateAnnotation set on Component CR only, to show which ManagementState value if defined in DSC for the component.
const ManagementStateAnnotation = "component.opendatahub.io/management-state"

const (
	PlatformVersion    = "platform.opendatahub.io/version"
	PlatformType       = "platform.opendatahub.io/type"
	InstanceGeneration = "platform.opendatahub.io/instance.generation"
	InstanceName       = "platform.opendatahub.io/instance.name"
	InstanceUID        = "platform.opendatahub.io/instance.uid"
)
