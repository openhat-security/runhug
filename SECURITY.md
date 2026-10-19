# Security policy

## Supported versions

Only the latest tagged release receives security fixes. Until the project
publishes a stable release, treat every build as pre-1.0.

## Network exposure (local bridge and proxy)

`runhug start <agent>` bridges and `runhug proxy` are **loopback-only by default**:

- They bind `127.0.0.1` unless you pass a host (`start --bridge-host`, `proxy --host` or `--addr`).
- Connections from non-loopback peers are refused (HTTP 403) unless you opt in.
- Binding any non-loopback host (including `0.0.0.0` or `:PORT`) requires **both**
  `--allow-remote` (or `RUNHUG_ALLOW_REMOTE=1`) **and** a strong API key of at least
  32 characters in `RUNHUG_API_KEY`, or `--generate-key` to create one. Otherwise runhug
  exits non-zero before listening; it never starts an open, unauthenticated listener.
- Clients send the key only as an `Authorization: Bearer <key>` header (or `x-api-key`).
  Query parameters are never accepted, so keys don't end up in URLs, logs or shell history.
- `--generate-key` writes the key to `<user config dir>/runhug/api-key` (mode 0600) and
  prints only that path. runhug never prints the API key in startup or debug output.
- Every future `start <agent>` target uses the same rules (`internal/netguard`).

```sh
runhug start claude                                              # 127.0.0.1 only
RUNHUG_ALLOW_REMOTE=1 runhug start claude --bridge-host 0.0.0.0 --generate-key
```

The design is adapted from Unsloth's exposure rules (behavior only; no code copied).

## Reporting a vulnerability

Do not open a public issue, discussion, or pull request for vulnerabilities.

Use this repository's private **Security → Report a vulnerability** form
(GitHub Security Advisories) and include:

- the affected version and operating system;
- reproduction steps and expected impact;
- relevant logs with secrets, tokens, addresses, and identifiers removed;
- whether coordinated disclosure requires an embargo.

We aim to acknowledge reports within 72 hours, provide an initial assessment
within 7 days, and coordinate publication after a fix is available.

See [GitHub's private vulnerability reporting](https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing-information-about-vulnerabilities/privately-reporting-a-security-vulnerability).
