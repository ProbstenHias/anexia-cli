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

Every resource uses the same CRUD vocabulary. Capability-specific operations
use a small reviewed extension rather than being forced into a misleading CRUD
verb.

| Verb | Arguments | Engine call | Notes |
| --- | --- | --- | --- |
| `list` | none | `List` | Paged. Always available if the Engine can enumerate the resource. |
| `get` | `<id>` | `Get` | One object by identifier. |
| `create` | none, flags carry the payload | `Create` | |
| `update` | `<id>`, flags carry the changes | `Get` then `Update` | |
| `delete` | `<id>` | `Destroy` | Confirms first. Aliased to `destroy`, which is never a command name. |

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

Four planned operations have no honest CRUD spelling and are allowed as leaf
verbs: `network address reserve`, `dns zone import`, `dns zone apply`, and
`storage bucket empty-and-delete`. The last name is deliberately explicit: go-anxcloud's
`EmptyAndDelete` permanently deletes the bucket after emptying it, so `empty`
would promise safer behavior than the API provides. Adding any other action verb
requires updating the design and the conformance vocabulary together.

Verbs that do not appear anywhere, on purpose: `describe` (that is `get`), `show` (also `get`),
`ls` (that is `list`), `rm` (that is `delete`), `edit`, `patch`.

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
| `--timeout` | `30s` | Deadline for the whole command, including every page of a list. |

Every `list` over a collection accepts the same paging flags: `--page` (default 1), `--limit`
(default 50, capped at 1000), and `--all` to walk every page. Relation lists such as
`resource tag list` do not, because the items come back inside the parent object and there is
nothing to page over.

`--all` stops on an empty page and nothing else. Failing that it gives up after a thousand pages
rather than looping until `--timeout`, and says how to narrow the results.

Nothing weaker than an empty page is safe, and the reason is worth writing down because three
earlier attempts got it wrong. Every paging field in an Engine response is optional, and
go-anxcloud reports a missing field and a literal zero as the same value. So the reported page
number, the applied page size, and the total page count are each untrustworthy on their own:

- Stopping on a page shorter than `--limit` truncates against an Engine that caps its page size
  below the requested limit, which the default limit of 50 makes likely rather than exotic.
- Stopping when the reported page number does not match the requested one truncates against any
  endpoint that omits the field.
- Stopping at the reported total truncates when that total is computed from the requested limit
  while the Engine caps lower.

Every one of those failures is silent and exits 0, which is worse than the runaway they were
meant to prevent. The cost of the empty-page rule is one extra request per walk.

Three consequences follow. An Engine that answers the page after the last one with a 404 instead
of an empty list ends the walk on either client rather than losing it, because that extra request
now happens on every complete walk. A resource go-anxcloud marks as not paginating is fetched
exactly once, with `--page` beyond the first rejected as a usage mistake instead of silently
returning page one. And the thousand-page backstop bounds pages of results rather than requests,
so a result set exactly that long is returned instead of reported as a runaway.

Ending a walk on a not-found is a guess, and the CLI says so. The same 404 could mean a page past
the end, a parent object deleted while the walk ran, or a proxy having a bad moment, and nothing in
the response tells them apart. So the walk ends, keeps what it collected, and writes a warning to
stderr naming the page it stopped at. A 404 on the first page the user asked for is different: it
is not past anything, so it stays a plain not-found and exits 4.

Flag names are lowercase and use dashes, never underscores. Every flag has a usage string. A
command never registers a local flag whose name collides with a global one.

Filter flags on `list` are named after the field they filter, in the singular: `--tag`, `--name`,
`--location`, `--status`, `--service`. Not after the Engine's query parameter, which is why
`core tag list` takes `--name` even though the Engine calls it `query`. Repeatable filters would
be plural, but the Engine does not currently accept any.

Free-text search is the one thing that flag naming rule cannot cover, because there is no field to
name it after: the Engine matches the term against whichever fields it likes. That flag is
`--search`, and it is only on the commands whose endpoint offers it, `network prefix list` and
`network address list`. Where an endpoint offers both, search and the field filters are separate
endpoints that do not accept each other's parameters, so asking for both at once is a usage
mistake rather than a request the CLI narrows on its own.

Sort controls are not filters and are named for what they do: `--order` takes the field to sort
by, `--descending` reverses it. Only `core tag list` has them, because it is the only endpoint
that accepts sorting.

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

An argument that cannot stay in one URL path segment is refused before anything is sent. Both
clients put an identifier into the path, so an empty value addresses the collection, a slash adds
another segment, and a relative segment like `..` is normalized away. Each can make the Engine act
on something the caller never named, and on a `delete` that is the worst outcome available:
`tag remove r-1 ..` issued a delete against the resource itself and reported success. Escaping
cannot help because go-anxcloud parses the escaped path and joins it again, turning an escaped
slash back into structure. Anything else about a valid identifier is the Engine's business.

## Output

Four formats, one flag.

`table` is the default and is meant for humans: aligned columns, uppercase headers, no borders.
Column sets are short on purpose, four or five fields, because a table wider than a terminal is
useless. The full object is one `-o json` away.

`tsv` is `table` without the alignment: raw values, lowercase headers, tab-separated. This is the
one to pipe into `cut` and `awk`.

`json` and `yaml` render the decoded Engine object using the API's own field names. Both come from
the same JSON encoding, so `yaml` keys match `json` keys exactly.

Decoded, not forwarded: what you get is go-anxcloud's view of the object, so a field the Engine
sent that the library does not model is dropped, a field it models but the Engine omitted appears
as a zero value, and a few objects reshape what they decode. Do not diff this against `curl`.

`--no-headers` affects `table` and `tsv` only.

Rules that hold across every command:

An empty `list` prints the header row to stdout and `no <plural> found` to **stderr**, so a
pipeline that reads stdout sees a clean empty result. In `json` the same case is `[]`, never
`null`.

Progress notes, confirmations, and messages like `deleted tag t-1` also go to stderr. For any
command that talks to the Engine, stdout carries data and nothing else. The `config` commands sit
outside that rule: their output is the thing you asked for, so `config path` prints to stdout on
purpose.

## Confirmation

`delete` prompts before acting:

```
delete tag "t-1" [y/N]: 
```

Only `y` or `yes`, case-insensitively, counts as yes. Everything else, including an empty line, is
a no and exits with code 7.

`--yes` skips the prompt. There is a difference between a refusal and nobody being there to ask:
an answer that is not yes is a refusal, while stdin ending without any answer at all means the
command is running unattended. Both exit 7, but the unattended case says to pass `--yes`, because
a bare "canceled" on a CI runner explains nothing.

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
anexia: invalid usage: unknown command "lcoation" for "anexia core"
anexia: invalid usage: --limit 0 must be between 1 and 1000
```

A name that used to work gets its replacement rather than that message. The commands are hidden,
so they do not show up in help or completion as if they still worked:

```
anexia: invalid usage: "location" has moved, use "anexia core location list" instead
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

That shape only exists when the library can parse the response body. It builds the error by
decoding the body first and discards the response when that fails, so a bodyless 404 or an HTML
403 from a proxy would arrive with no status at all and land on exit 1 with a message about JSON
syntax. `internal/anx` wraps the legacy client's transport to substitute a minimal JSON body in
that case, which keeps classification a property of the status rather than of the Engine's
wording.

Engine validation detail survives into the message. A 422 that names the offending fields renders
them, because "validation failed" alone does not tell the user what to change.

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
plural alias, an exit code that depended on the client, a `--all` flag present on one half only, a
`--all` walk that truncated on one half only, and a filter value with a space in it that worked on
one half and returned a 400 on the other. Sharing a helper is how that stops happening.

That last one needs the hand-written half to do something the registry does not. The legacy clients
build their URLs by interpolating values into a format string, so the CLI escapes every value it
hands them, filters and identifiers alike. Skipping it does not just break a value with a space: an
ampersand in a filter becomes extra query parameters, and a question mark in an identifier ends the
path, so the request addresses a different object and overrides the flags the user passed. On a
delete that means removing the wrong tag and reporting success.

Query and path escaping are not interchangeable. A query escaper writes a space as a plus, which a
path reader takes literally, so identifiers go through the path escaper and filters through the
query one.

Escaping is not only the legacy half's problem. The generic client escapes what it puts in a query,
but it assembles a path from the object's own fields and joins the segments, which collapses a
relative one. So a value the CLI hands either client as part of a path has to be escaped for its own
segment and refused if it names nothing, which is why that check sits in the shared code rather than
in the hand-written commands.

Paging is the one place where the two halves cannot share an implementation. The legacy clients
discard every byte of page metadata before returning, so `FetchPages` has only the page contents
to work from. They still agree on everything observable: both walk from an arbitrary start page,
both stop on an empty page, both give up after a thousand pages with the same message.

Anything that must hold for every command belongs in one place rather than in each command.
`--timeout` and `-o` are validated in the root's `PersistentPreRunE`, which is why `-o xml` is
rejected even by a command that renders no object and so never builds a writer. Per-command
validation would have to be remembered 12 times and was in fact forgotten 3 times.

The conformance test checks the rules that a registry cannot: verb vocabulary for both command
names and aliases, singular nouns carrying a plural alias that resolves back to the same command,
help text present and not ending in a period, groups printing help and rejecting arguments,
argument validators present, positional arguments documented, paging flags present on collection
lists and absent from relation lists, every output format accepted and a bad one rejected on every
Engine command, error messages reading as action then cause, flag naming, and no local flag
shadowing a global one.

Two things make the difference between a conformance test and a test that passes for the wrong
reason. It has to reach the behavior: the error-shape and output-format checks drive each command
with arguments derived from its own `Args` validator and flag set, and fail if a command never got
past parsing, because a test that only ever observes `invalid usage:` proves nothing. And it has to
check the value, not just its presence: asserting a noun has some alias passes on the wrong alias,
and so does resolving that alias through the tree, since cobra resolves whatever alias it was
given. Only comparing it against the noun catches a typo. Both of those were live defects here, and
every check in the file has been confirmed to fail against a deliberate violation, which is the
third thing: a check nobody has seen fail is a guess.

A fourth thing decides how much a check is worth later: what it selects. The two checks that need a
server used to walk commands whose path started with `anexia core`, which meant the first command in
a new group would land outside them and neither would say a word. They now name the groups that do
not reach the Engine and take everything else, so a new group is covered the day it appears and
adding a local one is a deliberate edit.

Two rules are checked by targeted tests rather than by walking the tree, because they need a
server to observe: `--all` requesting every page exactly once from any starting page, and
terminating against an Engine that caps page size, omits paging metadata, reports a wrong total,
rejects the page after the last, or never stops (`internal/resource`); and the exit-code table
holding on both clients whether or not the Engine repeats its status in the response body, or
sends a body at all (`internal/cli`).
