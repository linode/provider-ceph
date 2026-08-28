package v1alpha1

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
)

const bucketCRDPath = "../../../package/crds/provider-ceph.ceph.crossplane.io_buckets.yaml"

// requiredPaths reports every object below path that declares required fields.
func requiredPaths(path string, schema apiextv1.JSONSchemaProps) []string {
	paths := []string{}
	if len(schema.Required) > 0 {
		paths = append(paths, path)
	}

	for name, prop := range schema.Properties {
		paths = append(paths, requiredPaths(path+"."+name, prop)...)
	}

	if schema.AdditionalProperties != nil && schema.AdditionalProperties.Schema != nil {
		paths = append(paths, requiredPaths(path+"[*]", *schema.AdditionalProperties.Schema)...)
	}

	return paths
}

// The Status is written with a merge patch. A required field that nothing ever
// sets is never written, so the API server rejects every status write.
func TestBucketStatusAtProviderIsMergePatchSafe(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Clean(bucketCRDPath))
	require.NoError(t, err, "cannot read generated Bucket CRD - run `make generate`")

	crd := &apiextv1.CustomResourceDefinition{}
	require.NoError(t, yaml.Unmarshal(raw, crd), "cannot parse generated Bucket CRD")

	var status apiextv1.JSONSchemaProps
	for _, v := range crd.Spec.Versions {
		if v.Storage {
			require.NotNil(t, v.Schema, "storage version %s declares no schema", v.Name)
			status = v.Schema.OpenAPIV3Schema.Properties["status"]
		}
	}

	atProvider, ok := status.Properties["atProvider"]
	require.True(t, ok, "CRD has no status.atProvider schema")

	assert.NotContains(t, status.Required, "atProvider",
		"status.atProvider must stay optional so conditions-only patches are accepted")

	// Backends entries are written whole, so required fields there are fine.
	violations := requiredPaths("status.atProvider", apiextv1.JSONSchemaProps{Required: atProvider.Required})
	for name, prop := range atProvider.Properties {
		if name == "backends" {
			continue
		}

		violations = append(violations, requiredPaths("status.atProvider."+name, prop)...)
	}

	assert.Empty(t, violations, "no field under status.atProvider may be required")
}
