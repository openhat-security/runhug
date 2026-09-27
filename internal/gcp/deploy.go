package gcp

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// DeployRequest is a use-time Spot GPU deploy (no hardcoded project/model).
type DeployRequest struct {
	Project     string
	Zone        string
	Region      string
	Name        string
	ModelID     string
	GGUFFile    string
	GPU         string // L4 | T4 | empty=L4 then caller may retry T4
	IdleSeconds int
	DiskGB      int
	HFToken     string // optional; metadata-from-file only
	Bearer      string // CLI-managed; metadata-from-file only
	DryRun      bool
	// ContainerImage is the prebuilt llama-server image (Artifact Registry / GCR).
	// Required for live create; dry-run may use a printable placeholder.
	ContainerImage string
	// PublicIP adds an ephemeral external IP (dogfood egress). Default false =
	// no-address (needs Cloud NAT for image pull / HF download).
	PublicIP bool
}

// DeployPlan is the printable / executable plan.
type DeployPlan struct {
	Project        string      `json:"project"`
	Zone           string      `json:"zone"`
	Region         string      `json:"region"`
	Name           string      `json:"name"`
	ModelID        string      `json:"model_id"`
	GGUFFile       string      `json:"gguf_file,omitempty"`
	Target         GPUTarget   `json:"target"`
	IdleSeconds    int         `json:"idle_seconds"`
	Image          ImageConfig `json:"image"`
	ContainerImage string      `json:"container_image"`
	PublicIP       bool        `json:"public_ip"`
	Dockerfile     string      `json:"-"`
	Entrypoint     string      `json:"-"`
	Startup        string      `json:"-"` // deprecated stub; create-with-container owns boot
	// CreateArgs is safe to print (secrets redacted / placeholders).
	CreateArgs   []string `json:"create_args"`
	FirewallArgs []string `json:"firewall_args,omitempty"`
	OpenAIHint   string   `json:"openai_hint"`
	TunnelHint   string   `json:"tunnel_hint"`
}

// BuildPlan validates and materializes Dockerfile/startup + printable gcloud args.
func BuildPlan(req DeployRequest) (*DeployPlan, error) {
	if err := EnsureProject(req.Project); err != nil {
		return nil, err
	}
	model := strings.TrimSpace(req.ModelID)
	if model == "" {
		return nil, fmt.Errorf("HF/GGUF model required — pass a repo id (runhug never hardcodes a model)")
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = InstanceName(model)
	}
	region, zone := ZoneFromRegion(req.Region, req.Zone)
	target, err := PickTarget(req.GPU)
	if err != nil {
		return nil, err
	}
	if req.DiskGB > 0 {
		target.DiskGB = req.DiskGB
	}
	idle := req.IdleSeconds
	if idle <= 0 {
		idle = 600
	}
	img := ImageConfig{
		ModelID:     model,
		GGUFFile:    strings.TrimSpace(req.GGUFFile),
		Port:        ServerPort,
		IdleSeconds: idle,
	}
	if strings.TrimSpace(req.Bearer) == "" {
		return nil, fmt.Errorf("internal: Bearer required before BuildPlan (CLI GenerateBearer)")
	}

	containerImage := strings.TrimSpace(req.ContainerImage)
	if containerImage == "" {
		// Printable placeholder for dry-run; live Deploy rejects empty.
		containerImage = fmt.Sprintf("%s-docker.pkg.dev/%s/runhug/llama-server:cuda12.4", DefaultRegion, req.Project)
	}

	plan := &DeployPlan{
		Project:        req.Project,
		Zone:           zone,
		Region:         region,
		Name:           name,
		ModelID:        model,
		GGUFFile:       img.GGUFFile,
		Target:         target,
		IdleSeconds:    idle,
		Image:          img,
		ContainerImage: containerImage,
		PublicIP:       req.PublicIP,
		Dockerfile:     Dockerfile(img),
		Entrypoint:     EntrypointScript(img),
		Startup:        StartupScript(img),
		OpenAIHint:     fmt.Sprintf("http://127.0.0.1:%d/v1 (after IAP tunnel)", ServerPort),
		TunnelHint:     fmt.Sprintf("runhug gcp tunnel %s --project %s --zone %s", name, req.Project, zone),
	}
	plan.CreateArgs = buildCreateArgsPrintable(plan)
	plan.FirewallArgs = buildFirewallArgs(plan.Project)
	return plan, nil
}

func buildCreateArgsPrintable(plan *DeployPlan) []string {
	args := baseCreateArgs(plan)
	meta := "runhug-model-id=" + plan.ModelID + ",runhug-idle-seconds=" + fmt.Sprintf("%d", plan.IdleSeconds) + ",install-nvidia-driver=True"
	if plan.GGUFFile != "" {
		meta += ",runhug-gguf-file=" + plan.GGUFFile
	}
	args = append(args,
		"--metadata="+meta,
		"--metadata-from-file=runhug-api-key=<tmp-token>[,runhug-hf-token=<tmp>]",
	)
	return args
}

// baseCreateArgs builds gcloud create-with-container (COS + prebuilt image).
// Soft default: no public IP (Cloud NAT required). Dogfood may set PublicIP.
func baseCreateArgs(plan *DeployPlan) []string {
	env := fmt.Sprintf(
		"HOST=127.0.0.1,PORT=%d,IDLE_SECONDS=%d,CONTEXT=%d,NGL=-1,MODEL_ID=%s",
		ServerPort, plan.IdleSeconds, plan.Image.withDefaults().Context, plan.ModelID,
	)
	if plan.GGUFFile != "" {
		env += ",GGUF_FILE=" + plan.GGUFFile
	}
	args := []string{
		"compute", "instances", "create-with-container", plan.Name,
		"--project=" + plan.Project,
		"--zone=" + plan.Zone,
		"--machine-type=" + plan.Target.MachineType,
		"--provisioning-model=SPOT",
		"--instance-termination-action=STOP",
		"--maintenance-policy=TERMINATE",
		fmt.Sprintf("--boot-disk-size=%dGB", plan.Target.DiskGB),
		"--image-family=cos-stable",
		"--image-project=cos-cloud",
		"--scopes=cloud-platform",
		"--labels=runhug=1,runhug-provider=gcp",
		"--container-image=" + plan.ContainerImage,
		"--container-restart-policy=never",
		"--container-network=host",
		"--container-env=" + env,
	}
	if !plan.PublicIP {
		args = append(args, "--network-interface=no-address")
	}
	if plan.Target.Accelerator != "" {
		args = append(args,
			fmt.Sprintf("--accelerator=type=%s,count=%d", plan.Target.Accelerator, plan.Target.AcceleratorCnt),
		)
	}
	return args
}

func buildFirewallArgs(project string) []string {
	return []string{
		"compute", "firewall-rules", "create", "runhug-allow-iap",
		"--project=" + project,
		"--direction=INGRESS",
		"--priority=1000",
		"--network=default",
		"--action=ALLOW",
		"--rules=tcp:8080,tcp:22",
		"--source-ranges=35.235.240.0/20",
		"--description=runhug IAP TCP forwarding to llama-server",
	}
}

// DeployBundle holds temp files used for a live create.
type DeployBundle struct {
	StartupPath string
	TokenPath   string
	HFPath      string // optional
	Cleanup     func()
}

// Materialize writes startup + secret metadata files for gcloud.
func Materialize(plan *DeployPlan, bearer, hfToken string) (*DeployBundle, error) {
	// create-with-container: no startup-script; Bearer/HF via instance metadata only.
	tokenPath, err := WriteTempFile("runhug-token-*.txt", []byte(bearer))
	if err != nil {
		return nil, err
	}
	b := &DeployBundle{
		TokenPath: tokenPath,
	}
	if strings.TrimSpace(hfToken) != "" {
		hfPath, err := WriteTempFile("runhug-hf-*.txt", []byte(hfToken))
		if err != nil {
			_ = os.Remove(tokenPath)
			return nil, err
		}
		b.HFPath = hfPath
	}
	b.Cleanup = func() {
		_ = os.Remove(b.TokenPath)
		if b.HFPath != "" {
			_ = os.Remove(b.HFPath)
		}
	}
	return b, nil
}

func liveCreateArgs(plan *DeployPlan, bundle *DeployBundle) []string {
	args := baseCreateArgs(plan)
	meta := "runhug-model-id=" + plan.ModelID + ",runhug-idle-seconds=" + fmt.Sprintf("%d", plan.IdleSeconds) + ",install-nvidia-driver=True"
	if plan.GGUFFile != "" {
		meta += ",runhug-gguf-file=" + plan.GGUFFile
	}
	fromFile := "runhug-api-key=" + bundle.TokenPath
	if bundle.HFPath != "" {
		fromFile += ",runhug-hf-token=" + bundle.HFPath
	}
	args = append(args, "--metadata="+meta, "--metadata-from-file="+fromFile)
	return args
}

// Deploy creates the Spot VM (L4, optional T4 fallback).
func (c *Client) Deploy(ctx context.Context, plan *DeployPlan, bearer, hfToken string, tryFallback bool) error {
	if plan == nil {
		return fmt.Errorf("nil plan")
	}
	if strings.TrimSpace(plan.ContainerImage) == "" {
		return fmt.Errorf("container image required: pass --image (Artifact Registry) or build with: runhug gcp dockerfile --write-image ./img && docker build/push")
	}
	bundle, err := Materialize(plan, bearer, hfToken)
	if err != nil {
		return err
	}
	defer bundle.Cleanup()

	args := liveCreateArgs(plan, bundle)
	_, err = c.runner().Run(ctx, args...)
	if err == nil {
		return nil
	}
	if !tryFallback || plan.Target.Name != GPUTypeL4 {
		return err
	}
	t4, _ := PickTarget(GPUTypeT4)
	plan.Target = t4
	args = liveCreateArgs(plan, bundle)
	_, err2 := c.runner().Run(ctx, args...)
	if err2 != nil {
		return fmt.Errorf("L4 failed (%v); T4 fallback failed: %w", err, err2)
	}
	return nil
}

// EnsureIAPFirewall creates the IAP allow rule when missing (best-effort).
func (c *Client) EnsureIAPFirewall(ctx context.Context, project string) error {
	args := buildFirewallArgs(project)
	_, err := c.runner().Run(ctx, args...)
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "already exists") || strings.Contains(msg, "duplicate") {
		return nil
	}
	return err
}

// StopInstance stops a VM (preserves disk; Spot STOP termination action).
func (c *Client) StopInstance(ctx context.Context, project, zone, name string) error {
	_, err := c.runner().Run(ctx,
		"compute", "instances", "stop", name,
		"--project="+project,
		"--zone="+zone,
	)
	return err
}

// DeleteInstance deletes a VM.
func (c *Client) DeleteInstance(ctx context.Context, project, zone, name string) error {
	_, err := c.runner().Run(ctx,
		"compute", "instances", "delete", name,
		"--project="+project,
		"--zone="+zone,
		"--quiet",
	)
	return err
}

// InstanceStatus is a trimmed describe payload.
type InstanceStatus struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Zone   string `json:"zone"`
}

// DescribeInstance returns instance status.
func (c *Client) DescribeInstance(ctx context.Context, project, zone, name string) (*InstanceStatus, error) {
	var st InstanceStatus
	err := c.runner().RunJSON(ctx, &st,
		"compute", "instances", "describe", name,
		"--project="+project,
		"--zone="+zone,
		"--format=json(name,status,zone)",
	)
	if err != nil {
		return nil, err
	}
	return &st, nil
}

// InstanceName builds a GCE-legal name from a model id.
func InstanceName(modelID string) string {
	s := strings.ToLower(modelID)
	s = strings.ReplaceAll(s, "/", "-")
	var b strings.Builder
	b.WriteString("runhug-")
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	for strings.Contains(out, "--") {
		out = strings.ReplaceAll(out, "--", "-")
	}
	if len(out) > 63 {
		out = out[:63]
		out = strings.Trim(out, "-")
	}
	if out == "runhug" || out == "" {
		out = "runhug-gguf"
	}
	return out
}
