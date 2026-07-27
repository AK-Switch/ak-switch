package server

import (
	"net/http"
	"strings"
)

// copyHeaders copies headers from src to dst, filtering out sensitive headers.
func copyHeaders(dst, src http.Header) {
	for k, vals := range src {
		lower := strings.ToLower(k)
		if lower == "x-admin-token" || lower == "cookie" || lower == "proxy-authorization" {
			continue
		}
		for _, v := range vals {
			dst.Add(k, v)
		}
	}
}