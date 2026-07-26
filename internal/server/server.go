// Package server provides the HTTP server, proxy, and management handlers for AK Switch.
package server

import (
	"akswitch/internal/circuitbreaker"
	"akswitch/internal/config"
	"akswitch/internal/keypool"
	"net/http"
	"strings"
	"time"
)

// ProxyEngine holds the HTTP client and circuit breakers for upstream proxy requests.
type ProxyEngine struct {
	client *http.Client
	upCB   *circuitbreaker.UpstreamCircuitBreaker
}

// NewProxyEngine creates a ProxyEngine from config and key count.
func NewProxyEngine(cfg *config.Config, pool *keypool.KeyPool) *ProxyEngine {
	backoffCapSec := cfg.BackoffCapSec
	if backoffCapSec <= 0 {
		backoffCapSec = 120
	}
	backoffMult := cfg.BackoffMultiplier
	if backoffMult <= 0 {
		backoffMult = 2
	}
	upstreamThreshold := cfg.UpstreamCBThreshold
	if upstreamThreshold <= 0 {
		upstreamThreshold = 5
	}
	cbResetSec := cfg.CBResetSec
	if cbResetSec <= 0 {
		cbResetSec = 30
	}
	base := time.Duration(cfg.CooldownSec) * time.Second
	cap_ := time.Duration(backoffCapSec) * time.Second
	pool.ConfigureCBs(base, cap_, backoffMult)

	upCB := circuitbreaker.NewUpstreamCircuitBreaker(
		upstreamThreshold,
		time.Duration(cbResetSec)*time.Second,
	)

	return &ProxyEngine{
		client: &http.Client{
			Timeout: time.Duration(cfg.HTTPTimeoutSec) * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        500,
				MaxIdleConnsPerHost: 100,
				IdleConnTimeout:     90 * time.Second,
				ForceAttemptHTTP2:   true,
			},
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		upCB: upCB,
	}
}

// keyPrefixes are known API key prefixes to mask in debug logging.
var keyPrefixes = []string{"sk-", "nvapi-"}

// MaskSensitiveData scrubs potential API key patterns from a string for safe debug logging.
// It masks any word-like token starting with a known key prefix by replacing it with "***".
// It also truncates the result to maxLen bytes.
func MaskSensitiveData(data string, maxLen int) string {
	if len(data) > maxLen {
		data = data[:maxLen]
	}
	// Mask known API key prefixes
	result := data
	lower := strings.ToLower(data)
	for _, prefix := range keyPrefixes {
		idx := strings.Index(lower, prefix)
		for idx >= 0 {
			// Find end of token (word boundary)
			end := idx + len(prefix)
			for end < len(result) && (isAlphaNum(result[end]) || result[end] == '-' || result[end] == '_') {
				end++
			}
			if end > idx+len(prefix) {
				result = result[:idx] + "***" + result[end:]
				lower = strings.ToLower(result)
			}
			idx = strings.Index(lower, prefix)
		}
	}
	return result
}

func isAlphaNum(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// RouteEntry represents a single provider's routing info.
type RouteEntry struct {
	Config *config.Config
	Pool   *keypool.KeyPool
	Proxy  *ProxyEngine
}