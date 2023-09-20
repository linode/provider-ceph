package bucket

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMissingProviders(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		providers          []string
		providerConfigList *apisv1alpha1.ProviderConfigList
		missingProviders   []string
	}{
		"All spec.Providers found": {
			providers: []string{
				"cluster-1",
				"cluster-2",
			},
			providerConfigList: &apisv1alpha1.ProviderConfigList{
				Items: []apisv1alpha1.ProviderConfig{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "cluster-1",
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "cluster-2",
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "cluster-3",
						},
					},
				},
			},
			missingProviders: []string{},
		},
		"No spec.Providers found": {
			providers: []string{
				"cluster-1",
				"cluster-2",
			},
			providerConfigList: &apisv1alpha1.ProviderConfigList{
				Items: []apisv1alpha1.ProviderConfig{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "cluster-3",
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "cluster-4",
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "cluster-5",
						},
					},
				},
			},
			missingProviders: []string{
				"cluster-1",
				"cluster-2",
			},
		},
		"No ProviderConfigs exist": {
			providers: []string{
				"cluster-1",
				"cluster-2",
			},
			providerConfigList: &apisv1alpha1.ProviderConfigList{},
			missingProviders: []string{
				"cluster-1",
				"cluster-2",
			},
		},
		"Some spec.Providers found": {
			providers: []string{
				"cluster-1",
				"cluster-2",
				"cluster-3",
			},
			providerConfigList: &apisv1alpha1.ProviderConfigList{
				Items: []apisv1alpha1.ProviderConfig{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "cluster-1",
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "cluster-2",
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "cluster-5",
						},
					},
				},
			},
			missingProviders: []string{
				"cluster-3",
			},
		},
	}
	for name, tc := range cases {
		tc := tc
		n := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			missingProviders := missingProviders(tc.providers, tc.providerConfigList)
			if diff := cmp.Diff(tc.missingProviders, missingProviders); diff != "" {
				t.Errorf("\n%s\nmissingProviders(...): -want, +got:\n%s\n", n, diff)
			}
		})
	}
}
