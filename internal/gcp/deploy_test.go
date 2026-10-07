package gcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/adamsiwiec1/runhug/internal/hparams"
)

type fakeRunner struct {
	calls       [][]string
	err         error
	stdout      string
	jsonPayload string
}

func (f *fakeRunner) Run(ctx context.Context, args ...string) (string, error) {
	cp := append([]string{}, args...)
	f.calls = append(f.calls, cp)
	if f.err != nil {
		return "", f.err
	}
	return f.stdout, nil
}

func (f *fakeRunner) RunJSON(ctx context.Context, dest any, args ...string) error {
	_, err := f.Run(ctx, args...)
	if err != nil {
		return err
	}
	if f.jsonPayload != "" && dest != nil {
		return json.Unmarshal([]byte(f.jsonPayload), dest)
	}
	return nil
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
	if !strings.Contains(plan.OpenAIHint, "18080") || plan.TunnelHint != "runhug run" {
		t.Fatalf("hints openai=%q tunnel=%q", plan.OpenAIHint, plan.TunnelHint)
	}
}

func TestWithSamplingEntrypoint(t *testing.T) {
	temp := 0.55
	topP := 0.9
	cfg := ImageConfig{ModelID: "org/m"}.WithSampling(&hparams.Sampling{
		Temperature: &temp,
		TopP:        &topP,
	})
	ep := EntrypointScript(cfg)
	if !strings.Contains(ep, `--temp "$TEMP"`) {
		t.Fatal("entrypoint should pass --temp")
	}
	st := StartupScript(cfg)
	if !strings.Contains(st, "runhug-temp") {
		t.Fatal("startup should forward runhug-temp metadata")
	}
	plan, err := BuildPlan(DeployRequest{Project: "p", ModelID: "org/m", Bearer: "rh_x"})
	if err != nil {
		t.Fatal(err)
	}
	plan.Image = cfg
	joined := strings.Join(plan.CreateArgs, " ")
	_ = joined
	meta := liveCreateArgs(plan, &DeployBundle{StartupPath: "/tmp/s", TokenPath: "/tmp/t"})
	all := strings.Join(meta, " ")
	if !strings.Contains(all, "runhug-temp=0.55") {
		t.Fatalf("live args missing temp: %s", all)
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

func TestEnablePublicIPRewritesCreateArgs(t *testing.T) {
	plan, err := BuildPlan(DeployRequest{
		Project:        "p",
		ModelID:        "org/m",
		Bearer:         "rh_x",
		ContainerImage: "us-docker.pkg.dev/p/runhug/llama-server:cuda",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(plan.CreateArgs, " "), "no-address") {
		t.Fatal("expected no-address default")
	}
	plan.EnablePublicIP()
	if strings.Contains(strings.Join(plan.CreateArgs, " "), "no-address") {
		t.Fatal("EnablePublicIP must drop no-address")
	}
}
