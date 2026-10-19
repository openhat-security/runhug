package netguard

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIsLoopbackHost(t *testing.T) {
	for h, want := range map[string]bool{
		"127.0.0.1": true, "::1": true, "[::1]": true, "localhost": true, "127.0.0.2": true,
		"": false, "0.0.0.0": false, "::": false, "10.0.0.5": false, "example.com": false,
	} {
		if got := IsLoopbackHost(h); got != want {
			t.Errorf("IsLoopbackHost(%q)=%v want %v", h, got, want)
		}
	}
}

func TestCheckBind(t *testing.T) {
	strong := strings.Repeat("k", MinKeyLen)
	cases := []struct {
		addr, key string
		allow     bool
		ok        bool
	}{
		{"127.0.0.1:0", "", false, true},
		{"[::1]:9090", "", false, true},
		{"0.0.0.0:9090", strong, false, false}, // not opted in
		{":9090", strong, false, false},        // empty host = all interfaces
		{"0.0.0.0:9090", "", true, false},      // opted in, no key
		{"0.0.0.0:9090", "short", true, false}, // weak key
		{"10.0.0.5:9090", strong, true, true},
	}
	for _, c := range cases {
		err := CheckBind(c.addr, c.key, c.allow)
		if (err == nil) != c.ok {
			t.Errorf("CheckBind(%q,%q,%v) err=%v want ok=%v", c.addr, c.key, c.allow, err, c.ok)
		}
	}
}

func TestCheckBindNeverLeaksKey(t *testing.T) {
	secret := "sk-very-secret-but-too-short"
	err := CheckBind("0.0.0.0:1", secret, true)
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("err=%v must be non-nil and not contain key", err)
	}
}

func TestGenerateKeyStrong(t *testing.T) {
	a, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := GenerateKey()
	if a == b || len(a) < MinKeyLen {
		t.Fatalf("weak/duplicate keys %q %q", a, b)
	}
	if err := CheckBind("0.0.0.0:1", a, true); err != nil {
		t.Fatalf("generated key rejected: %v", err)
	}
}

func TestGuard(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
	key := strings.Repeat("z", MinKeyLen)
	do := func(h http.Handler, remote, target string, hdr map[string]string) int {
		r := httptest.NewRequest("GET", target, nil)
		r.RemoteAddr = remote
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	local := Guard(ok, "", false)
	if c := do(local, "127.0.0.1:5555", "/", nil); c != 204 {
		t.Fatalf("loopback v4 got %d", c)
	}
	if c := do(local, "[::1]:5555", "/", nil); c != 204 {
		t.Fatalf("loopback v6 got %d", c)
	}
	if c := do(local, "192.168.1.9:5555", "/", nil); c != 403 {
		t.Fatalf("remote peer not refused: %d", c)
	}
	remote := Guard(ok, key, true)
	if c := do(remote, "192.168.1.9:5555", "/", nil); c != 401 {
		t.Fatalf("missing key got %d", c)
	}
	if c := do(remote, "192.168.1.9:5555", "/?api_key="+key+"&key="+key, nil); c != 401 {
		t.Fatalf("query-param key must be ignored, got %d", c)
	}
	if c := do(remote, "192.168.1.9:5555", "/", map[string]string{"Authorization": "Bearer " + key}); c != 204 {
		t.Fatalf("bearer key got %d", c)
	}
	if c := do(remote, "192.168.1.9:5555", "/", map[string]string{"Authorization": "Bearer wrong"}); c != 401 {
		t.Fatalf("wrong key got %d", c)
	}
}

// The key must never reach logs: standard-library logging of errors/refusals
// produced while guarding must not contain it.
func TestKeyNeverLogged(t *testing.T) {
	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(old)
	key, _ := GenerateKey()
	srv := httptest.NewServer(Guard(http.NotFoundHandler(), key, true))
	defer srv.Close()
	req, _ := http.NewRequest("GET", srv.URL+"/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	_ = CheckBind("0.0.0.0:1", key[:10], true)
	if strings.Contains(buf.String(), key) || strings.Contains(buf.String(), key[:10]) {
		t.Fatalf("key leaked to log: %q", buf.String())
	}
}
