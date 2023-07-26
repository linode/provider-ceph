package radosgwadmin

import (
	"time"

	apisv1alpha1 "github.com/linode/provider-ceph/apis/v1alpha1"
	"github.com/linode/provider-ceph/internal/utils"
	rgw "github.com/myENA/radosgwadmin"
	rcl "github.com/myENA/restclient"
)

const clientTimeoutInSecs = 10

func NewClient(data map[string][]byte, pcSpec *apisv1alpha1.ProviderConfigSpec) (*rgw.AdminAPI, error) {
	cfg := &rgw.Config{
		ClientConfig: rcl.ClientConfig{
			ClientTimeout: rcl.Duration(time.Second * clientTimeoutInSecs),
		},
		ServerURL:       utils.ResolveHostBase(pcSpec.HostBase, pcSpec.UseHTTPS),
		AdminPath:       "admin",
		AccessKeyID:     string(data[utils.AccessKey]),
		SecretAccessKey: string(data[utils.SecretKey]),
	}

	return rgw.NewAdminAPI(cfg)
}
