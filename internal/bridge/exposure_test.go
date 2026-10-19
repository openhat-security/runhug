package bridge

import (
	"net/http"
	"strings"
	"testing"
)

func TestBridgeDefaultsToLoopback(t *testing.T) {
	srv, base, err := Start(Config{UpstreamBase: "http://127.0.0.1:1/v1"})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	if !strings.HasPrefix(base, "http://127.0.0.1:") {
		t.Fatalf("base %q", base)
	}
	if host := srv.ln.Addr().String(); !strings.HasPrefix(host, "127.0.0.1:") {
		t.Fatalf("bound %q, want loopback", host)
	}
	resp, err := http.Get(base + "/health")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("health %d", resp.StatusCode)
	}
}

func TestBridgeRefusesRemoteBindWithoutKey(t *testing.T) {
	secret := "tiny-secret"
	for _, c := range []Config{
		{UpstreamBase: "http://x/v1", ListenAddr: "0.0.0.0:0"},
		{UpstreamBase: "http://x/v1", ListenAddr: "0.0.0.0:0", AllowRemote: true},
		{UpstreamBase: "http://x/v1", ListenAddr: "0.0.0.0:0", AllowRemote: true, ExpectedToken: secret},
	} {
		srv, _, err := Start(c)
		if err == nil {
			srv.Close()
			t.Fatalf("expected refusal for %+v", c)
		}
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error leaks key: %v", err)
		}
	}
}

func TestBridgeRemoteBindWithStrongKey(t *testing.T) {
	key := strings.Repeat("a", 40)
	srv, base, err := Start(Config{UpstreamBase: "http://127.0.0.1:1/v1", ListenAddr: "0.0.0.0:0", AllowRemote: true, ExpectedToken: key})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	req, _ := http.NewRequest("GET", base+"/v1/models?key="+key, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("query-param key accepted: %d", resp.StatusCode)
	}
	req.URL.RawQuery = ""
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		t.Fatalf("bearer key rejected: %d", resp.StatusCode)
	}
}
