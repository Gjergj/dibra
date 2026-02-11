package secrets

import (
	"fmt"
	"strings"
)

// Provider fetches secrets from an external backend (Bitwarden, 1Password, etc.).
type Provider interface {
	// Lookup resolves a secret reference and returns the secret value.
	// The reference format is provider-specific.
	Lookup(reference string) (string, error)
}

// prefix maps a string prefix to its provider key.
type prefix struct {
	Prefix      string
	ProviderKey string
}

// Resolver routes secret references to registered providers.
type Resolver struct {
	providers map[string]Provider
	prefixes  []prefix
}

// NewResolver creates a Resolver with no registered providers.
func NewResolver() *Resolver {
	return &Resolver{
		providers: map[string]Provider{},
		prefixes: []prefix{
			{Prefix: "!op://", ProviderKey: "op"},
			{Prefix: "!bw:", ProviderKey: "bw"},
		},
	}
}

// Register adds a provider under the given key (e.g., "bw", "op").
func (r *Resolver) Register(key string, p Provider) {
	r.providers[key] = p
}

// ResolveValue checks if value starts with a known secret prefix and,
// if so, delegates to the matching provider. Plain values pass through unchanged.
func (r *Resolver) ResolveValue(value string) (string, error) {
	for _, pfx := range r.prefixes {
		if strings.HasPrefix(value, pfx.Prefix) {
			p, ok := r.providers[pfx.ProviderKey]
			if !ok {
				return "", fmt.Errorf("secret provider %q not registered (referenced by %q)", pfx.ProviderKey, value)
			}
			ref := strings.TrimPrefix(value, "!")
			return p.Lookup(ref)
		}
	}
	return value, nil
}

// ResolveMap recursively walks a variable map and resolves any secret references.
func (r *Resolver) ResolveMap(vars map[string]interface{}) (map[string]interface{}, error) {
	if vars == nil {
		return nil, nil
	}
	result := make(map[string]interface{}, len(vars))
	for k, v := range vars {
		resolved, err := r.resolveAny(v)
		if err != nil {
			return nil, fmt.Errorf("resolving key %q: %w", k, err)
		}
		result[k] = resolved
	}
	return result, nil
}

func (r *Resolver) resolveAny(v interface{}) (interface{}, error) {
	switch val := v.(type) {
	case string:
		return r.ResolveValue(val)
	case map[string]interface{}:
		return r.ResolveMap(val)
	case []interface{}:
		resolved := make([]interface{}, len(val))
		for i, item := range val {
			res, err := r.resolveAny(item)
			if err != nil {
				return nil, err
			}
			resolved[i] = res
		}
		return resolved, nil
	default:
		return v, nil
	}
}
