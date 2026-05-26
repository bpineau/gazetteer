package memcache_test

import (
	"testing"

	"github.com/bpineau/gazetteer/pkg/kvcache"
	"github.com/bpineau/gazetteer/pkg/kvcache/kvcachetest"
	"github.com/bpineau/gazetteer/pkg/kvcache/memcache"
)

func TestSuite(t *testing.T) {
	kvcachetest.Suite(t, func(*testing.T) kvcache.Cache {
		return memcache.New()
	})
}
