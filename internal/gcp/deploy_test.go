package gcp

import (
	"context"
	"strings"
	"testing"
)

type fakeRunner struct {
	calls [][]string
	err   error
}

func (f *fakeRunner) Run(ctx context.Context, args ...string) (string, error) {
	cp := append([]string{}, args...)
	f.calls = append(f.calls, cp)
	if f.err != nil {
		return "", f.err
	}
	return "", nil
}

func (f *fakeRunner) RunJSON(ctx context.Context, dest any, args ...string) error {
	_, err := f.Run(ctx, args...)
	return err
}

func TestBuildPlanRequiresProjectAndModel(t *testing.T) {
	_, err := BuildPlan(DeployRequest{Bearer: "rh_x"})
	if err == nil || !strings.Contains(err.Error(), "project") {
		t.Fatalf("want project error, got %v", err)
	}
	_, err = BuildPlan(DeployRequest{Project: "p", Bearer: "rh_x"})
	if err == nil || !strings.Contains(err.Error(), "model") {
		t.Fatalf("want model error, got %v", err)
	}
}

func TestBuildPlanL4Default(t *testing.T) {
	plan, err := BuildPlan(DeployRequest{
		Project: "my-proj",
		ModelID: "TheBloke/TinyLlama-1.1B-Chat-v1.0-GGUF",
		Bearer:  "rh_testtoken",
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Target.Name != GPUTypeL4 {
		t.Fatalf("target=%s", plan.Target.Name)
	}
	joined := strings.Join(plan.CreateArgs, " ")
	if !strings.Contains(joined, "--provisioning-model=SPOT") {
		t.Fatal("expected SPOT")
	}
	if len(plan.CreateArgs) < 3 || plan.CreateArgs[0] != "compute" || plan.CreateArgs[1] != "instances" || plan.CreateArgs[2] != "create" {
		t.Fatalf("expected instances create, got %v", plan.CreateArgs[:min(5, len(plan.CreateArgs))])
	}
	if strings.Contains(joined, "create-with-container") {
		t.Fatal("create-with-container is discontinued")
	}
	if !strings.Contains(joined, "common-cu129-ubuntu-2204-nvidia-580") {
		t.Fatal("expected DLVM image family")
	}
	if !strings.Contains(joined, "no-address") {
		t.Fatal("default must be no-address (Cloud NAT)")
	}
	if !strings.Contains(joined, "g2-standard-4") {
		t.Fatal("expected L4 machine type")
	}
	if strings.Contains(joined, "rh_testtoken") {
		t.Fatal("printable args must not include bearer")
	}
	if !strings.Contains(plan.Dockerfile, PrebuiltLlamaServerCUDA) {
		t.Fatal("plan Dockerfile should wrap official server-cuda")
	}
	if strings.Contains(plan.Dockerfile, "cmake -B") {
		t.Fatal("plan Dockerfile must not compile from source")
	}
	if !strings.Contains(plan.Startup, "docker run") {
		t.Fatal("startup must docker run the prebuilt image")
	}
	if strings.Contains(plan.Startup, "cmake -B") || strings.Contains(plan.Startup, "git clone") {
		t.Fatal("startup must not compile llama on the VM")
	}
	if !strings.HasPrefix(plan.Name, "runhug-") {
		t.Fatalf("name=%s", plan.Name)
	}
}

func TestBuildPlanT4(t *testing.T) {
	plan, err := BuildPlan(DeployRequest{
		Project: "p",
		ModelID: "org/m",
		GPU:     "T4",
		Bearer:  "rh_x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Target.Accelerator != "nvidia-tesla-t4" {
		t.Fatalf("accel=%s", plan.Target.Accelerator)
	}
}

func TestDeployL4ThenT4Fallback(t *testing.T) {
	fr := &fakeRunner{err: nil}
	// First call fails, second succeeds — simulate by custom runner.
	n := 0
	fr.err = nil
	c := &Client{Runner: &seqRunner{failFirst: true}}
	plan, err := BuildPlan(DeployRequest{Project: "p", ModelID: "org/m", Bearer: "rh_x"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Deploy(context.Background(), plan, "rh_x", "", true); err != nil {
		t.Fatal(err)
	}
	if plan.Target.Name != GPUTypeT4 {
		t.Fatalf("after fallback want T4, got %s", plan.Target.Name)
	}
	_ = n
}

type seqRunner struct {
	failFirst bool
	calls     int
}

func (s *seqRunner) Run(ctx context.Context, args ...string) (string, error) {
	s.calls++
	if s.failFirst && s.calls == 1 {
		return "", context.DeadlineExceeded // any error
	}
	return "", nil
}

func (s *seqRunner) RunJSON(ctx context.Context, dest any, args ...string) error {
	_, err := s.Run(ctx, args...)
	return err
}

func TestInstanceName(t *testing.T) {
	got := InstanceName("Org/My Model!")
	if !strings.HasPrefix(got, "runhug-") {
		t.Fatal(got)
	}
	if strings.ContainsAny(got, "/! ") {
		t.Fatal(got)
	}
}

func TestBuildPlanPublicIP(t *testing.T) {
	plan, err := BuildPlan(DeployRequest{
		Project:        "p",
		ModelID:        "org/m",
		Bearer:         "rh_x",
		ContainerImage: "us-docker.pkg.dev/p/runhug/llama-server:cuda",
		PublicIP:       true,
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(plan.CreateArgs, " ")
	if strings.Contains(joined, "no-address") {
		t.Fatal("public-ip dogfood must not set no-address")
	}
}
