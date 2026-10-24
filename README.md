# profilers

Collects your dotfiles, credential directories and project secret/config
files into a single `.tgz`, laid out relative to `$HOME` so it restores in place.

## Build

```sh
task build        # current platform → dist/profilers
task build:all    # dist/profilers-{darwin-arm64,linux-amd64,linux-arm64}, static + stripped
task clean
```

Or without [go-task](https://taskfile.dev): `go build -o dist/profilers .`

## Usage

```sh
dist/profilers --dry-run          # list what would be archived + total size
dist/profilers                    # write ~/Backups/profilers-<host>-<YYYYMMDD-HHMMSS>.tgz
dist/profilers -o /tmp/x.tgz -v   # custom destination, print each file
dist/profilers -c other.toml      # alternate config
```

Config lookup order: `-c`, `$PROFILERS_CONFIG`, `./config.toml`,
`~/.config/profilers/config.toml`.

## What gets archived

See [config.toml](./config.toml). In short:

| Section | Behaviour |
|---|---|
| `home.dotfiles` | every top-level `~/.*` regular file, minus `dotfile_excludes` globs |
| `home.dot_dirs` | full copies of e.g. `~/.ssh`, `~/.aws`, `~/.config`, with per-dir `dot_dir_excludes` |
| `[[scan]]` | walk `path` to `depth` levels (0 = the dir itself) and pick basenames matching `patterns.include` |
| `patterns.exclude_dirs` | pruned everywhere (`node_modules`, `.git`, `.venv`, …) |
| `macos.app_support` | full copies of `~/Library/Application Support/<name>` |

`follow_symlinks = true` stores the link target's content under the link's
path; set to `false` to store symlinks as symlinks. Directory loops are
detected. Entries are deduplicated by archive path. Directory entries are
included for full-tree copies so modes like `0700` on `~/.ssh` survive.

## Restore

```sh
tar -xzf ~/Backups/profilers-*.tgz -C ~
```

The archive is created `0600` and the tool refuses to overwrite an existing
file. **It contains live secrets in plain text** — encrypt it before it leaves
the machine (e.g. `age -p`, `gpg -c`).
