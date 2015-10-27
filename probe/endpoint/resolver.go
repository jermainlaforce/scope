package endpoint

import (
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/bluele/gcache"
)

const (
	rAddrCacheLen        = 500 // Default cache length
	rAddrBacklog         = 1000
	rAddrCacheExpiration = 30 * time.Minute
)

var errNotFound = fmt.Errorf("not found")

type revResFunc func(addr string) (names []string, err error)

// A caching, reverse resolver.
type reverseResolver struct {
	addresses chan string
	cache     gcache.Cache
	Throttle  <-chan time.Time // Made public for mocking
	Resolver  revResFunc
}

// newReverseResolver starts a new reverse resolver that performs reverse
// resolutions and caches the result.
func newReverseResolver() *reverseResolver {
	r := reverseResolver{
		addresses: make(chan string, rAddrBacklog),
		cache:     gcache.New(rAddrCacheLen).LRU().Expiration(rAddrCacheExpiration).Build(),
		Throttle:  time.Tick(time.Second / 10),
		Resolver:  net.LookupAddr,
	}
	go r.loop()
	return &r
}

// get the reverse resolution for an IP address if already in the cache, a
// gcache.NotFoundKeyError error otherwise. Note: it returns one of the
// possible names that can be obtained for that IP.
func (r *reverseResolver) get(address string) (string, error) {
	val, err := r.cache.Get(address)
	if hostname, ok := val.(string); err == nil && ok {
		return hostname, nil
	}
	if _, ok := val.(struct{}); err == nil && ok {
		return "", errNotFound
	}
	if err == gcache.NotFoundKeyError {
		// We trigger a asynchronous reverse resolution when not cached.
		select {
		case r.addresses <- address:
		default:
		}
	}
	return "", errNotFound
}

func (r *reverseResolver) loop() {
	for request := range r.addresses {
		// check if the answer is already in the cache
		if _, err := r.cache.Get(request); err == nil {
			continue
		}
		<-r.Throttle // rate limit our DNS resolutions
		names, err := r.Resolver(request)
		if err == nil && len(names) > 0 {
			sort.Strings(names)
			name := strings.TrimRight(names[0], ".")
			r.cache.Set(request, name)
		} else {
			r.cache.Set(request, struct{}{})
		}
	}
}

func (r *reverseResolver) stop() {
	close(r.addresses)
}
