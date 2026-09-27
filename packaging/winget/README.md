# winget

On each `v*` tag, `.github/workflows/release.yml` runs [`generate.sh`](generate.sh)
and (when `WINGET_PAT` is set) tries to submit via [Komac](https://github.com/russellbanks/Komac).

Package id: **`OpenHatSecurity.Runhug`**

Manual PR path:

```bash
bash packaging/winget/generate.sh 0.1.8 \
  https://github.com/openhat-security/runhug/releases/download/v0.1.8/runhug_0.1.8_windows_amd64.zip
# Copy packaging/winget/<ver>/* into a fork of microsoft/winget-pkgs under
# manifests/o/OpenHatSecurity/Runhug/<ver>/
```
