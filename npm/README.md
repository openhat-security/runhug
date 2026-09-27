# runhug (npm)

npm wrapper for the [runhug](https://github.com/openhat-security/runhug) CLI.
`postinstall` downloads the matching GitHub Release binary for your OS/arch.

```bash
npm install -g runhug
# or
npx runhug wizard
```

Requires Node ≥ 18. Supported: macOS/Linux/Windows × amd64/arm64.

If download fails (offline, unsupported platform), use the shell installer:

```bash
curl -fsSL https://raw.githubusercontent.com/openhat-security/runhug/main/scripts/install.sh | bash
```
