package gcp

import (
	"context"
	"fmt"
	"strings"
)

// ParseARDockerImage splits REGION-docker.pkg.dev/PROJECT/REPO/image:tag.
func ParseARDockerImage(ref string) (region, project, repo string, ok bool) {
	ref = strings.TrimSpace(ref)
	host, rest, found := strings.Cut(ref, "/")
	if !found || rest == "" {
		return "", "", "", false
	}
	if !strings.HasSuffix(host, "-docker.pkg.dev") {
		return "", "", "", false
	}
	region = strings.TrimSuffix(host, "-docker.pkg.dev")
	parts := strings.Split(rest, "/")
	if len(parts) < 2 || region == "" || parts[0] == "" || parts[1] == "" {
		return "", "", "", false
	}
	return region, parts[0], parts[1], true
}

// CanAutoPush is true for Artifact Registry tags we can docker build+push.
func CanAutoPush(ref string) bool {
	_, _, _, ok := ParseARDockerImage(ref)
	return ok
}

// ArtifactImageExists reports whether the tag is already in Artifact Registry.
func (c *Client) ArtifactImageExists(ctx context.Context, image string) (bool, error) {
	image = strings.TrimSpace(image)
	if image == "" {
		return false, nil
	}
	var dummy map[string]any
	err := c.runner().RunJSON(ctx, &dummy,
		"artifacts", "docker", "images", "describe", image,
	)
	if err != nil {
		return false, nil
	}
	return true, nil
}

// EnsureDockerRepo creates the Artifact Registry docker repo if missing.
func (c *Client) EnsureDockerRepo(ctx context.Context, image string) error {
	region, project, repo, ok := ParseARDockerImage(image)
	if !ok {
		return nil // GCR / other; docker push will fail with a clear error
	}
	_, err := c.runner().Run(ctx,
		"artifacts", "repositories", "describe", repo,
		"--location="+region,
		"--project="+project,
	)
	if err == nil {
		return nil
	}
	_, err = c.runner().Run(ctx,
		"artifacts", "repositories", "create", repo,
		"--repository-format=docker",
		"--location="+region,
		"--project="+project,
		"--quiet",
	)
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "already exists") {
		return fmt.Errorf("create Artifact Registry repo %s: %w", repo, err)
	}
	return nil
}

// ConfigureDockerAuth runs gcloud auth configure-docker for the image's AR host.
func (c *Client) ConfigureDockerAuth(ctx context.Context, image string) error {
	region, _, _, ok := ParseARDockerImage(image)
	if !ok {
		return nil
	}
	host := region + "-docker.pkg.dev"
	_, err := c.runner().Run(ctx, "auth", "configure-docker", host, "--quiet")
	if err != nil {
		return fmt.Errorf("gcloud auth configure-docker %s: %w", host, err)
	}
	return nil
}
