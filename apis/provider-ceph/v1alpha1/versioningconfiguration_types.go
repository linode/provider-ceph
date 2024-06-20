package v1alpha1

type VersioningStatus string

const (
	VersioningStatusEnabled   VersioningStatus = "Enabled"
	VersioningStatusSuspended VersioningStatus = "Suspended"
)

type MFADelete string

const (
	MFADeleteEnabled  MFADelete = "Enabled"
	MFADeleteDisabled MFADelete = "Disabled"
)

// VersioningConfiguration describes the versioning state of an S3 bucket.
type VersioningConfiguration struct {
	// MFADelete specifies whether MFA delete is enabled in the bucket versioning configuration.
	// This element is only returned if the bucket has been configured with MFA
	// delete. If the bucket has never been so configured, this element is not returned.
	// +kubebuilder:validation:Enum=Enabled;Disabled
	MFADelete *MFADelete `json:"mfaDelete,omitempty"`

	// Status is the desired versioning state of the bucket.
	// +kubebuilder:validation:Enum=Enabled;Suspended
	Status *VersioningStatus `json:"status,omitempty"`
}
