package server

import (
	"fmt"
	"net/http"
)

// lookupProvider returns the ProviderState for a given provider name.
func (pr *ProviderRouter) lookupProvider(name string) *ProviderState {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	return pr.providers[name]
}

// firstProvider returns the first (alphabetically) provider, or nil if none exist.
func (pr *ProviderRouter) firstProvider() *ProviderState {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	for _, ps := range pr.providers {
		return ps
	}
	return nil
}

// resolveProvider gets the provider specified by the "provider" query parameter.
// If not set, returns the first provider. Returns an error string if no provider found.
func (pr *ProviderRouter) resolveProvider(r *http.Request) (*ProviderState, string) {
	pName := r.URL.Query().Get("provider")
	if pName == "" {
		ps := pr.firstProvider()
		if ps == nil {
			return nil, "no providers configured"
		}
		return ps, ""
	}
	ps := pr.lookupProvider(pName)
	if ps == nil {
		return nil, fmt.Sprintf("provider %q not found", pName)
	}
	return ps, ""
}
