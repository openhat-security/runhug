#!/usr/bin/env bash
# Generate winget manifests under packaging/winget/<version>/.
# Usage: generate.sh <version> <windows-amd64-exe-url> [sha256]
set -euo pipefail

VER="${1:?version}"
URL="${2:?url}"
SHA="${3:-}"
ROOT="$(cd "$(dirname "$0")" && pwd)"
OUT="$ROOT/$VER"
PKG="OpenHatSecurity.Runhug"
mkdir -p "$OUT"

if [ -z "$SHA" ]; then
  tmp="$(mktemp)"
  curl -fsSL "$URL" -o "$tmp"
  SHA="$(shasum -a 256 "$tmp" | awk '{print $1}')"
  rm -f "$tmp"
fi
SHA="$(echo "$SHA" | tr '[:lower:]' '[:upper:]')"

cat > "$OUT/${PKG}.yaml" <<EOF
# yaml-language-server: \$schema=https://aka.ms/winget-manifest.version.1.6.0.schema.json
PackageIdentifier: ${PKG}
PackageVersion: ${VER}
DefaultLocale: en-US
ManifestType: version
ManifestVersion: 1.6.0
EOF

cat > "$OUT/${PKG}.locale.en-US.yaml" <<EOF
# yaml-language-server: \$schema=https://aka.ms/winget-manifest.defaultLocale.1.6.0.schema.json
PackageIdentifier: ${PKG}
PackageVersion: ${VER}
PackageLocale: en-US
Publisher: OpenHat Security
PublisherUrl: https://github.com/openhat-security
PublisherSupportUrl: https://github.com/openhat-security/runhug/issues
Author: OpenHat Security
PackageName: runhug
PackageUrl: https://github.com/openhat-security/runhug
License: MIT
LicenseUrl: https://github.com/openhat-security/runhug/blob/main/LICENSE
ShortDescription: Find the best Hugging Face model. Deploy it in minutes. Run it for pennies.
Moniker: runhug
Tags:
  - cli
  - llm
  - runpod
  - gcp
  - huggingface
ManifestType: defaultLocale
ManifestVersion: 1.6.0
EOF

cat > "$OUT/${PKG}.installer.yaml" <<EOF
# yaml-language-server: \$schema=https://aka.ms/winget-manifest.installer.1.6.0.schema.json
PackageIdentifier: ${PKG}
PackageVersion: ${VER}
InstallerLocale: en-US
InstallerType: zip
NestedInstallerType: portable
NestedInstallerFiles:
  - RelativeFilePath: runhug.exe
    PortableCommandAlias: runhug
Commands:
  - runhug
Installers:
  - Architecture: x64
    InstallerUrl: ${URL}
    InstallerSha256: ${SHA}
    UpgradeBehavior: install
ManifestType: installer
ManifestVersion: 1.6.0
EOF

echo "Wrote winget manifests → $OUT"
