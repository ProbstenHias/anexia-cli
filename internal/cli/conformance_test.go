package cli_test

import (
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ProbstenHias/anexia-cli/internal/cli"
	"github.com/ProbstenHias/anexia-cli/internal/errmap"
	"github.com/ProbstenHias/anexia-cli/internal/output"
)

// verbs are the only command names allowed at a leaf of the command tree. The
// CLI's promise is that every resource is driven by the same small vocabulary,
// so a new verb must be added here deliberately rather than by accident.
var verbs = map[string]bool{
	// The five standard verbs every resource supports as far as the Engine
	// allows.
	"list":   true,
	"get":    true,
	"create": true,
	"update": true,
	"delete": true,

	// Sub-noun relation verbs, used where a resource owns a collection that
	// is not addressable on its own, such as a resource's tags.
	"add":    true,
	"remove": true,

	// destroy is accepted as an alias of delete for users coming from the
	// Engine's own vocabulary. It is never a command name.
	"destroy": true,

	// Local commands that talk to the config file or the binary itself
	// rather than to the Engine.
	"path":    true,
	"init":    true,
	"set":     true,
	"view":    true,
	"version": true,
	"help":    true,

	// Cobra's generated shell-completion tree, which the CLI does not own.
	"completion": true,
}

// groupNames are the command names that only group children and are not
// nouns, so the singular rule does not apply to them. Engine API areas are
// named by Anexia, not by the CLI, and several are not singular words.
var groupNames = map[string]bool{
	"core":       true,
	"config":     true,
	"network":    true,
	"vsphere":    true,
	"kubernetes": true,
	"lbaas":      true,
	"dns":        true,
	"e5e":        true,
	"frontier":   true,
	"storage":    true,
}

// singular flags whether a name reads as a singular noun. Plurals are only
// allowed as aliases, never as the command name, so "anexia core location get"
// reads correctly.
func singular(name string) bool {
	return !strings.HasSuffix(name, "s") || strings.HasSuffix(name, "ss")
}

// plural reports whether alias is the plural of noun. English is only handled
// as far as the Engine's own vocabulary needs: a trailing "s", or "es" after a
// sibilant, which covers "address" and "prefix". An irregular plural has to be
// added here deliberately, which is the point.
func plural(noun, alias string) bool {
	for _, suffix := range []string{"s", "x", "z", "ch", "sh"} {
		if strings.HasSuffix(noun, suffix) {
			return alias == noun+"es"
		}
	}

	return alias == noun+"s"
}

// walk visits cmd and every descendant, skipping cobra's completion subtree
// and any hidden command.
//
// Hidden commands are tombstones for names that have moved. They exist only to
// point at a replacement, so the naming and verb rules do not apply to them.
func walk(cmd *cobra.Command, visit func(*cobra.Command)) {
	if cmd.Name() == "completion" || cmd.Hidden {
		return
	}

	visit(cmd)

	for _, child := range cmd.Commands() {
		walk(child, visit)
	}
}

// commands returns every command in the tree except the root.
func commands(t *testing.T) []*cobra.Command {
	t.Helper()

	_, found := tree(t)

	return found
}

// tree returns a root command and every command below it, so tests that need
// to resolve a path back to a command compare pointers from the same tree.
func tree(t *testing.T) (root *cobra.Command, found []*cobra.Command) {
	t.Helper()

	root = cli.NewRootCommand(cli.Deps{})

	walk(root, func(cmd *cobra.Command) {
		if cmd.HasParent() {
			found = append(found, cmd)
		}
	})

	require.NotEmpty(t, found)

	return root, found
}

// path renders a command's full invocation path for test failure messages.
func path(cmd *cobra.Command) string {
	return cmd.CommandPath()
}

func TestConformanceLeavesUseKnownVerbs(t *testing.T) {
	t.Parallel()

	for _, cmd := range commands(t) {
		if cmd.HasSubCommands() {
			continue
		}

		assert.True(t, verbs[cmd.Name()], "%s: %q is not a known verb, add it to the conformance list deliberately", path(cmd), cmd.Name())
	}
}

// TestConformanceLeafAliasesUseKnownVerbs closes the gap the verb check leaves:
// an alias is another name for the command, so "rm" or "describe" smuggled in
// as an alias would reintroduce vocabulary the design rules out.
func TestConformanceLeafAliasesUseKnownVerbs(t *testing.T) {
	t.Parallel()

	checked := 0

	for _, cmd := range commands(t) {
		if cmd.HasSubCommands() {
			continue
		}

		for _, alias := range cmd.Aliases {
			assert.True(t, verbs[alias],
				"%s: alias %q is not a known verb, add it to the conformance list deliberately", path(cmd), alias)

			checked++
		}
	}

	// Without this the test passes when the tree has no leaf alias at all,
	// which would make it look like the rule is enforced when it is not.
	assert.Positive(t, checked, "the tree should have leaf aliases to check")
}

func TestConformanceNounsAreSingular(t *testing.T) {
	t.Parallel()

	for _, cmd := range commands(t) {
		if !cmd.HasSubCommands() || groupNames[cmd.Name()] {
			continue
		}

		assert.True(t, singular(cmd.Name()), "%s: noun names must be singular, plurals belong in Aliases", path(cmd))
	}
}

// TestConformanceNounsCarryAPluralAlias checks the other half of the singular
// rule: a user who reaches for the plural must land on the same command. The
// alias is resolved through the tree rather than compared to a computed plural,
// so an irregular plural still passes but a wrong alias does not.
func TestConformanceNounsCarryAPluralAlias(t *testing.T) {
	t.Parallel()

	root, found := tree(t)

	checked := 0

	for _, cmd := range found {
		if !cmd.HasSubCommands() || groupNames[cmd.Name()] {
			continue
		}

		require.NotEmpty(t, cmd.Aliases, "%s: a noun must accept its plural as an alias", path(cmd))

		// Swap the noun for each alias in its own invocation path and
		// confirm the tree still lands on the same command.
		parts := strings.Fields(strings.TrimPrefix(path(cmd), "anexia "))

		for _, alias := range cmd.Aliases {
			aliased := make([]string, len(parts))
			copy(aliased, parts)
			aliased[len(aliased)-1] = alias

			resolved, _, err := root.Find(aliased)

			require.NoError(t, err, "%s: alias %q does not resolve", path(cmd), alias)
			assert.Same(t, cmd, resolved, "%s: alias %q resolves to %s instead", path(cmd), alias, path(resolved))

			// Resolution alone proves nothing: cobra matches whatever alias
			// is registered, so any string would resolve. The alias has to
			// actually read as the plural of the noun.
			assert.True(t, plural(cmd.Name(), alias),
				"%s: alias %q is not the plural of %q", path(cmd), alias, cmd.Name())

			checked++
		}
	}

	assert.Positive(t, checked, "the tree should have nouns to check")
}

// TestConformanceRelationListsOmitPagingFlags is the counterpart to the
// collection-list check: paging flags that do nothing must not be offered.
func TestConformanceRelationListsOmitPagingFlags(t *testing.T) {
	t.Parallel()

	checked := 0

	for _, cmd := range commands(t) {
		if cmd.Name() != "list" || !takesPositionalArgs(cmd) {
			continue
		}

		checked++

		for _, name := range []string{"page", "limit", "all"} {
			assert.Nil(t, cmd.Flags().Lookup(name),
				"%s: a relation list has nothing to page over, so --%s must not exist", path(cmd), name)
		}
	}

	// Without this a regression in takesPositionalArgs would empty the loop
	// and silently disable the rule.
	assert.Positive(t, checked, "the tree should have relation lists to check")
}

// invocation builds a command line that satisfies cmd's own argument and flag
// requirements, so the command reaches the Engine instead of failing to parse.
// Requirements are read off the command rather than guessed, which keeps this
// working as the tree grows.
func invocation(cmd *cobra.Command) []string {
	args := strings.Fields(strings.TrimPrefix(cmd.CommandPath(), "anexia "))

	// A command without a validator accepts any count, including zero, so
	// probing would pick zero and the command would then index the arguments
	// it documents and panic, taking the whole test binary with it. Use what
	// Use documents instead, and let the test that owns that rule report the
	// missing validator.
	if cmd.Args == nil {
		for range strings.Count(cmd.Use, "<") {
			args = append(args, "placeholder")
		}
	}

	for n := range 4 {
		if cmd.Args == nil {
			break
		}

		if cmd.ValidateArgs(make([]string, n)) == nil {
			for range n {
				args = append(args, "placeholder")
			}

			break
		}
	}

	// Flags a command requires to get past its own validation. Only those
	// it actually defines are passed, so no command sees an unknown flag.
	for _, name := range []string{"name", "service"} {
		if cmd.Flags().Lookup(name) != nil {
			args = append(args, "--"+name, "placeholder")
		}
	}

	return args
}

// TestConformanceErrorMessagesReadAsActionThenCause pins the documented error
// shape against a real Engine failure. Every command has to name the action it
// was performing before the cause, so the message reads as a sentence.
func TestConformanceErrorMessagesReadAsActionThenCause(t *testing.T) {
	// isolate sets environment variables, so this test cannot be parallel.
	configPath := isolate(t)

	srv, _ := server(t, http.StatusInternalServerError, `{"error":{"code":500,"message":"boom"}}`)

	require.NoError(t, os.WriteFile(configPath,
		[]byte("token: tok\napi_base_url: "+srv.URL+"\n"), 0o600))

	checked := 0

	for _, cmd := range commands(t) {
		if cmd.HasSubCommands() || !strings.HasPrefix(cmd.CommandPath(), "anexia core ") {
			continue
		}

		full := cmd.CommandPath()

		_, _, err := runWithInput(t, "y\n", invocation(cmd)...)
		require.Error(t, err, "%s: expected the Engine failure to surface", full)

		message := errmap.Message(err)

		require.False(t, strings.HasPrefix(message, "invalid usage:"),
			"%s: %q never reached the Engine, so the error shape is untested", full, message)

		action, _, found := strings.Cut(message, ": ")
		require.True(t, found, "%s: %q must read as \"<action>: <cause>\"", full, message)

		// The action names what the command was doing, so it reads as a
		// gerund. Without this the check passes on any cause that happens
		// to contain a colon, with no action prefix at all.
		assert.True(t, strings.HasSuffix(strings.Fields(action)[0], "ing"),
			"%s: %q must open with the action, like \"listing tags: ...\"", full, message)

		assert.Equal(t, strings.ToLower(action[:1]), action[:1], "%s: %q must start lowercase", full, message)
		assert.False(t, strings.HasSuffix(message, "."), "%s: %q must not end in a period", full, message)
		assert.NotContains(t, message, "received error from api",
			"%s: %q leaks the legacy client's struct dump", full, message)

		checked++
	}

	assert.Positive(t, checked, "the tree should have engine commands to check")
}

func TestConformanceEveryCommandHasShortHelp(t *testing.T) {
	t.Parallel()

	for _, cmd := range commands(t) {
		assert.NotEmpty(t, cmd.Short, "%s: needs a Short description", path(cmd))
		assert.False(t, strings.HasSuffix(cmd.Short, "."), "%s: Short must not end in a period", path(cmd))
	}
}

func TestConformanceGroupsPrintHelpAndTakeNoArgs(t *testing.T) {
	t.Parallel()

	for _, cmd := range commands(t) {
		if !cmd.HasSubCommands() {
			continue
		}

		assert.NotNil(t, cmd.RunE, "%s: a group must print help instead of erroring", path(cmd))

		// A group must reject arguments rather than guess a default verb,
		// so a mistyped subcommand is reported instead of printing help
		// and exiting zero.
		assert.Error(t, cmd.ValidateArgs([]string{"bogus"}),
			"%s: a group must reject arguments so an unknown subcommand is an error", path(cmd))

		stdout, _, err := run(t, strings.Fields(strings.TrimPrefix(path(cmd), "anexia "))...)

		require.NoError(t, err, "%s: bare invocation must succeed", path(cmd))
		assert.Contains(t, stdout, "Usage:", "%s: bare invocation must print help", path(cmd))
	}
}

func TestConformanceLeavesValidateArgumentCount(t *testing.T) {
	t.Parallel()

	for _, cmd := range commands(t) {
		if cmd.HasSubCommands() || cmd.Name() == "help" {
			continue
		}

		assert.NotNil(t, cmd.Args, "%s: needs an Args validator so stray arguments are rejected", path(cmd))
	}
}

// takesPositionalArgs reports whether cmd accepts at least one positional
// argument, probing counts because validators such as ExactArgs(2) reject a
// single argument.
//
// A command without a validator is judged by what Use documents, so the
// missing validator is reported by the test that owns that rule rather than
// turning into a second, misleading failure about undocumented arguments.
func takesPositionalArgs(cmd *cobra.Command) bool {
	if cmd.Args == nil {
		return strings.Contains(cmd.Use, "<")
	}

	for n := 1; n <= 3; n++ {
		if cmd.ValidateArgs(make([]string, n)) == nil {
			return true
		}
	}

	return false
}

func TestConformancePositionalArgumentsAreDocumented(t *testing.T) {
	t.Parallel()

	for _, cmd := range commands(t) {
		if cmd.HasSubCommands() || cmd.Name() == "help" {
			continue
		}

		// A leaf taking positional arguments must name them in Use so the
		// help output shows what to pass, and a leaf that names them must
		// accept them.
		assert.Equal(t, takesPositionalArgs(cmd), strings.Contains(cmd.Use, "<"),
			"%s: Use must name every positional argument the command accepts, like \"get <id>\"", path(cmd))
	}
}

func TestConformanceCollectionListsSharePagingFlags(t *testing.T) {
	t.Parallel()

	seen := 0

	for _, cmd := range commands(t) {
		// Relation lists such as "resource tag list <resource-id>" read
		// their items out of the parent object, so the Engine offers
		// nothing to page over. Only collection lists are checked.
		if cmd.Name() != "list" || takesPositionalArgs(cmd) {
			continue
		}

		seen++

		for _, name := range []string{"page", "limit", "all"} {
			assert.NotNil(t, cmd.Flags().Lookup(name), "%s: every collection list must accept --%s", path(cmd), name)
		}
	}

	assert.Positive(t, seen, "the tree should have collection lists to check")
}

func TestConformanceFlagNamesAreLowercaseKebabCase(t *testing.T) {
	t.Parallel()

	for _, cmd := range commands(t) {
		cmd.Flags().VisitAll(func(f *pflag.Flag) {
			assert.Equal(t, strings.ToLower(f.Name), f.Name, "%s: flag --%s must be lowercase", path(cmd), f.Name)
			assert.NotContains(t, f.Name, "_", "%s: flag --%s must use dashes, not underscores", path(cmd), f.Name)
			assert.NotEmpty(t, f.Usage, "%s: flag --%s needs a usage string", path(cmd), f.Name)
		})
	}
}

func TestConformanceNoCommandShadowsAGlobalFlag(t *testing.T) {
	t.Parallel()

	root := cli.NewRootCommand(cli.Deps{})

	globals := map[string]bool{}
	root.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		globals[f.Name] = true
	})

	require.NotEmpty(t, globals)

	walk(root, func(cmd *cobra.Command) {
		cmd.LocalNonPersistentFlags().VisitAll(func(f *pflag.Flag) {
			assert.False(t, globals[f.Name], "%s: local flag --%s shadows a global flag", path(cmd), f.Name)
		})
	})
}

// TestConformanceEveryEngineCommandSupportsEveryOutputFormat walks every leaf
// that talks to the Engine, because a command that renders nothing can still
// accept a bogus -o and ignore it, which is only visible by trying each one.
//
// This proves acceptance and rejection, not that a command renders in the
// format it accepted. Rendering is asserted per command in core_test.go.
func TestConformanceEveryEngineCommandSupportsEveryOutputFormat(t *testing.T) {
	// isolate sets environment variables, so this test cannot be parallel.
	isolate(t)

	checked := 0

	for _, cmd := range commands(t) {
		if cmd.HasSubCommands() || !strings.HasPrefix(cmd.CommandPath(), "anexia core ") {
			continue
		}

		full := cmd.CommandPath()

		for _, format := range append(output.Formats, "xml") {
			args := append(invocation(cmd), "-o", string(format))

			_, _, err := runWithInput(t, "y\n", args...)
			require.Error(t, err, "%s: expected a failure without a token", full)

			if format == "xml" {
				// A rejected format must be reported before anything
				// else, even by a command that renders no object.
				assert.Equal(t, errmap.ExitUsage, errmap.ExitCode(err),
					"%s: -o %s must be rejected as a usage mistake, got %q", full, format, errmap.Message(err))

				continue
			}

			// Without a token every command fails on authentication, never
			// on the format, which proves the format was accepted.
			assert.Equal(t, errmap.ExitAuth, errmap.ExitCode(err),
				"%s: -o %s must be accepted, got %q", full, format, errmap.Message(err))
		}

		checked++
	}

	assert.Positive(t, checked, "the tree should have engine commands to check")
}
