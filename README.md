# oa

`oa` is a command-line client for [OpenAleph](https://openaleph.org), in the style of `gh`: log in once, then list
collections, search entities, check processing status, upload and download documents and call any API endpoint.
It is a single static binary and replaces the Python `openaleph` client (`openaleph-client`).

```console
$ oa auth login --host https://aleph.example.org
$ oa collection list
ID  FOREIGN ID  LABEL    CATEGORY  ENTITIES
12  leak_a      Leak A   leak      48210
$ oa search "acme corp" --schema Company --collection leak_a
$ oa status
$ oa collection upload leak_a ./documents --parallel 4
```

## Installation

### Homebrew (macOS and Linux)

```sh
brew install 0x46616c6b/tap/oa
```

This installs the binary and the shell completions for bash, zsh and fish.

### Binaries and source

The binaries are not signed or notarized. If macOS refuses to open a binary that you downloaded in the browser, check
the download and remove the quarantine flag:

```sh
xattr -d com.apple.quarantine ./oa
```

Installing with Homebrew is not affected, because Homebrew does not quarantine formulae.

Download a binary from the releases page, or build from source (Go 1.26 or newer):

```sh
go install github.com/0x46616c6b/openaleph-cli/cmd/oa@latest
```

### Shell completion

`oa completion` prints a completion script for `bash`, `zsh`, `fish` and `powershell`. It completes commands,
subcommands and flags.

zsh:

```sh
# once, if completion is not enabled yet
echo "autoload -U compinit; compinit" >> ~/.zshrc

# install the script into a directory on your $fpath
oa completion zsh > "${fpath[1]}/_oa"

# or load it on every start, in ~/.zshrc
source <(oa completion zsh)
```

bash (needs the `bash-completion` package):

```sh
# macOS with Homebrew
oa completion bash > "$(brew --prefix)/etc/bash_completion.d/oa"

# Linux
oa completion bash | sudo tee /etc/bash_completion.d/oa

# or load it on every start, in ~/.bashrc
source <(oa completion bash)
```

fish:

```sh
oa completion fish > ~/.config/fish/completions/oa.fish
```

Restart the shell afterwards. Run `oa completion <shell> --help` for more options.

## Authentication

Create an API key in the OpenAleph web UI (profile settings), then run `oa auth login`. The key is checked against
the server and stored in the OS keyring, or in `~/.config/oa/hosts.json` (mode 0600) when no keyring is available.

For scripts and CI, use environment variables instead. In order of precedence:

| Setting | Variables |
| --- | --- |
| Host | `OA_HOST`, `OPAL_HOST`, `OPENALEPH_HOST`, `MEMORIOUS_ALEPH_HOST` |
| API key | `OA_API_KEY`, `OPAL_API_KEY`, `OPENALEPH_API_KEY`, `MEMORIOUS_ALEPH_API_KEY` |
| Retries | `OPENALEPH_MAX_TRIES` (default 5) |

The `--host`, `--api-key` and `--retries` flags override all of them. The names used by the Python client keep working.

## Commands

Collections are referenced by numeric ID or foreign ID.

| Command | Purpose |
| --- | --- |
| `oa auth login / logout / status` | Manage and verify the saved login |
| `oa status [collection]` | Show collections that are being processed |
| `oa status watch [collection]` | Refresh the processing status until all tasks have finished |
| `oa collection list / view` | Browse collections |
| `oa collection create / edit / delete / flush` | Manage collections (`delete` and `flush` need `--yes` in scripts) |
| `oa collection reindex / reingest` | Re-run indexing or ingest |
| `oa collection upload <collection> <path>` | Upload a directory, resumable, parallel |
| `oa collection download <collection>` | Rebuild a collection as a folder tree |
| `oa search <query>` | Search entities |
| `oa entity view / delete / download` | Work with single entities |
| `oa entity stream / write / write-many` | Export and import entities as JSON lines |
| `oa entityset list / items / create / delete` | Work with lists and other entity sets |
| `oa api <path>` | Call any API endpoint with authentication |

List-style commands print tables on a terminal. Add `--json fields` (or `--json all`) for JSON and `--jq expression`
to filter it:

```sh
oa collection list --json id,label
oa search "acme" --jq '.[].id'
oa api collections -f limit=5 --jq '.results[].label'
```

## Migrating from the Python client

All commands of the Python `openaleph` client still work under their old names and flags (`oa crawldir -f ...`,
`oa write-entities -f ...`, `oa stream-entities -f ...`), hidden from `--help`. See
[docs/migration-from-python.md](docs/migration-from-python.md) for the mapping and the few intentional differences.

## Development

```sh
make test     # go test -race ./...
make build    # bin/oa
make lint     # golangci-lint
```

## License

MIT, see [LICENSE](LICENSE).
