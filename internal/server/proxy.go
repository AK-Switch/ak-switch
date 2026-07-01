package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

// ErrorCode represents a machine-readable error category for proxy responses.
type ErrorCode string

const (
	ErrorBadRequest       ErrorCode = "BAD_REQUEST"
	ErrorUpstreamError    ErrorCode = "UPSTREAM_ERROR"
	ErrorAllKeysInvalid   ErrorCode = "ALL_KEYS_INVALID"
	ErrorExhaustedRetries ErrorCode = "EXHAUSTED_RETRIES"
)

// ErrorCategory represents whether an upstream response is retryable or not.
type ErrorCategory int

const (
	CatUnknown      ErrorCategory = iota
	CatRetryable                  // 可换 Key 重试：5xx、429、网络问题
	CatNonRetryable               // 客户端问题：400/422 等，换 Key 也解决不了
	CatClientAbort                // 客户端主动中断：不污染 Key 健康度
)

// categorizeError classifies an upstream HTTP status code (or network error)
// into an ErrorCategory. NonRetryable codes are returned immediately without
// consuming retry attempts and without penalizing key health.
func categorizeError(statusCode int, err error) ErrorCategory {
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return CatClientAbort
		}
		return CatRetryable
	}
	switch statusCode {
	case 400, 405, 406, 413, 414, 415, 422, 501:
		return CatNonRetryable
	default:
		return CatRetryable
	}
}

// writeProxyError writes a JSON error response with the given status code and error code.
func writeProxyError(w http.ResponseWriter, status int, code ErrorCode, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]interface{}{
			"code":    string(code),
			"message": message,
		},
	})
}
