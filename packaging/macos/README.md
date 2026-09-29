# macOS codesign + notarization

Tracks [openhat-security/runhug#41](https://github.com/openhat-security/runhug/issues/41).

Until OpenHat has an **Apple Developer Program** membership, darwin release
binaries stay unsigned. Gatekeeper will warn on fresh downloads; that is
expected.

## Current workaround

**Homebrew (your case):**

```bash
xattr -dr com.apple.quarantine /opt/homebrew/Caskroom/runhug
# Intel Homebrew prefix:
# xattr -dr com.apple.quarantine /usr/local/Caskroom/runhug
```

**Manual / curl install:**

```bash
xattr -d com.apple.quarantine "$(command -v runhug)"
```

`scripts/install.sh` and the npm postinstall strip quarantine after download.
Homebrew casks get a GoReleaser `post` install hook that runs the same `xattr -dr`
(next release after that hook lands in the tap).

Prefer Homebrew when possible:

```bash
brew install --cask openhat-security/tap/runhug
```

## Enable when we have the license

1. Enroll: [Apple Developer Program](https://developer.apple.com/programs/).
2. Certificates, Identifiers & Profiles → create **Developer ID Application**.
3. Export the cert as a password-protected `.p12`.
4. App Store Connect → Users and Access → Integrations → **Team Keys** → create
   an API key with access to notarization (`.p8`). Note **Issuer ID** and **Key ID**.
5. Base64 the files (no newlines) and set repo secrets on `openhat-security/runhug`:

| Secret | Value |
|--------|--------|
| `MACOS_SIGN_P12` | `base64 -i DeveloperID.p12 \| tr -d '\n'` |
| `MACOS_SIGN_PASSWORD` | password used when exporting the `.p12` |
| `MACOS_NOTARY_KEY` | `base64 -i AuthKey_XXXXXX.p8 \| tr -d '\n'` |
| `MACOS_NOTARY_KEY_ID` | Key ID from App Store Connect |
| `MACOS_NOTARY_ISSUER_ID` | Issuer UUID from App Store Connect |

6. Tag a release. GoReleaser’s `notarize.macos` block is already wired and
   **auto-enables** when `MACOS_SIGN_P12` is set (quill; runs on `ubuntu-latest`).
7. Verify on a clean Mac: download the darwin asset from the release page and
   open it — Gatekeeper should not show the malware dialog.

## Notes

- Quill-based notarize is configured in `.goreleaser.yaml` (no macOS runner).
- If notarization ever needs native `codesign`/`notarytool` (e.g. DMG / app
  bundle), move the release job to `macos-latest` and follow
  [GoReleaser native notarize](https://goreleaser.com/customization/sign/notarize/).
- Never commit `.p12` / `.p8` files or passwords.
