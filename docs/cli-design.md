# CLI design

This document is the contract for how `anexia` commands look and behave. It exists so that
adding the tenth resource takes as little thought as adding the second, and so that a user who
learned one command already knows the rest.

Most of these rules are enforced by `internal/cli/conformance_test.go`, which walks the whole
command tree on every test run. If you break a rule, that test tells you which command and why.

## Shape of a command

```
anexia <group> <noun> <verb> [<id>] [flags]
```

For example:

```sh
anexia core location list --limit 10
anexia core location get 52b5f6b2fd3a4a24b41c4d8f8f7a5e01
anexia core tag create --name prod --service <service-id>
anexia core tag delete <tag-id> --service <service-id> --yes
```

Group, noun, verb, in that order, always. The noun is singular so the sentence reads correctly:
`core location get`, not `core locations get`. Every noun registers its plural as an alias, so
`anexia core locations list` works too, but the canonical name in help output is singular. Use
`resource.Noun` rather than `resource.Group` to build a noun and the alias comes with it.

Groups mirror the Anexia Engine's own API areas rather than inventing a taxonomy: `core`,
and later `network`, `vsphere`, `kubernetes`, `lbaas`, `dns`, `e5e`, `frontier`, `storage`.
The singular rule does not apply to them, because Anexia named them, not us. Two commands sit
outside this scheme because they never talk to the Engine: `anexia config` and `anexia version`.

A group or noun invoked bare prints its help and exits 0. It never errors and never guesses a
default verb.

## The five verbs

Every resource uses the same vocabulary. Nothing else is allowed at a leaf.

| Verb | Arguments | Engine call | Notes |
| --- | --- | --- | --- |
| `list` | none | `List` | Paged. Always available if the Engine can enumerate the resource. |
| `get` | `<id>` | `Get` | One object by identifier. |
| `create` | none, flags carry the payload | `Create` | |
| `update` | `<id>`, flags carry the changes | `Get` then `Update` | |
| `delete` | `<id>` | `Destroy` | Confirms first. Aliased to `destroy`. |

A resource only gets the verbs the Engine actually supports. `core location` is read-only in the
Engine, so it exposes `list` and `get` and nothing else. This is deliberate: a `create` that
always fails with "operation not supported" is worse than no `create` at all.

The same rule applies to the registry itself. `internal/resource` currently implements `list` and
`get`, because every resource the CLI reaches today is read-only. The write verbs are specified
here and land with the first resource that needs them, rather than sitting unused.

Two extra verbs exist for relations, meaning a collection a resource owns that has no identity of
its own. A resource's tags are the example:

```sh
anexia core resource tag list <resource-id>
anexia core resource tag add <resource-id> prod staging
anexia core resource tag remove <resource-id> staging
```

`add` and `remove` are used instead of `create` and `delete` because you are not creating a tag,
you are attaching an existing one. The distinction matters: `anexia core tag create` really does
create a tag object, and `anexia core resource tag add` does not.

Verbs that do not appear anywhere, on purpose: `describe` (that is `get`), `show` (also `get`),
`ls` (that is `list`), `rm` (that is `delete`), `edit`, `apply`, `patch`.

## Flags

Global flags live on the root command and work everywhere.

| Flag | Default | Purpose |
| --- | --- | --- |
| `--config` | | Path to the config file. |
| `--token` | | API token. |
| `--api-base-url` | | Engine base URL. |
| `-o`, `--output` | `table` | Output format: `table`, `json`, `yaml`, `tsv`. |
| `--no-headers` | `false` | Drop the header row from `table` and `tsv`. |
| `-y`, `--yes` | `false` | Skip confirmation prompts. |
| `--timeout` | `30s` | Deadline for each API request. |

Every `list` over a collection accepts the same paging flags: `--page` (default 1), `--limit`
(default 50, capped at 1000), and `--all` to walk every page. Relation lists such as
`resource tag list` do not, because the items come back inside the parent object and there is
nothing to page over.

`--all` stops when a page comes back shorter than the page size the Engine actually applied, when
the Engine reports it has run out of pages, or when the Engine hands back a page number other
than the one asked for, which is what an endpoint that ignores paging does. Failing all of those
it gives up after a thousand pages rather than looping until `--timeout`. Comparing against the
requested `--limit` instead of the applied page size would silently truncate the walk against an
Engine that caps page size, so it must not be used for this.

Flag names are lowercase and use dashes, never underscores. Every flag has a usage string. A
command never registers a local flag whose name collides with a global one.

Filter flags on `list` are named after the field they filter, in the singular: `--tag`, `--name`,
`--location`, `--status`, `--service`. Not after the Engine's query parameter, which is why
`core tag list` takes `--name` even though the Engine calls it `query`. Repeatable filters would
be plural, but the Engine does not currently accept any.

When write verbs arrive, the ones on resources that report a provisioning state will get `--wait`
and `--wait-timeout`. Resources without a state must not get the flags at all, so `--wait` is
never accepted only to fail later.

## Positional arguments

Only identifiers go in positional arguments. Everything else is a flag.

The reason is symmetry: `get <id>`, `update <id>`, `delete <id>` all address one object, and an
identifier is the one thing that is never optional. Payload fields as flags also means
`create` and `update` can share flag definitions and `update` only needs the fields you actually
changed.

Every positional argument is named in the command's `Use` string, so help output shows
`get <id>` and `tag add <resource-id> <tag>...`. Every leaf validates its argument count, so a
stray argument is an error rather than being silently dropped.

## Output

Four formats, one flag.

`table` is the default and is meant for humans: aligned columns, uppercase headers, no borders.
Column sets are short on purpose, four or five fields, because a table wider than a terminal is
useless. The full object is one `-o json` away.

`tsv` is `table` without the alignment: raw values, lowercase headers, tab-separated. This is the
one to pipe into `cut` and `awk`.

`json` and `yaml` render the Engine object as it came off the wire, using the same field names
the API uses. Both are derived from the same JSON encoding, so `yaml` keys match `json` keys
exactly.

`--no-headers` affects `table` and `tsv` only.

Rules that hold across every command:

An empty `list` prints the header row to stdout and `no <plural> found` to **stderr**, so a
pipeline that reads stdout sees a clean empty result. In `json` the same case is `[]`, never
`null`.

Progress notes, confirmations, and messages like `deleted tag t-1` also go to stderr. Stdout
carries data and nothing else.

## Confirmation

`delete` prompts before acting:

```
delete tag "t-1" [y/N]: 
```

Only `y` or `yes`, case-insensitively, counts as yes. Everything else, including an empty line
and a closed stdin, is a no and exits with code 7.

`--yes` skips the prompt. When stdin is not available and `--yes` was not passed, the command
fails with a message telling you to pass `--yes` rather than hanging or silently proceeding.

Relation removal (`resource tag remove`) does not prompt, because reattaching a tag is trivial
and prompting on every tag change would be noise.

## Errors and exit codes

Errors read as `<what failed>: <why>`, lowercase, no trailing period:

```
anexia: listing locations: Engine returned an error: 500 Internal Server Error (500)
```

The prefix comes from the command (`listing locations`, `reading tag "t-1"`), the rest is the
underlying error. Nothing is swallowed.

One rewrite happens. The legacy client's error stringifies as a Go struct dump,
`received error from api: {Code:404 Message:... Validation:map[]}`, which is not something to show
a user. `errmap` replaces that span with `<message> (<status>)` and leaves the command's prefix
alone. The generic client already reads well and passes through untouched.

Anything the user got wrong about the invocation is prefixed `invalid usage:` instead, which is
what makes it exit 2:

```
anexia: invalid usage: unknown command "location" for "anexia"
anexia: invalid usage: --limit 0 must be between 1 and 1000
```

That covers unknown commands, unknown flags, wrong argument counts, an unsupported `--output`,
and out-of-range paging. `internal/cli.markUsageErrors` wraps every argument validator in the
tree once at construction time, so a new command gets this without doing anything.

Exit codes let scripts branch without parsing messages:

| Code | Meaning |
| --- | --- |
| 0 | Success. |
| 1 | Unclassified failure. |
| 2 | Bad flags or arguments. |
| 3 | Missing or rejected token. |
| 4 | Object not found. |
| 5 | Request deadline elapsed. |
| 6 | Throttled by the Engine. |
| 7 | Confirmation declined. |

Two failures get an extra hint appended, because the Engine's own wording is not actionable:
an auth failure suggests `anexia config view`, and a rate limit suggests retrying later. A
timeout says what the timeout was and that `--timeout` raises it.

The code must not depend on which go-anxcloud client a command happens to use. The two report
status differently, the generic one as `api.HTTPError` and the legacy one as `client.ResponseError`,
so `errmap` reads both. A 404 is exit 4 either way. For the legacy shape the status comes off the
HTTP response, not the `code` field in the decoded body: the Engine does not always repeat its
status there, and reading the body alone turns those failures into exit 1.

The hint and the exit code are decided by the same classification, so a message can never
disagree with the code the process exits with.

## How this is enforced

`internal/resource` holds a declarative `Spec` per resource plus the verb builders. A resource is
a value, not a command tree:

```go
resource.Command(opts, resource.Spec[corev1.Location, *corev1.Location]{
    Noun:     "location",
    Short:    "Work with Anexia locations",
    List:     true,
    Get:      true,
    Identify: func(l *corev1.Location, id string) { l.Identifier = id },
    Columns: []resource.Column[corev1.Location]{
        {Name: "identifier", Value: func(l *corev1.Location) string { return l.Identifier }},
        {Name: "code", Value: func(l *corev1.Location) string { return l.Code }},
    },
})
```

Flag wiring, paging, rendering and error wrapping live in the verb builders, so a new resource
cannot get them wrong. Setting `List: true` gets you `--page`, `--limit`, `--all`, all four output
formats, `--no-headers`, the plural alias, the empty-result note on stderr and the
`listing <plural>: %w` error prefix, identical to every other resource.

Some Engine areas have no generic object in go-anxcloud yet, so their commands are written by
hand against the legacy client (`core tag` and `core service` are the current examples). They
follow the same rules by sharing the same pieces rather than by copying them: `resource.Noun` for
the plural alias, `RegisterPagingFlags` and `ValidatePaging` for paging, `FetchPages` for `--all`,
`RenderList` for output. Reach for those before writing a variant. When the generic client gains
the object, the hand-written command is replaced by a `Spec` and the behavior does not change.

Every divergence between the two halves that users could observe has been a bug so far: a missing
plural alias, an exit code that depended on the client, a `--all` flag present on one half only.
Sharing a helper is how that stops happening.

The conformance test checks the rules that a registry cannot: verb vocabulary, singular nouns
carrying a plural alias, help text present and not ending in a period, groups printing help,
argument validators present, positional arguments documented, paging flags present on collection
lists and absent from relation lists, error messages reading as action then cause, flag naming,
and no local flag shadowing a global one.

The error-shape check drives each command with arguments derived from its own `Args` validator and
flag set, and fails if a command never got past parsing. A conformance test that only ever
observes `invalid usage:` proves nothing, which is exactly how it was wrong once.

Two rules are checked by targeted tests rather than by walking the tree, because they need a
server to observe: `--all` requesting every page exactly once from any starting page, and
terminating against an Engine that caps page size, ignores paging, or never stops
(`internal/resource`); and the exit-code table holding on both clients whether or not the Engine
repeats its status in the response body (`internal/cli`).
