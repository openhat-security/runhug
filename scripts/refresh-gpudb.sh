#!/usr/bin/env bash
# Refresh vendored GPU catalogs from Jr23xd23/gpu-database (Apache-2.0).
# Source: TechPowerUp via dbgpu / RightNow. Do not use resolve-cache URLs.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
trim_vendor() {
  local url="$1" out="$2" vendor="$3"
  local tmp
  tmp="$(mktemp)"
  echo "fetching $url"
  curl -fsSL "$url" -o "$tmp"
  python3 - "$tmp" "$out" "$vendor" <<'PY'
import json, sys
src, dest, vendor = sys.argv[1], sys.argv[2], sys.argv[3]
data = json.load(open(src))
out = []
for g in data:
    mem = g.get("memorySize") or 0
    if mem < 8:
        continue
    name = g.get("name") or ""
    if vendor == "nvidia":
        try:
            cuda = float(g["cuda"]) if g.get("cuda") is not None else 0.0
        except (TypeError, ValueError):
            cuda = 0.0
        if cuda < 7.5:
            continue
    else:
        low = name.lower()
        keep = bool(g.get("fp16") or g.get("fp32"))
        if any(p in low for p in ("instinct", "radeon rx", "radeon pro", "radeon vii", "firepro")):
            keep = True
        if not keep:
            continue
    row = {
        "name": name,
        "vendor": vendor,
        "architecture": g.get("architecture"),
        "generation": g.get("generation"),
        "memorySize": mem,
        "memoryBandwidth": g.get("memoryBandwidth"),
        "memoryType": g.get("memoryType"),
        "fp16": g.get("fp16"),
        "fp32": g.get("fp32"),
        "fp64": g.get("fp64"),
        "tensorCores": g.get("tensorCores"),
        "cuda": str(g["cuda"]) if g.get("cuda") is not None else "",
        "shaders": g.get("shaders"),
        "tdp": g.get("tdp"),
    }
    row = {k: v for k, v in row.items() if v is not None and v != ""}
    out.append(row)
out.sort(key=lambda r: (-(r.get("fp16") or 0), -(r.get("memorySize") or 0), r.get("name") or ""))
with open(dest, "w") as f:
    json.dump(out, f, separators=(",", ":"))
    f.write("\n")
print(f"wrote {len(out)} {vendor} rows -> {dest}")
PY
  rm -f "$tmp"
}

trim_vendor \
  "https://huggingface.co/datasets/Jr23xd23/gpu-database/resolve/main/data/nvidia/all.json" \
  "$ROOT/internal/gpudb/data/nvidia.json" \
  nvidia

trim_vendor \
  "https://huggingface.co/datasets/Jr23xd23/gpu-database/resolve/main/data/amd/all.json" \
  "$ROOT/internal/gpudb/data/amd.json" \
  amd

echo "GCP catalog: edit internal/gpudb/data/gcp.json (or runhug gpu update with gcloud auth)"
