package v1alpha1

// CORSConfiguration describes the cross-origin access configuration for a bucket.
type CORSConfiguration struct {
	// A set of origins and methods (cross-origin access that you want to allow).
	// You can add up to 100 rules to the configuration.
	Rules []CORSRule `json:"rules"`
}

// CORSRule specifies a cross-origin access rule for a bucket.
type CORSRule struct {
	// Headers that are specified in the Access-Control-Request-Headers header.
	// These headers are allowed in a preflight OPTIONS request. In response to
	// any preflight OPTIONS request, Amazon S3 returns any requested headers that
	// are allowed.
	// +optional
	AllowedHeaders []string `json:"allowedHeaders,omitempty"`

	// An HTTP method that you allow the origin to execute. Valid values are GET,
	// PUT, HEAD, POST, and DELETE.
	AllowedMethods []string `json:"allowedMethods"`

	// One or more origins you want customers to be able to access the bucket from.
	AllowedOrigins []string `json:"allowedOrigins"`

	// One or more headers in the response that you want customers to be able to
	// access from their applications (for example, from a JavaScript XMLHttpRequest
	// object).
	// +optional
	ExposeHeaders []string `json:"exposeHeaders,omitempty"`

	// Unique identifier for the rule. The value cannot be longer than 255 characters.
	// +optional
	ID *string `json:"id,omitempty"`

	// The time in seconds that your browser is to cache the preflight response
	// for the specified resource.
	// +optional
	MaxAgeSeconds *int32 `json:"maxAgeSeconds,omitempty"`
}
