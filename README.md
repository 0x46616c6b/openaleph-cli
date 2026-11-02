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
| `oa browse` | Browse collections and entities in an interactive terminal UI |
| `oa entity view / delete / download` | Work with single entities |
| `oa entity stream / write / write-many` | Export and import entities as JSON lines |
| `oa entity map <mapping.yml>` | Create entities from CSV files with an `ftm map` mapping, printed as JSON lines |
| `oa entityset list / items / create / delete` | Work with lists and other entity sets |
| `oa mapping list / view / create / update / delete` | Manage entity mappings stored on the server (`delete` needs `--yes` in scripts) |
| `oa mapping trigger / flush` | Generate entities from a mapping, or delete what it generated |
| `oa api <path>` | Call any API endpoint with authentication |

`oa entity map` reads a [FollowTheMoney mapping](https://followthemoney.tech/docs/mappings/) with CSV sources and
prints the entities it creates, so `oa entity map people.yml | oa entity write-many my_leak` loads them. SQL sources
are not supported, and the generated entity IDs are stable but not guaranteed to match those of `ftm map`.

List-style commands print tables on a terminal. Add `--json fields` (or `--json all`) for JSON and `--jq expression`
to filter it:

```sh
oa collection list --json id,label
oa search "acme" --jq '.[].id'
oa api collections -f limit=5 --jq '.results[].label'
```

### Interactive browser

`oa browse` opens a full-screen browser for collections and entities. Like the web UI, it lists datasets and
investigations (collections of the category `casefile`) separately; `tab` switches between them. Start it with
`--collection leak_a` to go
straight to a collection, with `--search "acme"` to start with an entity search (both together search in the
collection), `--schema Person,Address` to filter by schema, or with `--status` to start in the processing status. Rows load page by page as you scroll.

| Key | Action |
| --- | --- |
| `enter`, `l` | Open the selected collection, entity, linked entity or value |
| `esc`, `h` | Go back |
| `tab` | Switch between datasets and investigations; in a collection, between its entities and its documents |
| `/` | Search the current list (`enter` runs it, `esc` cancels) |
| `s` | Search entities in all collections |
| `j`/`k`, `g`/`G`, `ctrl-d`/`ctrl-u` | Move, jump to top or bottom, half page |
| `y` | Copy the ID (or the shown text) to the clipboard, also over SSH |
| `o` | Open in the web browser |
| `J` | Show the raw JSON of an entity |
| `d` | Download a document, or a folder with everything in it, in the background |
| `f` | Filter an entity list by schema: type to narrow, `space` selects several, `enter` applies |
| `F` | Clear the schema filter |
| `p` | Show the processing status, refreshed every 3 seconds |
| `a` | Menu with the collection actions below (in the datasets and investigations lists) |
| `n` | Create a dataset or an investigation, depending on the list |
| `e` | Edit the selected collection |
| `D` | Delete the selected collection (type its foreign ID to confirm) |
| `i` | Re-index the selected collection, optionally flushing the index first |
| `I` | Re-ingest the selected collection, optionally indexing while processing |
| `r` | Reload |
| `?` | Show all keys |
| `ctrl-z` | Suspend to the shell (`fg` returns) |
| `q` | Quit |

Documents open on their content, and `tab` switches to their properties:

- emails show their headers (from, date, subject, to, cc), their attachments and the body; `A` opens the attachments
  and `T` the thread (by the thread topic, or else by following the replies), oldest first,
- tables show their rows (the first 1,000), with `<` and `>` to scroll through wide tables,
- PDFs show their text page by page, loading more pages as you scroll,
- text and HTML documents show their text.

Lists that contain emails get an ATT column with the number of attachments (one extra request per page).

In the content, `/` finds text, `n` and `N` jump between the hits, and `Y` copies the text. Text from documents is
shown without terminal control characters, so a document cannot change the title or the clipboard of your terminal.

In the documents of a collection, `enter` opens folders, and documents such as emails list their attachments under
"contents". A search (`/`) in a folder covers all folders below it.

`oa browse --debug oa.log` logs every API request with status and duration, and the start of error responses, to
`oa.log`. The API key and credentials in links (such as the signature of a pre-signed storage link a download is
redirected to) are not logged, but search terms and IDs are, so the file is only readable by you.

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
