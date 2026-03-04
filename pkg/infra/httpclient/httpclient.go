// Package httpclient provides a centralized HTTP client factory for PicoClaw.
//
// All outgoing HTTP requests (LLM APIs, skill registries, web tools, channels)
// should use this package instead of creating ad-hoc http.Client instances.
// This ensures consistent proxy, timeout, and transport settings across the
// entire application.
//
// Usage:
//
//	httpclient.Init("http://127.0.0.1:7890")  // call once at startup
//	client := httpclient.New(30 * time.Second) // per-component timeout
//	client := httpclient.Default()             // shared default (30s timeout)
package httpclient

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	DefaultTimeout = 30 * time.Second
)

var (
	mu              sync.RWMutex
	globalProxy     string
	globalTransport *http.Transport
	defaultClient   *http.Client
)

func init() {
	globalTransport = newTransport("")
	defaultClient = &http.Client{
		Timeout:   DefaultTimeout,
		Transport: globalTransport,
	}
}

// Init configures the global proxy for all HTTP clients created via this package.
// Call once at startup before any HTTP requests are made.
// An empty proxyURL means use the system environment proxy (HTTP_PROXY, etc.).
func Init(proxyURL string) {
	mu.Lock()
	defer mu.Unlock()

	globalProxy = proxyURL
	globalTransport = newTransport(proxyURL)
	defaultClient = &http.Client{
		Timeout:   DefaultTimeout,
		Transport: globalTransport,
	}

	if proxyURL != "" {
		log.Printf("httpclient: global proxy configured: %s", proxyURL)
	}
}

// Default returns the shared default HTTP client (30s timeout, global proxy).
// This client is safe for concurrent use.
func Default() *http.Client {
	mu.RLock()
	defer mu.RUnlock()
	return defaultClient
}

// New creates a new HTTP client with the given timeout and global proxy settings.
// Each caller gets its own client (with its own timeout) but shares the global
// transport configuration (proxy, TLS, connection pooling).
func New(timeout time.Duration) *http.Client {
	mu.RLock()
	transport := globalTransport
	mu.RUnlock()

	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}
}

// NewWithProxy creates an HTTP client with a specific proxy override.
// Use this when a component has its own proxy configured (e.g., per-model proxy).
// If proxyURL is empty, falls back to the global proxy.
func NewWithProxy(timeout time.Duration, proxyURL string) (*http.Client, error) {
	if proxyURL == "" {
		return New(timeout), nil
	}

	transport, err := newTransportValidated(proxyURL)
	if err != nil {
		return nil, err
	}

	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}, nil
}

// Proxy returns the currently configured global proxy URL.
func Proxy() string {
	mu.RLock()
	defer mu.RUnlock()
	return globalProxy
}

// newTransport creates an http.Transport with proxy support.
func newTransport(proxyURL string) *http.Transport {
	t := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 15 * time.Second,
		DisableCompression:  false,
	}

	if proxyURL != "" {
		parsed, err := url.Parse(proxyURL)
		if err == nil {
			t.Proxy = http.ProxyURL(parsed)
		} else {
			log.Printf("httpclient: invalid proxy URL %q, falling back to env: %v", proxyURL, err)
			t.Proxy = http.ProxyFromEnvironment
		}
	} else {
		t.Proxy = http.ProxyFromEnvironment
	}

	return t
}

// newTransportValidated creates a transport with strict proxy URL validation.
func newTransportValidated(proxyURL string) (*http.Transport, error) {
	parsed, err := url.Parse(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy URL: %w", err)
	}

	scheme := strings.ToLower(parsed.Scheme)
	switch scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return nil, fmt.Errorf(
			"unsupported proxy scheme %q (supported: http, https, socks5, socks5h)",
			parsed.Scheme,
		)
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("invalid proxy URL: missing host")
	}

	t := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 15 * time.Second,
		DisableCompression:  false,
		Proxy:               http.ProxyURL(parsed),
	}
	return t, nil
}
