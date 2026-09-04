/*
internal/auth/auth.go
Project API-key authentication.

The SDK authenticates with `Authorization: Bearer <PROJECT_API_KEY>`, and the
key is what determines which project and organization an event belongs to. The
client is never trusted to state that itself.

This package owns the seam, not the answer: Resolver is the interface, and main
picks the implementation. Today that is StaticResolver, a hardcoded dev table.
Replacing it with a database lookup is a new type in this package and one line
in main, with no change to any handler.
*/
package auth

import (
	"context"
	"errors"
	"maps"
)

// ErrInvalidKey is the sentinel returned when an API key does not resolve to a
// project. Callers match it with errors.Is to answer 401 rather than 500: an
// unknown key is the client's problem, a resolver that cannot reach its
// database is ours.
var ErrInvalidKey = errors.New("invalid api key")

/*
Identity is the authoritative ownership of a request, derived from the API key.

These are the two fields the event contract marks as server-sourced. Once a
request carries an Identity, handlers stamp it onto every event rather than
reading project_id or org_id out of the request body.
*/
type Identity struct {
	ProjectID string
	OrgID     string
}

/*
Resolver turns a project API key into the identity it belongs to.

It returns an error wrapping ErrInvalidKey when the key is unknown, and any
other error when the lookup itself failed.
*/
type Resolver interface {
	Resolve(ctx context.Context, apiKey string) (Identity, error)
}

// Compile-time assertion that the dev resolver satisfies the interface, so a
// signature drift is a build failure rather than a wiring surprise in main.
var _ Resolver = (*StaticResolver)(nil)

/*
StaticResolver resolves keys from a fixed in-memory table.

Development only. A real implementation must not hold raw API keys at all: it
should store a hash of each key and look up by hash, so a leaked table is not a
set of working credentials. Key creation and revocation are still open
questions in the event contract.
*/
type StaticResolver struct {
	keys map[string]Identity
}

// NewStaticResolver returns a resolver over a copy of keys, so a caller that
// keeps and mutates the original map cannot change what authenticates later.
func NewStaticResolver(keys map[string]Identity) *StaticResolver {
	return &StaticResolver{
		keys: maps.Clone(keys),
	}
}

// Resolve looks up apiKey in the static table.
func (r *StaticResolver) Resolve(_ context.Context, apiKey string) (Identity, error) {
	identity, ok := r.keys[apiKey]
	if !ok {
		return Identity{}, ErrInvalidKey
	}

	return identity, nil
}
