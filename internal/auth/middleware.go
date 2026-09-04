/*
internal/auth/middleware.go
The HTTP half of authentication: read the bearer token, resolve it, and put the
resulting Identity on the request context for handlers downstream.
*/
package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"

	log "github.com/sirupsen/logrus"
)

/*
contextKey is the type used to key the Identity on a request context.

It is an unexported empty struct rather than a string on purpose. Context keys
are compared by interface value, which includes the dynamic type, so a private
type cannot collide with a key set by any other package. An empty struct also
occupies no memory.
*/
type contextKey struct{}

var identityKey contextKey

// bearerScheme is the auth scheme from the event contract. RFC 7235 makes the
// scheme case-insensitive, so it is compared with strings.EqualFold.
const bearerScheme = "Bearer"

/*
NewContext returns a copy of ctx carrying identity.

Exported so tests and future callers can build an authenticated context without
going through an HTTP request.
*/
func NewContext(ctx context.Context, identity Identity) context.Context {
	return context.WithValue(ctx, identityKey, identity)
}

/*
IdentityFrom returns the Identity stored on ctx.

The bool reports whether one was present. A context value is always a runtime
lookup that can miss, so this reports the miss instead of panicking and leaves
the caller to decide what a missing identity means.
*/
func IdentityFrom(ctx context.Context) (Identity, bool) {
	identity, ok := ctx.Value(identityKey).(Identity)

	return identity, ok
}

/*
Middleware returns a decorator that authenticates a request before passing it on.

The returned func(http.Handler) http.Handler shape is the conventional Go
middleware signature, so these compose with each other and with anything else
in the ecosystem.

A request that does not authenticate never reaches the wrapped handler, which
is what makes project scoping a boundary rather than a convention: a handler
has no way to run without an Identity on its context.
*/
func Middleware(resolver Resolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiKey, ok := bearerToken(r)
			if !ok {
				unauthorized(w, "missing or malformed Authorization header")
				return
			}

			identity, err := resolver.Resolve(r.Context(), apiKey)
			if err != nil {
				// An unknown key is the caller's problem; a resolver that
				// failed to answer is ours, and must not read as a rejection.
				if errors.Is(err, ErrInvalidKey) {
					unauthorized(w, "invalid api key")
					return
				}

				log.WithError(err).WithFields(log.Fields{
					"method": r.Method,
					"path":   r.URL.Path,
				}).Error("api key resolution failed")

				http.Error(w, "internal error", http.StatusInternalServerError)

				return
			}

			next.ServeHTTP(w, r.WithContext(NewContext(r.Context(), identity)))
		})
	}
}

// bearerToken extracts the credential from an Authorization header, reporting
// whether the header was present and well formed.
func bearerToken(r *http.Request) (string, bool) {
	scheme, token, found := strings.Cut(r.Header.Get("Authorization"), " ")
	if !found || !strings.EqualFold(scheme, bearerScheme) || token == "" {
		return "", false
	}

	return token, true
}

// unauthorized writes a 401 with the challenge RFC 7235 requires. The message
// never distinguishes an unknown key from a malformed header in a way that
// helps an attacker enumerate valid keys.
func unauthorized(w http.ResponseWriter, message string) {
	w.Header().Set("WWW-Authenticate", bearerScheme)
	http.Error(w, message, http.StatusUnauthorized)
}
