# Security

## Reporting a vulnerability

Please do not open a public issue for security problems. Use
[GitHub's private vulnerability reporting](https://github.com/philippdormann/umago/security/advisories/new)
on this repository. You will get an acknowledgement within a few days and a
fix or mitigation as soon as one is ready.

Only the latest release is supported.

## Hardening notes

- Set `UMAGO_ADMIN_USER` and `UMAGO_ADMIN_PASSWORD`. Without them the
  dashboard and admin API are open; only run that way behind your own proxy or
  VPN.
- Put Umago behind an HTTPS proxy. It reads the client IP from
  `X-Forwarded-For`, so make sure the proxy overwrites that header rather than
  appending to it.
- API tokens are stored hashed and are read-only. Revoke any you no longer
  use in Settings.
- Favicon fetches go through a guard that refuses loopback, private,
  link-local and carrier-grade NAT addresses, including after redirects.
- Google and Polar credentials live in the database. Protect the `data`
  directory (SQLite) or your Postgres instance as you would any secret.
