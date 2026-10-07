package gcp

import (
	"context"
	"strings"
	"testing"
)

func TestParseARDockerImage(t *testing.T) {
	r, p, repo, ok := ParseARDockerImage("us-central1-docker.pkg.dev/otw-portal-dev/runhug/llama-server:cuda")
	if !ok || r != "us-central1" || p != "otw-portal-dev" || repo != "runhug" {
		t.Fatalf("%s %s %s %v", r, p, repo, ok)
	}
	if _, _, _, ok := ParseARDockerImage("gcr.io/x/y"); ok {
		t.Fatal("gcr")
	}
	if CanAutoPush("gcr.io/x/y") || !CanAutoPush("us-central1-docker.pkg.dev/p/runhug/llama-server:cuda") {
		t.Fatal("CanAutoPush")
	}
}

func TestEnsureDockerRepoCreatesWhenMissing(t *testing.T) {
	f := &fakeRunner{err: errSentinel{}}
	c := &Client{Runner: f}
	err := c.EnsureDockerRepo(context.Background(), "us-central1-docker.pkg.dev/p/runhug/llama-server:cuda")
	if err == nil || !strings.Contains(err.Error(), "create Artifact Registry") {
		t.Fatalf("got %v", err)
	}
	if len(f.calls) != 2 {
		t.Fatalf("describe then create: %v", f.calls)
	}
}

func TestArtifactImageExistsMissing(t *testing.T) {
	f := &fakeRunner{err: errSentinel{}}
	c := &Client{Runner: f}
	ok, err := c.ArtifactImageExists(context.Background(), "us-central1-docker.pkg.dev/p/runhug/llama-server:cuda")
	if err != nil || ok {
		t.Fatalf("%v %v", ok, err)
	}
}

type errSentinel struct{}

func (errSentinel) Error() string { return "NOT_FOUND" }

func TestEnsureDockerRepoDescribeOK(t *testing.T) {
	f := &fakeRunner{}
	c := &Client{Runner: f}
	if err := c.EnsureDockerRepo(context.Background(), "us-central1-docker.pkg.dev/p/runhug/llama-server:cuda"); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 1 || !strings.Contains(strings.Join(f.calls[0], " "), "repositories describe runhug") {
		t.Fatalf("%v", f.calls)
	}
}

func TestConfigureDockerAuth(t *testing.T) {
	f := &fakeRunner{}
	c := &Client{Runner: f}
	if err := c.ConfigureDockerAuth(context.Background(), "europe-west4-docker.pkg.dev/p/runhug/x:tag"); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(f.calls[0], " ")
	if !strings.Contains(got, "configure-docker europe-west4-docker.pkg.dev") {
		t.Fatalf("%s", got)
	}
}
