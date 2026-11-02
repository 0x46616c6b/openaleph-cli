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

## Dependencies

Dependabot (`.github/dependabot.yml`) checks Go modules and GitHub Actions every Monday. All updates, major ones
included, come as one grouped pull request per ecosystem. Read the release notes of major updates in the pull request
before merging. New releases wait 7 days before they are proposed (30 days for major and 3 days for patch updates of Go
modules), and security updates are not delayed.

Actions are locked in `.github/workflows/actions.lock`. Dependabot updates the lockfile in the same pull request. If you
change a `uses:` line yourself, run `gh actions-lock` and commit the result.

## Releases and changelog

Release notes are drafted by [release-drafter](https://github.com/release-drafter/release-drafter) from merged pull
requests. The workflow in `.github/workflows/release-drafter.yml` keeps a draft release up to date on every push to
`main`, and `.github/release-drafter.yml` defines the sections and the next version.

- **Use a Conventional Commits title for every pull request** (`feat(api): ...`, `fix: ...`) and squash-merge, so the
  title becomes the commit and the changelog entry. `feat` lists under Features and bumps the minor version, `fix` and
  `perf` bump the patch version, and a breaking change (`feat!:` or a `BREAKING CHANGE` footer) bumps the major
  version. `docs`, `test`, `chore`, `ci`, `build` and `style` are left out of the notes.
- **A wrong title does not lose the change.** The autolabeler (`.github/workflows/autolabeler.yml`) also labels pull
  requests by loose titles such as `Add support for X` or `Fixed crash on Y` and by branch names such as `feature/`,
  `fix/` or `docs/`, and the draft sorts by these labels just like by the title. A pull request that matches nothing
  is still listed with the plain changes. You can always set a label by hand, and `skip-changelog` leaves a pull
  request out. Labels are defined in `.github/labels.txt` and created by the "Sync labels" workflow:

  | Label | Section | Version |
  | --- | --- | --- |
  | `breaking` | Breaking changes | major |
  | `feature` | Features | minor |
  | `bug` | Fixes | patch |
  | `performance` | Performance | patch |
  | `documentation`, `maintenance`, `skip-changelog` | left out | none |

  `documentation` and `maintenance` only hide a pull request when no other label is set, so a feature that also
  touches `.github/` or the docs stays in the notes.
- Commits that are pushed directly to `main` without a pull request do not appear in the draft.
- To release, check the draft on the Releases page and publish it, which creates its tag (for example `v0.2.0`).
  Publishing starts the release workflow, which builds the binaries with GoReleaser and attaches them to the
  published release, keeping its notes. Do not push the tag by hand: the workflow only reacts to a published release.

## Homebrew tap

GoReleaser publishes a Homebrew formula to the tap repository `homebrew-tap` of the repository owner on every release, so
users can run `brew install <owner>/tap/oa`. One-time setup:

1. Create a public repository named `homebrew-tap` (it may be empty, but needs at least one commit, for example a
   README).
2. Create a fine-grained personal access token that is limited to that repository and has the repository permission
   "Contents: Read and write".
3. Add it to this repository as the Actions secret `HOMEBREW_TAP_GITHUB_TOKEN`. The default `GITHUB_TOKEN` cannot push
   to another repository.

Without the secret the release workflow fails at the formula step. The tap uses a formula and not a cask on purpose:
Homebrew puts downloaded casks under macOS quarantine, which kills an unsigned binary, and formulae are not quarantined.
GoReleaser marks `brews` as deprecated, so `goreleaser check` reports it. The release itself is not affected. Switch to
`homebrew_casks` only together with signing and notarizing the binary.

## Reporting bugs

Please include the `oa --version` output, the command you ran (with secrets removed) and the error. Never paste API
keys into issues.
