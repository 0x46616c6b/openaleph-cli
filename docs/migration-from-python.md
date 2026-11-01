# Migrating from the Python `openaleph` client

`oa` is a drop-in replacement for `openaleph-client`. Every command of the Python client is still available under
its old name and flags (hidden from `--help`), so most scripts only need `openaleph` replaced by `oa`. The
environment variables are read as before.

## Command mapping

| Python client | Preferred in `oa` |
| --- | --- |
| `openaleph crawldir -f ID PATH` | `oa collection upload ID PATH` |
| `openaleph fetchdir -f ID -p DIR` | `oa collection download ID -o DIR` |
| `openaleph fetchdir -e ENTITY -p DIR` | `oa entity download ENTITY -o DIR` |
| `openaleph reingest -f ID` | `oa collection reingest ID` |
| `openaleph reindex -f ID` | `oa collection reindex ID` |
| `openaleph delete -f ID` | `oa collection delete ID` |
| `openaleph flush -f ID` | `oa collection flush ID` |
| `openaleph write-entity -f ID` | `oa entity write ID` |
| `openaleph write-entities -f ID` | `oa entity write-many ID` |
| `openaleph delete-entity ENTITY` | `oa entity delete ENTITY` |
| `openaleph stream-entities -f ID` | `oa entity stream ID` |
| `openaleph entitysets -f ID` | `oa entityset list -c ID` |
| `openaleph entitysetitems SET` | `oa entityset items SET` |
| `openaleph make-list -f ID LABEL` | `oa entityset create ID LABEL` |

The new commands take the collection as an argument and accept a numeric ID or a foreign ID. The old `-f` flags
accept foreign IDs only.

## Intentional differences

These are deliberate, to avoid data loss:

- **Destructive commands ask first.** `delete`, `flush` and `delete-entity` (old and new names) ask for confirmation
  on a terminal and refuse to run without one in scripts. Add `--yes` to scripts.
- **`crawldir` never deletes its state file.** The Python client silently deletes `.openaleph_crawl_state.db` when
  `--resume` is missing. `oa` refuses to start and tells you to pass `--resume` or to remove the file yourself. The
  state file format is unchanged, so a crawl started with the Python client can be resumed with `oa` and the other
  way round.
- **No `.openaleph-failed.txt`.** Failed paths are listed in the log at the end of the crawl and stay in the state
  file. A resumed crawl retries them and clears them on success.
- **`fetchdir` keeps existing files.** Without `--overwrite`, a file that already exists is skipped, also when its size
  differs. Downloads are written to a temporary file first, so an existing file is always complete. Server-provided
  file names are sanitized and cannot write outside the destination.
- **Folders that fail to upload** skip their subtree and are recorded as failed, instead of uploading the children
  without a parent.
- **Blank lines** on standard input of `write-entities` are ignored. Invalid JSON is reported with its line number.

## Not carried over

- The Python library API (`AlephAPI`). `oa` is a command-line tool. Use `oa api` or the REST API directly.
