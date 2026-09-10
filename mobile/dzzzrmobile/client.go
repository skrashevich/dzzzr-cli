package dzzzrmobile

import (
	"context"
	"sync"
	"time"

	"github.com/skrashevich/dzzzr-cli/dzzzr"
)

const (
	defaultCodeSendTimeout = 10 * time.Second
	defaultMinInterval     = 350 * time.Millisecond
)

// DzzzrClient wraps dzzzr.Client for use from Swift or Kotlin.
//
// Beyond the language boundary it adds two things a phone needs: a short
// timeout for code submissions, because a player standing at a wall wants to
// know quickly whether the code went through, and a floor on the interval
// between requests, because the engine is a shared PHP host and a screen that
// polls on every redraw would hammer it.
type DzzzrClient struct {
	client *dzzzr.Client

	mu              sync.Mutex
	codeSendTimeout time.Duration
	minInterval     time.Duration
	lastRequest     time.Time
}

func newClient(c *dzzzr.Client) *DzzzrClient {
	return &DzzzrClient{client: c, codeSendTimeout: defaultCodeSendTimeout, minInterval: defaultMinInterval}
}

// NewClient creates a client for a city of classic.dzzzr.ru, for example
// "moscow".
func NewClient(city string) *DzzzrClient {
	return newClient(dzzzr.New(city))
}

// NewClientWithOptions creates a client with an explicit engine root (for a
// test server), optional plain HTTP, optional TLS verification skipping and a
// request timeout in seconds (0 = default).
func NewClientWithOptions(city, baseURL string, insecureTLS, useHTTP bool, timeoutSeconds int64) *DzzzrClient {
	var opts []dzzzr.Option
	if baseURL != "" {
		opts = append(opts, dzzzr.WithBaseURL(baseURL))
	}
	if useHTTP {
		opts = append(opts, dzzzr.WithHTTP())
	}
	if insecureTLS {
		opts = append(opts, dzzzr.WithInsecureTLS())
	}
	if timeoutSeconds > 0 {
		opts = append(opts, dzzzr.WithTimeout(time.Duration(timeoutSeconds)*time.Second))
	}
	return newClient(dzzzr.New(city, opts...))
}

// City returns the city the client talks to.
func (c *DzzzrClient) City() string { return c.client.City() }

// BaseURL returns the engine root the client talks to.
func (c *DzzzrClient) BaseURL() string { return c.client.BaseURL() }

// SetUserAgent sets the User-Agent header, so an app can identify itself.
func (c *DzzzrClient) SetUserAgent(ua string) { c.client.SetUserAgent(ua) }

// SetCodeSendTimeoutSeconds sets the per-request timeout for code
// submissions. Zero or less restores the default.
func (c *DzzzrClient) SetCodeSendTimeoutSeconds(seconds int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if seconds <= 0 {
		c.codeSendTimeout = defaultCodeSendTimeout
		return
	}
	c.codeSendTimeout = time.Duration(seconds) * time.Second
}

// SetRequestMinIntervalMillis sets the minimum interval between requests.
// Zero or less disables pacing.
func (c *DzzzrClient) SetRequestMinIntervalMillis(milliseconds int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if milliseconds <= 0 {
		c.minInterval = 0
		return
	}
	c.minInterval = time.Duration(milliseconds) * time.Millisecond
}

// pace reserves the next slot under the lock and waits outside it, so the
// mutex is never held during a sleep.
func (c *DzzzrClient) pace() {
	c.mu.Lock()
	var wait time.Duration
	if d := c.minInterval; d > 0 {
		now := time.Now()
		next := c.lastRequest.Add(d)
		if !c.lastRequest.IsZero() && next.After(now) {
			wait = next.Sub(now)
			c.lastRequest = next
		} else {
			c.lastRequest = now
		}
	}
	c.mu.Unlock()
	if wait > 0 {
		time.Sleep(wait)
	}
}

// ctx returns a paced background context for a normal request.
func (c *DzzzrClient) ctx() context.Context {
	c.pace()
	return context.Background()
}

// codeCtx returns a paced context with the code-send timeout.
func (c *DzzzrClient) codeCtx() (context.Context, context.CancelFunc) {
	c.pace()
	c.mu.Lock()
	d := c.codeSendTimeout
	c.mu.Unlock()
	if d <= 0 {
		d = defaultCodeSendTimeout
	}
	return context.WithTimeout(context.Background(), d)
}
