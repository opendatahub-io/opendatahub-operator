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
	SuffixVersion            = "/version"
	SuffixType               = "/type"
	SuffixInstanceGeneration = "/instance.generation"
	SuffixInstanceName       = "/instance.name"
	SuffixInstanceUID        = "/instance.uid"
)

const (
	PlatformVersion    = "platform.opendatahub.io" + SuffixVersion
	PlatformType       = "platform.opendatahub.io" + SuffixType
	InstanceGeneration = "platform.opendatahub.io" + SuffixInstanceGeneration
	InstanceName       = "platform.opendatahub.io" + SuffixInstanceName
	InstanceUID        = "platform.opendatahub.io" + SuffixInstanceUID
)

// PSAElevatedBy records which component elevated the namespace PSA level to "privileged".
// DSCI preserves the elevated level only while this annotation is present.
const PSAElevatedBy = "opendatahub.io/psa-elevated-by"

// Connection annotation for referencing secrets containing connection information.
const Connection = "opendatahub.io/connections"

// ConnectionTypeRef annotation for specifying the type of connection.
//
// Deprecated: Use ConnectionTypeProtocol instead.
const ConnectionTypeRef = "opendatahub.io/connection-type-ref"

// ConnectionTypeProtocol annotation for specifying the type of connection.
const ConnectionTypeProtocol = "opendatahub.io/connection-type-protocol"

// ConnectionPath annotation for specifying the path under bucket(s3) to use for the connection.
// TODO: extend to oci.
const ConnectionPath = "opendatahub.io/connection-path"
