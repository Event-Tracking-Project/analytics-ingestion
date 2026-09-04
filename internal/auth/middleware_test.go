/*
internal/auth/middleware_test.go
Tests for bearer-token extraction, key resolution and what reaches the wrapped
handler.
*/
package auth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	log "github.com/sirupsen/logrus"
)

const testKey = "key_abc"

var testIdentity = Identity{ProjectID: "proj_1", OrgID: "org_1"}

func testResolver() *StaticResolver {
	return NewStaticResolver(map[string]Identity{testKey: testIdentity})
}

// TestMain silences logrus for the whole package: the resolver-failure path
// logs by design, and that output is noise in a passing run.
func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)

	os.Exit(m.Run())
}

// spyHandler records whether it ran and what identity it saw.
type spyHandler struct {
	called   bool
	identity Identity
	found    bool
}

func (s *spyHandler) ServeHTTP(_ http.ResponseWriter, r *http.Request) {
	s.called = true
	s.identity, s.found = IdentityFrom(r.Context())
}

func TestMiddlewareRejects(t *testing.T) {
	tests := []struct {
		name       string
		authHeader string
		wantStatus int
	}{
		{name: "no header", authHeader: "", wantStatus: http.StatusUnauthorized},
		{name: "no scheme", authHeader: testKey, wantStatus: http.StatusUnauthorized},
		{name: "wrong scheme", authHeader: "Basic " + testKey, wantStatus: http.StatusUnauthorized},
		{name: "empty token", authHeader: "Bearer ", wantStatus: http.StatusUnauthorized},
		{name: "unknown key", authHeader: "Bearer nope", wantStatus: http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spy := &spyHandler{}
			h := Middleware(testResolver())(spy)

			req := httptest.NewRequest(http.MethodPost, "/v1/event", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}

			// The point of the middleware: a rejected request must never reach
			// the handler behind it.
			if spy.called {
				t.Error("wrapped handler ran for an unauthenticated request")
			}

			if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
				t.Errorf("WWW-Authenticate = %q, want %q", got, "Bearer")
			}
		})
	}
}

func TestMiddlewareAcceptsValidKey(t *testing.T) {
	tests := []struct {
		name   string
		header string
	}{
		{name: "canonical scheme", header: "Bearer " + testKey},
		// RFC 7235 makes the scheme case-insensitive.
		{name: "lowercase scheme", header: "bearer " + testKey},
		{name: "uppercase scheme", header: "BEARER " + testKey},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spy := &spyHandler{}
			h := Middleware(testResolver())(spy)

			req := httptest.NewRequest(http.MethodPost, "/v1/event", nil)
			req.Header.Set("Authorization", tt.header)

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if !spy.called {
				t.Fatalf("wrapped handler did not run (status %d)", rec.Code)
			}

			if !spy.found {
				t.Fatal("no identity on the context the handler received")
			}

			if spy.identity != testIdentity {
				t.Errorf("identity = %+v, want %+v", spy.identity, testIdentity)
			}
		})
	}
}

// failingResolver fails every lookup with an error that is not ErrInvalidKey.
type failingResolver struct {
	err error
}

func (f failingResolver) Resolve(_ context.Context, _ string) (Identity, error) {
	return Identity{}, f.err
}

func TestMiddlewareResolverFailureIsNotARejection(t *testing.T) {
	spy := &spyHandler{}
	h := Middleware(failingResolver{err: errors.New("key database unreachable")})(spy)

	req := httptest.NewRequest(http.MethodPost, "/v1/event", nil)
	req.Header.Set("Authorization", "Bearer "+testKey)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	// A resolver outage must not be reported as a bad key: that would tell a
	// caller their credentials are wrong when they are fine.
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	if spy.called {
		t.Error("wrapped handler ran despite a failed resolve")
	}

	// The cause belongs in the log, not in a response to an unauthenticated caller.
	if body := rec.Body.String(); strings.Contains(body, "unreachable") {
		t.Errorf("body = %q, want the resolver error to stay out of the response", body)
	}
}

func TestIdentityFromMissing(t *testing.T) {
	if _, ok := IdentityFrom(context.Background()); ok {
		t.Error("IdentityFrom() reported an identity on a bare context")
	}
}

func TestStaticResolverCopiesItsKeys(t *testing.T) {
	keys := map[string]Identity{testKey: testIdentity}
	resolver := NewStaticResolver(keys)

	// Mutating the caller's map must not change what authenticates.
	keys["backdoor"] = Identity{ProjectID: "attacker"}
	delete(keys, testKey)

	if _, err := resolver.Resolve(context.Background(), "backdoor"); !errors.Is(err, ErrInvalidKey) {
		t.Error("a key added to the caller's map after construction resolved")
	}

	if _, err := resolver.Resolve(context.Background(), testKey); err != nil {
		t.Errorf("Resolve() error = %v, want the original key to still resolve", err)
	}
}

func TestStaticResolverUnknownKey(t *testing.T) {
	_, err := testResolver().Resolve(context.Background(), "missing")

	if !errors.Is(err, ErrInvalidKey) {
		t.Errorf("Resolve() error = %v, want ErrInvalidKey", err)
	}
}
