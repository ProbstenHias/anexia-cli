# anexia-cli

[![CI](https://github.com/ProbstenHias/anexia-cli/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/ProbstenHias/anexia-cli/actions/workflows/ci.yml)
[![Coverage](https://raw.githubusercontent.com/ProbstenHias/anexia-cli/badges/.badges/main/coverage.svg)](https://github.com/ProbstenHias/anexia-cli/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/ProbstenHias/anexia-cli?sort=semver)](https://github.com/ProbstenHias/anexia-cli/releases/latest)
[![Go Reference](https://pkg.go.dev/badge/github.com/ProbstenHias/anexia-cli.svg)](https://pkg.go.dev/github.com/ProbstenHias/anexia-cli)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

A small command-line interface for the [Anexia Engine API](https://engine.anexia-it.com/), built on the
official [go-anxcloud](https://github.com/anexia-it/go-anxcloud) client. It currently covers listing
vSphere provisioning locations and managing local configuration.

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
anexia location list
```

## Usage

### anexia location list

Lists vSphere provisioning locations.

| Flag | Default | Description |
| --- | --- | --- |
| `--page` | `1` | Page number to fetch. Must be 1 or greater. |
| `--limit` | `50` | Locations per page. Must be between 1 and 1000. |
| `--location-code` | | Filter by location code. |
| `--organization` | | Filter by organization. |

```
$ anexia location list --limit 3
CODE    NAME                      COUNTRY       ID
ANX04   AT, Vienna, Datasix       Austria       52b5f6b2fd3a4a24b41c4d8f8f7a5e01
ANX21   DE, Frankfurt, Equinix    Germany       f6f2d1c4f0f14e2b9a3c7d5e2b1a4c33
ANX63   CH, Zurich, Interxion     Switzerland   3c9a7b1e5d4f42a08e6b2c1d9f8a7b65
```

If no locations match, the table header is still printed and `no locations found` goes to stderr.

JSON output returns the raw location objects from the API:

```sh
anexia location list -o json --location-code ANX04
```

```json
[
  {
    "code": "ANX04",
    "country": "AT",
    "id": "52b5f6b2fd3a4a24b41c4d8f8f7a5e01",
    "lat": "48.208174",
    "lon": "16.373819",
    "name": "AT, Vienna, Datasix",
    "country_name": "Austria"
  }
]
```

The `COUNTRY` column shows `country_name` when the API returns it, and falls back to the
two-letter `country` code otherwise.

### Global flags

| Flag | Default | Description |
| --- | --- | --- |
| `--config` | | Path to the config file. |
| `--token` | | Anexia API token. |
| `--api-base-url` | | Anexia Engine base URL. Empty means the go-anxcloud default. |
| `--output`, `-o` | `table` | Output format: `table` or `json`. |
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
| `anexia config view` | Print the whole stored config, as a table or with `-o json`. |

The token is masked in both `config get token` and `config view`: all but the last four
characters are replaced with `*`. `get` and `view` read the config file only, they do not layer in
the environment or flags.

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
