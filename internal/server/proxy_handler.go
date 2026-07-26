package server

import (
	"akswitch/internal/config"
	"io"
	"net/http"
	"strings"
)

// ── Proxy Handler ──────────────────────────────────────

func (pr *ProviderRouter) proxyHandler(w http.ResponseWriter, r *http.Request) {
	// Extract provider name from path: /{provider}/...
	providerName, restPath := pr.extractProvider(r.URL.Path)
	if providerName == "" {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "no provider specified in path"})
		return
	}

	pr.mu.RLock()
	ps, ok := pr.providers[providerName]
	pr.mu.RUnlock()
	if !ok {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "provider not found: " + providerName})
		return
	}

	// Rewrite the URL path (strip provider prefix)
	r.URL.Path = restPath

	// Delegate to the proxy executor for the full request lifecycle
	pr.proxyExecutor.Execute(w, r, ps)
}

// ── Extracted Utilities ───────────────────────────────

// readRequestBody reads and limits the request body to 10MB.
// Returns the body bytes, or nil and an error if the body is too large.
func readRequestBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20) // 10MB limit
	bodyBytes, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		writeProxyError(w, http.StatusBadRequest, ErrorBadRequest, "request body too large or unreadable")
		return nil, err
	}
	return bodyBytes, nil
}

// buildTargetURL constructs the upstream URL based on path routing rules.
// Routes /genai/ paths to GenaiBase, everything else to TargetBase.
func buildTargetURL(cfg *config.Config, path, rawQuery string) string {
	if strings.Contains(path, "/genai/") {
		target := cfg.GenaiBase + path
		if rawQuery != "" {
			target += "?" + rawQuery
		}
		return target
	}
	if strings.HasSuffix(cfg.TargetBase, "/v1") && strings.HasPrefix(path, "/v1") {
		path = path[3:]
	}
	if rawQuery != "" {
		path += "?" + rawQuery
	}
	return cfg.TargetBase + path
}