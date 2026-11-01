# Contributing

Thanks for helping to improve `oa`.

## Setup

You need Go (the version in `go.mod` or newer).

```sh
make test    # go test -race ./...
make vet
make lint    # golangci-lint
make build   # bin/oa
```

## Guidelines

- Commits follow [Conventional Commits](https://www.conventionalcommits.org): `feat(scope): ...`, `fix: ...`,
  `docs: ...`, `test: ...`, `chore: ...`. Write the subject in the imperative, in English.
- Add tests for new behavior. The API client and the commands are tested against `httptest` servers, so no OpenAleph
  instance is needed. Look at `internal/cmd/cmd_test.go` for the harness.
- Keep the API client in `internal/api` free of CLI concerns, and commands free of HTTP details.
- Destructive commands must ask for confirmation (`app.confirm`) and need `--yes` in scripts. Do not add code paths
  that delete or overwrite user files without an explicit flag.
- Behavior that the Python `openaleph` client had (flags, environment variables, the crawl state format) is kept
  compatible. Intentional differences are listed in `docs/migration-from-python.md`; add to that list when you add one.

## Reporting bugs

Please include the `oa --version` output, the command you ran (with secrets removed) and the error. Never paste API
keys into issues.
