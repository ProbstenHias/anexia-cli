# anexia-cli

[![CI](https://github.com/ProbstenHias/anexia-cli/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/ProbstenHias/anexia-cli/actions/workflows/ci.yml)
[![Coverage](https://raw.githubusercontent.com/ProbstenHias/anexia-cli/badges/.badges/main/coverage.svg)](https://github.com/ProbstenHias/anexia-cli/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/ProbstenHias/anexia-cli?sort=semver)](https://github.com/ProbstenHias/anexia-cli/releases/latest)
[![Go Reference](https://pkg.go.dev/badge/github.com/ProbstenHias/anexia-cli.svg)](https://pkg.go.dev/github.com/ProbstenHias/anexia-cli)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

A command-line interface for the [Anexia Engine API](https://engine.anexia-it.com/), built on the
official [go-anxcloud](https://github.com/anexia-it/go-anxcloud) client.

Every resource follows the same shape, `anexia <group> <noun> <verb>`, with the same verb
vocabulary, paging flags and four output formats. Each resource exposes only the operations its
API supports. See [docs/cli-design.md](docs/cli-design.md)
for the rules and [Feature coverage](#feature-coverage) for what is implemented so far.

The project is `anexia-cli`; the installed command is `anexia`.

## Install

### Homebrew

```sh
brew install --cask ProbstenHias/tap/anexia
```

The cask lives in [ProbstenHias/homebrew-tap](https://github.com/ProbstenHias/homebrew-tap) and is
regenerated on every release. The binary is not signed or notarized, so the cask strips the macOS
quarantine attribute at install time.

### Prebuilt archives

Archives for darwin, linux and windows (amd64 and arm64) are attached to each
[GitHub release](https://github.com/ProbstenHias/anexia-cli/releases). Unpack one and put `anexia`
somewhere on your `PATH`.

### From source

```sh
git clone https://github.com/ProbstenHias/anexia-cli.git
cd anexia-cli
make build   # ./bin/anexia
```

`go install` is deliberately not supported: it names the binary after the module, giving you
`anexia-cli` instead of `anexia`.

## Quickstart

```sh
anexia config set token <token>
anexia core location list
```

## Usage

Commands are `anexia <group> <noun> <verb>`. Nouns are singular and accept their plural as an
alias, so `anexia core locations list` works too.

```
$ anexia core location list --limit 3
IDENTIFIER                         CODE    NAME                     COUNTRY   CITY
52b5f6b2fd3a4a24b41c4d8f8f7a5e01   ANX04   AT, Vienna, Datasix      AT        VIE
f6f2d1c4f0f14e2b9a3c7d5e2b1a4c33   ANX21   DE, Frankfurt, Equinix   DE        FRA
3c9a7b1e5d4f42a08e6b2c1d9f8a7b65   ANX63   CH, Zurich, Interxion    CH        ZRH
```

Tables go to stdout; notes like `no locations found` go to stderr, so piping stays clean. `-o json`
and `-o yaml` print the Engine objects with the API's own field names, `-o tsv` prints
tab-separated columns for `cut` and `awk`, and `--no-headers` drops the header row from `table` and
`tsv`.

```sh
anexia core location list -o tsv --no-headers | cut -f2
anexia core resource list --tag production -o json
anexia core resource tag add <resource-id> production staging
anexia core tag delete <tag-id> --service <service-id> --yes
anexia network vlan list --location <location-id> --status Active
anexia network address list --prefix <prefix-id> --version 4
```

Every collection list takes `--page`, `--limit` and `--all`. Every `delete` asks for confirmation
unless you pass `--yes`; `tag remove` does not, because reattaching a tag costs nothing. Failures
exit with a distinct code per error class: 2 for usage mistakes, 3 for authentication, 4 for not
found, 5 for timeouts, 6 for rate limits, 7 for a declined confirmation.

### DNS

Payload fields are flags, on `create` and `update` alike, so an update names only what is
changing and everything else keeps the value the Engine already had.

```sh
anexia dns zone create --name example.com --admin-email admin@example.com
anexia dns zone update example.com --ttl 7200
anexia dns record create --zone example.com --name www --type A --rdata 10.0.0.1
anexia dns record list --zone example.com --type A
```

A record lives inside a zone, so every `dns record` verb takes `--zone`. There is no
`dns record get`: the Engine has no endpoint for a single record, and `dns record update` works
around that by reading the zone's records and writing back the one you named.

Renaming a zone is not offered. The Engine's zone update carries the name in the request body
with no old name anywhere, so what a changed name would do is not something the client defines.

Two zone operations take a document rather than flags, because that is what the Engine accepts:

```sh
anexia dns zone import example.com --file zone.db
anexia dns zone apply example.com --file changeset.json
```

`import` replaces the zone's contents with a BIND zone file. `apply` sends a JSON changeset of
records to create and records to delete. Both confirm first, and both read stdin with `--file -`,
which needs `--yes` since the prompt would otherwise read the document.

### Global flags

| Flag | Default | Description |
| --- | --- | --- |
| `--config` | | Path to the config file. |
| `--token` | | Anexia API token. |
| `--api-base-url` | | Anexia Engine base URL. Empty means the go-anxcloud default. |
| `--output`, `-o` | `table` | Output format: `table`, `json`, `yaml` or `tsv`. |
| `--no-headers` | `false` | Omit the header row in `table` and `tsv` output. |
| `--yes`, `-y` | `false` | Skip confirmation prompts. |
| `--timeout` | `30s` | Timeout for API requests. |

## Configuration

Values are layered, lowest precedence first:

| Precedence | Source | Details |
| --- | --- | --- |
| 1 (lowest) | Config file | `token`, `api_base_url` |
| 2 | Environment | `ANEXIA_TOKEN` -> `token`, `ANEXIA_API_BASE_URL` -> `api_base_url` |
| 3 (highest) | Command-line flags | `--token`, `--api-base-url`, only when explicitly set |

Flags only participate when you actually pass them, so unset flag defaults never overwrite a
value from the file or the environment. Empty environment variables are ignored too.

The config file path is resolved in this order:

1. `--config`
2. `$ANEXIA_CONFIG`
3. `$XDG_CONFIG_HOME/anexia/config.yaml`
4. `~/.config/anexia/config.yaml`

A missing file is not an error. When anexia writes the file it creates the parent directory with
mode 0700 and the file with mode 0600, replacing it atomically. The only valid keys are `token`
and `api_base_url`; anything else makes reading the file fail rather than being silently ignored.

```yaml
token: your-anexia-api-token
api_base_url: https://engine.anexia-it.com
```

### Config subcommands

| Command | Description |
| --- | --- |
| `anexia config path` | Print the resolved config file path. |
| `anexia config init [--force]` | Write an empty config file. Fails if one exists unless `--force`. |
| `anexia config set <key> <value>` | Set `token` or `api_base_url` and save. |
| `anexia config get <key>` | Print one value from the config file. |
| `anexia config view` | Print the whole stored config in any output format. |

The token is masked in both `config get token` and `config view`: all but the last four
characters are replaced with `*`; values four characters or shorter become `****`. `get` and
`view` read the config file only, they do not layer in the environment or flags.

## Upgrading from 0.1

`anexia location list` is now `anexia core location list`. The old command is gone and the new one
is not a rename: it reads the Engine's core location endpoint instead of the vSphere provisioning
one, so the columns differ and the `--location-code` and `--organization` filters do not exist.
Filter client-side for now:

```sh
anexia core location list -o tsv --no-headers | grep ANX04
```

The vSphere-specific location list, with its human-readable country name and server-side filters,
returns with the `vsphere` group.

## Feature coverage

`[x]` ships and `[ ]` is planned. A `-` means the CLI will not grow the verb, because go-anxcloud
cannot reach it: either the library says the Engine has no such operation, or it has not
implemented one. The distinction matters to whoever picks the work up, so the tables say which
when the library says which, but a `-` is never evidence about the Engine on its own.

The `core`, `network` and `dns` groups below are implemented. `network` reads only for now: its
prefix and address commands are hand-written against the older client, and the registry only just
grew the write verbs, so giving them to one half of the CLI would leave the two halves offering
different verbs for the same noun. Everything after those three groups is a roadmap of what the
library can reach, read off go-anxcloud v0.14.5 and not verified against the Engine.

### core

| Resource | list | get | create | update | delete | extra |
| --- | :-: | :-: | :-: | :-: | :-: | --- |
| `core location` | [x] | [x] | - | - | - | read-only in the Engine |
| `core resource` | [x] | [x] | - | - | - | `tag list`/`add`/`remove` [x]; update and delete unimplemented in the library |
| `core tag` | [x] | [x] | [x] | - | [x] | |
| `core service` | [x] | - | - | - | - | the library implements only list |

### network

| Resource | list | get | create | update | delete | extra |
| --- | :-: | :-: | :-: | :-: | :-: | --- |
| `network vlan` | [x] | [x] | [ ] | [ ] | [ ] | `--status` and `--location` filters [x] |
| `network prefix` | [x] | [x] | [ ] | [ ] | [ ] | `--search` [x] |
| `network address` | [x] | [x] | [ ] | [ ] | [ ] | `--search` [x]; field filters [x]; `reserve` [ ] |

### vsphere

| Resource | list | get | create | update | delete | extra |
| --- | :-: | :-: | :-: | :-: | :-: | --- |
| `vsphere vm` | [ ] | [ ] | [ ] | [ ] | [ ] | `power get`/`set` [ ] |
| `vsphere template` | [ ] | [ ] | - | - | - | |
| `vsphere location` | [ ] | - | - | - | - | |
| `vsphere disk-type` | [ ] | - | - | - | - | |
| `vsphere nic-type` | [ ] | - | - | - | - | |
| `vsphere cpu-performance-type` | [ ] | - | - | - | - | |
| `vsphere availability-zone` | [ ] | - | - | - | - | |
| `vsphere free-ip` | [ ] | - | - | - | - | |

### dns

| Resource | list | get | create | update | delete | extra |
| --- | :-: | :-: | :-: | :-: | :-: | --- |
| `dns zone` | [x] | [x] | [x] | [x] | [x] | `import`/`apply` [x]; no rename, the Engine's update carries no old name |
| `dns record` | [x] | - | [x] | [x] | [x] | scoped by `--zone`; get unimplemented in the library |

### kubernetes

| Resource | list | get | create | update | delete | extra |
| --- | :-: | :-: | :-: | :-: | :-: | --- |
| `kubernetes cluster` | [ ] | [ ] | [ ] | [ ] | [ ] | `kubeconfig get`/`delete` [ ] |
| `kubernetes node-pool` | [ ] | [ ] | [ ] | [ ] | [ ] | |
| `kubernetes disk` | [ ] | [ ] | [ ] | [ ] | [ ] | legacy client only |
| `kubernetes network` | [ ] | [ ] | [ ] | [ ] | [ ] | legacy client only |

### lbaas

| Resource | list | get | create | update | delete |
| --- | :-: | :-: | :-: | :-: | :-: |
| `lbaas load-balancer` | [ ] | [ ] | [ ] | [ ] | [ ] |
| `lbaas frontend` | [ ] | [ ] | [ ] | [ ] | [ ] |
| `lbaas backend` | [ ] | [ ] | [ ] | [ ] | [ ] |
| `lbaas server` | [ ] | [ ] | [ ] | [ ] | [ ] |
| `lbaas bind` | [ ] | [ ] | [ ] | [ ] | [ ] |
| `lbaas acl` | [ ] | [ ] | [ ] | [ ] | [ ] |
| `lbaas rule` | [ ] | [ ] | [ ] | [ ] | [ ] |
| `lbaas cluster` | [ ] | [ ] | [ ] | [ ] | [ ] |
| `lbaas node` | [ ] | [ ] | [ ] | [ ] | [ ] |
| `lbaas v2 load-balancer` | [ ] | [ ] | [ ] | [ ] | [ ] |

The first seven come from the LBaaS v1 API; `cluster`, `node` and the distinct v2 load balancer
come from v2. How the two versions are grouped in the CLI is not settled yet.

### e5e and frontier

| Resource | list | get | create | update | delete | extra |
| --- | :-: | :-: | :-: | :-: | :-: | --- |
| `e5e application` | [ ] | [ ] | [ ] | [ ] | [ ] | |
| `e5e function` | [ ] | [ ] | [ ] | [ ] | [ ] | |
| `frontier api` | [ ] | [ ] | [ ] | [ ] | [ ] | |
| `frontier endpoint` | [ ] | [ ] | [ ] | [ ] | [ ] | |
| `frontier action` | [ ] | [ ] | [ ] | [ ] | [ ] | |
| `frontier deployment` | [ ] | [ ] | [ ] | [ ] | [ ] | create is a deploy action |

### storage

| Resource | list | get | create | update | delete | extra |
| --- | :-: | :-: | :-: | :-: | :-: | --- |
| `storage bucket` | [ ] | [ ] | [ ] | [ ] | [ ] | `empty-and-delete` [ ] |
| `storage tenant` | [ ] | [ ] | [ ] | [ ] | [ ] | |
| `storage user` | [ ] | [ ] | [ ] | [ ] | [ ] | |
| `storage key` | [ ] | [ ] | [ ] | [ ] | [ ] | |
| `storage region` | [ ] | [ ] | [ ] | [ ] | [ ] | |
| `storage endpoint` | [ ] | [ ] | [ ] | [ ] | [ ] | |
| `storage backend` | [ ] | [ ] | [ ] | [ ] | [ ] | |
| `storage server-interface` | [ ] | [ ] | [ ] | [ ] | [ ] | dynamic-volume API |

The object storage API is marked beta in go-anxcloud, and `region`, `endpoint` and `backend`
handle updates differently from the other four, so their write verbs may not all work out.

### Cross-cutting

| Feature | Status |
| --- | :-: |
| Declarative resource registry, read verbs | [x] |
| Registry support for `create`, `update`, `delete` | [x] |
| Registry support for scoped resources, such as a record inside its zone | [x] |
| `table`, `json`, `yaml`, `tsv` output | [x] |
| `--no-headers` | [x] |
| Paging with `--page`, `--limit`, `--all` | [x] |
| Confirmation prompts and `--yes` | [x] |
| Per-error-class exit codes on both API clients | [x] |
| Config file, environment and flag layering | [x] |
| Shell completion | [x] |
| Conformance test enforcing the design rules | [x] |
| `--wait` and `--wait-timeout` for stateful resources | [ ] |
| Tag filters on every taggable resource | [ ] |
| `--field` column selection | [ ] |

The write verbs landed with the `dns` group, the first resources the Engine lets the CLI write
through the registry. Most other resources reachable today are read-only in the Engine anyway.
`core tag` is the exception and drives the legacy client directly; prefixes and addresses are
writable in the library and still wait to be declared on the registry, so that the two halves of
the CLI keep offering the same verbs.

## Development

```sh
make build   # build ./bin/anexia with version info linked in
make test    # go test -race ./...
make lint    # golangci-lint
make fmt     # gofumpt -w .
make ci      # fmt-check, vet, lint, test
```

Tests exercise the real command tree against `net/http/httptest` servers instead of mocking the
API client.

## Dependency updates

[Dependabot](.github/dependabot.yml) opens pull requests every Monday for Go modules and GitHub
Actions. Minor and patch bumps arrive grouped in one pull request, majors come separately so a
breaking change is never buried. Titles are generated as `chore(deps): ...` and `ci(deps): ...` so
they pass the conventional commit check, and both types are hidden from the changelog.

Dependabot decides the capitalisation of the subject from recent commit subjects rather than from
its configuration, so if a pull request ever arrives titled `chore(deps): Bump ...` the title check
will fail on the leading capital and the title has to be edited by hand.

## Releasing

Releases are cut by [release-please](https://github.com/googleapis/release-please). Pull requests
are squash merged and the pull request title becomes the commit subject on `main`, so titles must
follow [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/); a CI check enforces
this. `feat:` bumps the minor version, `fix:` the patch version.

release-please keeps a `chore(release): release X.Y.Z` pull request open that collects those
subjects into `CHANGELOG.md` and bumps `.release-please-manifest.json`. Merging it tags the
release, which triggers GoReleaser to build the archives, publish the GitHub release and update the
Homebrew cask. Nothing is tagged by hand.

## License

MIT, Matthias Weilinger. See [LICENSE](LICENSE).
