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

## Releases and changelog

Release notes are drafted by [release-drafter](https://github.com/release-drafter/release-drafter) from merged pull
requests. The workflow in `.github/workflows/release-drafter.yml` keeps a draft release up to date on every push to
`main`, and `.github/release-drafter.yml` defines the sections and the next version.

- **Use a Conventional Commits title for every pull request** (`feat(api): ...`, `fix: ...`) and squash-merge, so the
  title becomes the commit and the changelog entry. `feat` lists under Features and bumps the minor version, `fix` and
  `perf` bump the patch version, and a breaking change (`feat!:` or a `BREAKING CHANGE` footer) bumps the major
  version. `docs`, `test`, `chore`, `ci`, `build` and `style` are left out of the notes.
- Commits that are pushed directly to `main` without a pull request do not appear in the draft.
- To release, check the draft on the Releases page, then push the tag it names (for example `v0.2.0`). The release
  workflow builds the binaries with GoReleaser and attaches them to that draft, keeping its notes. If the tag does not
  match the draft, GoReleaser creates a new release without these notes.

## Reporting bugs

Please include the `oa --version` output, the command you ran (with secrets removed) and the error. Never paste API
keys into issues.
