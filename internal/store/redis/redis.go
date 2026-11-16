// Package redis wraps go-redis for cache, counters, locks and pub/sub. Every
// method degrades gracefully: callers treat errors as a cache miss, and the
// usage module falls back to in-memory limits (SYSTEM_ARCHITECTURE §9).
package redis

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// Client is the shared Redis handle.
type Client struct {
	R *goredis.Client
}

// ErrMiss is returned when a key does not exist.
var ErrMiss = errors.New("redis: miss")

// Open parses the URL and pings once; a failed ping is returned but the
// client is still usable once Redis comes back.
func Open(ctx context.Context, url string) (*Client, error) {
	opt, err := goredis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	opt.DialTimeout = 2 * time.Second
	opt.ReadTimeout = 2 * time.Second
	opt.WriteTimeout = 2 * time.Second
	c := &Client{R: goredis.NewClient(opt)}
	pctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return c, c.R.Ping(pctx).Err()
}

// Close closes the client.
func (c *Client) Close() error { return c.R.Close() }

// Ping checks connectivity.
func (c *Client) Ping(ctx context.Context) error { return c.R.Ping(ctx).Err() }

// GetJSON decodes a cached value into out.
func (c *Client) GetJSON(ctx context.Context, key string, out any) error {
	b, err := c.R.Get(ctx, key).Bytes()
	if errors.Is(err, goredis.Nil) {
		return ErrMiss
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

// SetJSON caches v for ttl.
func (c *Client) SetJSON(ctx context.Context, key string, v any, ttl time.Duration) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.R.Set(ctx, key, b, ttl).Err()
}

// Incr increments a counter, setting its expiry on first use, and returns
// the new value.
func (c *Client) Incr(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	pipe := c.R.TxPipeline()
	incr := pipe.Incr(ctx, key)
	pipe.ExpireNX(ctx, key, ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, err
	}
	return incr.Val(), nil
}

// Get returns an integer counter (0 when missing).
func (c *Client) GetInt(ctx context.Context, key string) (int64, error) {
	v, err := c.R.Get(ctx, key).Int64()
	if errors.Is(err, goredis.Nil) {
		return 0, nil
	}
	return v, err
}

// Decr decrements a counter.
func (c *Client) Decr(ctx context.Context, key string) error {
	return c.R.Decr(ctx, key).Err()
}

// Lock sets key if absent for ttl; true when acquired.
func (c *Client) Lock(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	return c.R.SetNX(ctx, key, "1", ttl).Result()
}

// Unlock deletes a lock key.
func (c *Client) Unlock(ctx context.Context, key string) error {
	return c.R.Del(ctx, key).Err()
}

// Publish sends a message on a channel.
func (c *Client) Publish(ctx context.Context, channel, msg string) error {
	return c.R.Publish(ctx, channel, msg).Err()
}

// Subscribe returns a subscription; the caller closes it.
func (c *Client) Subscribe(ctx context.Context, channels ...string) *goredis.PubSub {
	return c.R.Subscribe(ctx, channels...)
}
