# Security policy

## Reporting a vulnerability

Please report security issues privately through GitHub's "Report a vulnerability" feature on the repository's
Security tab, and do not open a public issue. Expect an answer within a few days.

## Handling of credentials

- API keys are stored in the OS keyring, or in `~/.config/oa/hosts.json` with mode 0600 when no keyring is available.
- The API key is only sent to the host it was configured for. Links to other hosts (for example file downloads from an
  object store) are fetched without it.
- `oa` has no command that prints a stored key.
