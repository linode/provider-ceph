package v1alpha1

type Type string

// Enum values for Type
const (
	TypeCanonicalUser Type = "CanonicalUser"
	TypeEmail         Type = "Email"
	TypeGroup         Type = "Group"
)

type Permission string

// Enum values for Permission
const (
	PermissionFullControl Permission = "FULL_CONTROL"
	PermissionWrite       Permission = "WRITE"
	PermissionWriteAcp    Permission = "WRITE_ACP"
	PermissionRead        Permission = "READ"
	PermissionReadAcp     Permission = "READ_ACP"
)

// Contains the elements that set the ACL permissions for an object per grantee.
type AccessControlPolicy struct {
	// A list of grants.
	// +optional
	Grants []Grant `json:"grants,omitempty"`
	// Container for the bucket owner's display name and ID.
	// +optional
	Owner *Owner `json:"owner,omitempty"`
}

// Container for grant information.
type Grant struct {
	// The person being granted permissions.
	// +optional
	Grantee *Grantee `json:"grantee,omitempty"`
	// Specifies the permission given to the grantee.
	// +optional
	// +kubebuilder:validation:Enum=FULL_CONTROL;WRITE;WRITE;WRITE_ACP;READ;READ_ACP
	Permission Permission `json:"permission,omitempty"`
}

// Container for the person being granted permissions.
type Grantee struct {
	// Type of grantee.
	// Type is a required field.
	// +kubebuilder:validation:Enum=CanonicalUser;Email;Group
	Type Type `json:"type"`
	// Screen name of the grantee.
	// +optional
	DisplayName *string `json:"displayName,omitempty"`
	// Email address of the grantee.
	// +optional
	EmailAddress *string `json:"emailAddress,omitempty"`
	// The canonical user ID of the grantee.
	// +optional
	ID *string `json:"id,omitempty"`
	// URI of the grantee group.
	// +optional
	URI *string `json:"uri,omitempty"`
}

// Container for the owner's display name and ID.
type Owner struct {
	// Container for the display name of the owner.
	// +optional
	DisplayName *string `json:"displayName,omitempty"`
	// Container for the ID of the owner.
	// +optional
	ID *string `json:"id,omitempty"`
}
