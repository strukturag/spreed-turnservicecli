package turnservicecli

import (
	"sync"
	"time"
)

// CachedCredentialsData combine CredentialsData with a expiration timer.
type CachedCredentialsData struct {
	sync.RWMutex

	Turn    *CredentialsData
	expires int64
	expired bool

	closed bool
	quit   chan bool
}

// NewCachedCredentialsData add expiration timer with a percentile to CredentialsData.
func NewCachedCredentialsData(turn *CredentialsData, expirationPercentile uint) *CachedCredentialsData {
	c := &CachedCredentialsData{
		Turn:    turn,
		expires: time.Now().Unix() + turn.TTL,
		quit:    make(chan bool),
	}

	go func() {
		expiry := turn.TTL / 100 * int64(expirationPercentile)
		select {
		case <-c.quit:
		case <-time.After(time.Duration(expiry) * time.Second):
		}
		c.Lock()
		defer c.Unlock()
		c.expired = true
	}()

	return c
}

// Expired returns if the cached CredentialsData has expired.
func (c *CachedCredentialsData) Expired() bool {
	c.RLock()
	defer c.RUnlock()
	return c.expired || c.closed
}

// TTL returns the remaining TTL of the cached CredentialsData.
func (c *CachedCredentialsData) TTL() int64 {
	ttl := c.expires - time.Now().Unix()
	if ttl < 0 {
		return 0
	}
	return ttl
}

// Close closes the cached CredentialsData and expires it if not already expired.
func (c *CachedCredentialsData) Close() {
	c.Lock()
	defer c.Unlock()
	if !c.expired {
		close(c.quit)
	}
	c.closed = true
}
