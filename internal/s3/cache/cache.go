package cache

import (
	"context"
	"sync"
	"time"

	"github.com/allegro/bigcache/v3"
	"gopkg.in/alecthomas/kingpin.v2"
)

var (
	BucketExistsCacheTTL = time.Duration(0)

	bucketExistsCache      *bigcache.BigCache
	bucketExistsCacheEntry = make([]byte, 0)
	bucketExistsCacheInit  = sync.Once{}
)

func bucketExistsCacheInitFunc() {
	config := bigcache.DefaultConfig(BucketExistsCacheTTL)
	config.MaxEntrySize = 0
	config.Verbose = false
	config.Logger = nil

	var err error
	bucketExistsCache, err = bigcache.New(context.Background(), config)
	kingpin.FatalIfError(err, "Cannot create S3 bucket exists client cache")
}

func Exists(key string) bool {
	bucketExistsCacheInit.Do(bucketExistsCacheInitFunc)

	_, err := bucketExistsCache.Get(key)

	return err == nil
}

func Set(key string) {
	bucketExistsCacheInit.Do(bucketExistsCacheInitFunc)

	err := bucketExistsCache.Set(key, bucketExistsCacheEntry)
	if err != nil {
		kingpin.Errorf("failed to set bucket exists cache entry: %w", err)
	}
}
