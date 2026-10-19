// Package netguard enforces runhug's exposure rules for every local listener
// (the Anthropic bridge, the OpenAI proxy, and any future `start <agent>` target):
//
//   - bind loopback (127.0.0.1 / ::1) by default;
//   - binding any other host requires an explicit opt-in AND a strong API key,
//     otherwise startup fails closed;
//   - when not opted in, connections from non-loopback peers are refused;
//   - the key is only ever accepted from a request header (Authorization: Bearer
//     or x-api-key), never a query parameter, and is never printed.
//
// Design adapted from Unsloth's exposure rules (behavior only, no code copied).
package netguard

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"strings"
)

// DefaultHost is the bind host used when none is given.
const DefaultHost = "127.0.0.1"

// EnvAllowRemote opts in to non-loopback binds/peers when set to 1/true/yes.
const EnvAllowRemote = "RUNHUG_ALLOW_REMOTE"

// EnvAPIKey holds the inbound API key required for non-loopback binds.
const EnvAPIKey = "RUNHUG_API_KEY"

// MinKeyLen is the minimum accepted API key length for remote exposure.
const MinKeyLen = 32

// Truthy reports whether an env-style value means "on".
func Truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// IsLoopbackHost reports whether host (no port) is a loopback name/address.
// An empty host means "all interfaces" and is NOT loopback.
func IsLoopbackHost(host string) bool {
	h := strings.Trim(strings.TrimSpace(host), "[]")
	if h == "" {
		return false
	}
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// JoinHostPort builds a listen address, defaulting host to DefaultHost.
func JoinHostPort(host string, port int) string {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if host == "" {
		host = DefaultHost
	}
	return net.JoinHostPort(host, fmt.Sprint(port))
}

// CheckBind validates a listen address against the exposure rules. It never
// includes the key in its error text.
func CheckBind(addr, key string, allowRemote bool) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid listen address %q: %w", addr, err)
	}
	if IsLoopbackHost(host) {
		return nil
	}
	shown := host
	if shown == "" {
		shown = "0.0.0.0 (all interfaces)"
	}
	if !allowRemote {
		return fmt.Errorf("refusing to listen on non-loopback host %s: runhug binds 127.0.0.1 by default; pass --allow-remote (or set %s=1) to expose it", shown, EnvAllowRemote)
	}
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("refusing to listen on %s without an API key: set %s (or use --generate-key) so remote clients must send Authorization: Bearer <key>", shown, EnvAPIKey)
	}
	if len(strings.TrimSpace(key)) < MinKeyLen {
		return fmt.Errorf("refusing to listen on %s: API key is too weak (need at least %d characters; use --generate-key)", shown, MinKeyLen)
	}
	return nil
}

// GenerateKey returns a strong random key (32 bytes, base64url, 43 chars).
func GenerateKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "rh_" + base64.RawURLEncoding.EncodeToString(b), nil
}

// IsLoopbackPeer reports whether an http.Request RemoteAddr is loopback.
func IsLoopbackPeer(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

// RequestKey extracts the client key from headers only (never the query string).
func RequestKey(r *http.Request) string {
	if ah := r.Header.Get("Authorization"); len(ah) > 7 && strings.EqualFold(ah[:7], "bearer ") {
		return strings.TrimSpace(ah[7:])
	}
	return strings.TrimSpace(r.Header.Get("x-api-key"))
}

// KeyMatches compares in constant time.
func KeyMatches(got, want string) bool {
	return got != "" && subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// Guard wraps h: non-loopback peers are refused unless allowRemote; when key
// is non-empty every request must carry it as a header.
func Guard(h http.Handler, key string, allowRemote bool) http.Handler {
	key = strings.TrimSpace(key)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowRemote && !IsLoopbackPeer(r.RemoteAddr) {
			http.Error(w, "forbidden: runhug only accepts loopback connections (start with --allow-remote to expose)", http.StatusForbidden)
			return
		}
		if key != "" && !KeyMatches(RequestKey(r), key) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="runhug"`)
			http.Error(w, "unauthorized: send Authorization: Bearer <key>", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	})
}
