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
```

Every collection list takes `--page`, `--limit` and `--all`. Every `delete` asks for confirmation
unless you pass `--yes`; `tag remove` does not, because reattaching a tag costs nothing. Failures
exit with a distinct code per error class: 2 for usage mistakes, 3 for authentication, 4 for not
found, 5 for timeouts, 6 for rate limits, 7 for a declined confirmation.

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

Only the `core` group below is implemented. Everything after it is a roadmap of what the library
can reach, read off go-anxcloud v0.14.5 and not verified against the Engine.

### core

| Resource | list | get | create | update | delete | extra |
| --- | :-: | :-: | :-: | :-: | :-: | --- |
| `core location` | [x] | [x] | - | - | - | read-only in the Engine |
| `core resource` | [x] | [x] | - | - | - | update and delete unimplemented in the library |
| `core tag` | [x] | [x] | [x] | - | [x] | |
| `core service` | [x] | - | - | - | - | the library implements only list |

### network

| Resource | list | get | create | update | delete | extra |
| --- | :-: | :-: | :-: | :-: | :-: | --- |
| `network vlan` | [ ] | [ ] | [ ] | [ ] | [ ] | |
| `network prefix` | [ ] | [ ] | [ ] | [ ] | [ ] | |
| `network address` | [ ] | [ ] | [ ] | [ ] | [ ] | `reserve` [ ] |

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
| `dns zone` | [ ] | [ ] | [ ] | [ ] | [ ] | `import` [ ] |
| `dns record` | [ ] | - | [ ] | [ ] | [ ] | |

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

The first seven come from the LBaaS v1 API, `cluster` and `node` from v2. How they are grouped in
the CLI is not settled yet.

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
| `storage bucket` | [ ] | [ ] | [ ] | [ ] | [ ] | `empty` [ ] |
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
| Registry support for `create`, `update`, `delete` | [ ] |
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

The write verbs are specified in [docs/cli-design.md](docs/cli-design.md) but not implemented:
every resource reachable today is read-only in the Engine, so there is nothing yet for them to
act on. `core tag` is the exception and drives the legacy client directly.

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

## License

MIT, Matthias Weilinger. See [LICENSE](LICENSE).
