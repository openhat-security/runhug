# llama.cpp (GCP Phase 1)

runhug generates a **thin wrap** of `ghcr.io/ggml-org/llama.cpp:server-cuda`
(Dockerfile + entrypoint) for Spot GPU VMs — cold start = pull + start, no
first-boot compile, no Cloud Build.

GCP discontinued `create-with-container`; deploy uses a Spot Deep Learning VM
startup script that `docker pull`s + `docker run`s this image (`--gpus all`,
`--network host`, bind `127.0.0.1`).

```bash
# Local build + push to Artifact Registry (linux/amd64)
runhug gcp push --image REGION-docker.pkg.dev/PROJECT/runhug/llama-server:cuda

# Or write files and build yourself:
runhug gcp dockerfile --model org/model --out ./containers/llamacpp
docker build --platform linux/amd64 -t REGION-docker.pkg.dev/PROJECT/runhug/llama-server:cuda .
docker push REGION-docker.pkg.dev/PROJECT/runhug/llama-server:cuda

# Then deploy
runhug gcp deploy org/model --project PROJECT --image REGION-docker.pkg.dev/PROJECT/runhug/llama-server:cuda --dry-run
```

Soft locks: no ADC / service-account JSON / Bearer baked into the image. The CLI
injects the Bearer via GCE metadata, binds `llama-server` to `127.0.0.1`, and
uses an SSH local-forward tunnel plus stop-on-idle.
