package v1alpha1

type ObjectLockEnabled string

const (
	ObjectLockEnabledEnabled ObjectLockEnabled = "Enabled"
)

type DefaultRetentionMode string

const (
	ModeGovernance DefaultRetentionMode = "GOVERNANCE"
	ModeCompliance DefaultRetentionMode = "COMPLIANCE"
)

// ObjectLockConfiguration describes the object lock state of an S3 bucket.
type ObjectLockConfiguration struct {
	// +optional.
	// Indicates whether this bucket has an Object Lock configuration enabled. Enable
	// ObjectLockEnabled when you apply ObjectLockConfiguration to a bucket.
	// +kubebuilder:validation:Enum=Enabled
	ObjectLockEnabled *ObjectLockEnabled `json:"objectLockEnabled,omitempty"`
	// +optional.
	// Specifies the Object Lock rule for the specified object. Enable this rule
	// when you apply ObjectLockConfiguration to a bucket. Bucket settings require
	// both a mode and a period. The period can be either Days or Years but you must
	// select one. You cannot specify Days and Years at the same time.
	Rule *ObjectLockRule `json:"objectLockRule,omitempty"`
}

type ObjectLockRule struct {
	// +optional.
	// The default Object Lock retention mode and period that you want to apply to new
	// objects placed in the specified bucket. Bucket settings require both a mode and
	// a period. The period can be either Days or Years but you must select one. You
	// cannot specify Days and Years at the same time.
	DefaultRetention *DefaultRetention `json:"defaultRetention,omitempty"`
}

type DefaultRetention struct {
	// +optional.
	// The number of days that you want to specify for the default retention period.
	// Must be used with Mode.
	Days *int32 `json:"days,omitempty"`
	// The default Object Lock retention mode you want to apply to new objects placed
	// in the specified bucket. Must be used with either Days or Years.
	// +kubebuilder:validation:Enum=GOVERNANCE;COMPLIANCE
	Mode DefaultRetentionMode `json:"mode,omitempty"`
	// +optional.
	// The number of years that you want to specify for the default retention period.
	// Must be used with Mode.
	Years *int32 `json:"years,omitempty"`
}
