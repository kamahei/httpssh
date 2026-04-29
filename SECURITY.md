# Security Policy

`httpssh` exposes a PowerShell session over HTTP/WebSocket. Treat any
deployment as security-sensitive.

## Supported Versions

The project is pre-1.0. Only the latest commit on `main` and the latest
published release are considered supported.

## Reporting A Vulnerability

For vulnerabilities that could expose a host, bearer token, Cloudflare
credential, shell session, or private network, use GitHub's private
vulnerability reporting feature if it is enabled for the repository.

Do not paste real bearer tokens, Cloudflare Service Token values, private
hostnames, tunnel IDs, screenshots containing secrets, or live logs with
credentials into public issues.

For non-sensitive bugs, open a normal GitHub issue with reproduction steps.
