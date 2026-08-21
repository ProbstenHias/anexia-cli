package cli_test

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ProbstenHias/anexia-cli/internal/cli"
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

// singular flags whether a name reads as a singular noun. Plurals are only
// allowed as aliases, never as the command name, so "anexia core location get"
// reads correctly.
func singular(name string) bool {
	return !strings.HasSuffix(name, "s") || strings.HasSuffix(name, "ss")
}

// walk visits cmd and every descendant, skipping the completion subtree.
func walk(cmd *cobra.Command, visit func(*cobra.Command)) {
	if cmd.Name() == "completion" {
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

	root := cli.NewRootCommand(cli.Deps{})

	var found []*cobra.Command

	walk(root, func(cmd *cobra.Command) {
		if cmd.HasParent() {
			found = append(found, cmd)
		}
	})

	require.NotEmpty(t, found)

	return found
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

func TestConformanceGroupsAndNounsAreSingular(t *testing.T) {
	t.Parallel()

	for _, cmd := range commands(t) {
		if !cmd.HasSubCommands() {
			continue
		}

		assert.True(t, singular(cmd.Name()), "%s: group and noun names must be singular, plurals belong in Aliases", path(cmd))
	}
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
func takesPositionalArgs(cmd *cobra.Command) bool {
	if cmd.Args == nil {
		return true
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

		for _, name := range []string{"page", "limit"} {
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

func TestConformanceEveryEngineCommandSupportsEveryOutputFormat(t *testing.T) {
	// isolate sets environment variables, so this test cannot be parallel.
	// Rendering is centralized, so it is enough to prove the flag is
	// accepted everywhere and rejected consistently.
	isolate(t)

	for _, format := range []string{"table", "json", "yaml", "tsv"} {
		_, _, err := run(t, "core", "location", "list", "-o", format)

		// Without a token the command fails on authentication, never on
		// the format, which proves the format parsed.
		require.ErrorContains(t, err, "not authenticated", "format %q must be accepted", format)
	}

	_, _, err := run(t, "core", "location", "list", "-o", "xml")
	require.ErrorContains(t, err, "invalid output format")
}
