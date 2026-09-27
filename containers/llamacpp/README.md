# llama.cpp (GCP Phase 1)

runhug generates `Dockerfile` + `entrypoint.sh` at deploy time for Spot GPU VMs:

```bash
runhug gcp dockerfile --model org/model --out ./containers/llamacpp
# or
runhug gcp deploy org/model --project <id> --write-image ./containers/llamacpp --dry-run
```

Soft locks: no ADC / service-account JSON / Bearer baked into the image. The CLI
injects the Bearer via GCE metadata, binds `llama-server` to `127.0.0.1`, and
uses an IAP tunnel plus stop-on-idle.
