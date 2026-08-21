// Package resource turns a declarative description of an Engine object into a
// cobra command carrying the CLI's standard verbs, so every resource behaves
// the same way without repeating flag wiring and rendering per noun.
package resource

import (
	"context"
	"io"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"go.anx.io/go-anxcloud/pkg/api"
	"go.anx.io/go-anxcloud/pkg/api/types"
	"go.anx.io/go-anxcloud/pkg/client"

	"github.com/ProbstenHias/anexia-cli/internal/errmap"
	"github.com/ProbstenHias/anexia-cli/internal/output"
)

// MaxLimit is the largest page size the CLI accepts.
const MaxLimit = 1000

// Env is what commands need from the CLI's global options. The cli package
// implements it, which keeps this package free of flag-parsing concerns.
type Env interface {
	// Writer builds an output writer for the requested format.
	Writer(out io.Writer) (*output.Writer, error)
	// API builds the generic go-anxcloud client.
	API(flags *pflag.FlagSet) (api.API, error)
	// Client builds the legacy go-anxcloud client.
	Client(flags *pflag.FlagSet) (client.Client, error)
	// Context derives a request context honoring --timeout.
	Context(parent context.Context) (context.Context, context.CancelFunc)
	// Fail annotates an error before it reaches the user.
	Fail(err error) error
	// AssumeYes reports whether --yes was passed.
	AssumeYes() bool
}

// Pointer constrains PO to be the pointer type of O that implements the
// go-anxcloud object interface. Generic API objects declare their methods on
// the pointer receiver, so both type parameters are needed: O to hold values
// in slices, PO to talk to the client.
type Pointer[O any] interface {
	*O
	types.Object
}

// Column projects one field of an object onto a tabular output column.
type Column[O any] struct {
	// Name is the column header, lowercase.
	Name string
	// Value renders the column for one object.
	Value func(*O) string
}

// Spec declares a resource: its command name, how it renders, and which verbs
// it supports.
//
// Only the read verbs exist so far, because every resource the CLI reaches
// today is read-only in the Engine. create, update and delete belong here too
// and land with the first resource that needs them, along with the --wait
// handling their asynchronous cousins require.
type Spec[O any, PO Pointer[O]] struct {
	// Noun is the command name, singular and lowercase.
	Noun string
	// Short is the one-line command description.
	Short string
	// Plural names the resource in prose such as "no locations found",
	// defaulting to Noun with an s appended.
	Plural string

	// Columns render objects in the table and tsv formats.
	Columns []Column[O]

	// List enables "<noun> list".
	List bool
	// Get enables "<noun> get <id>", and requires Identify.
	Get bool

	// Identify writes a positional identifier into an empty object so the
	// single-object verbs can address it.
	Identify func(*O, string)

	// Filters registers list-only flags and returns a hook applying them to
	// the filter object.
	Filters func(*pflag.FlagSet) func(*O)
}

// identify returns a fresh object addressed by id, or a usage error when the
// resource cannot be addressed by identifier.
func (s Spec[O, PO]) identify(id string) (PO, error) {
	if s.Identify == nil {
		return nil, errmap.Usagef("%s cannot be addressed by identifier", s.Noun)
	}

	var obj O
	s.Identify(&obj, id)

	return PO(&obj), nil
}

// plural names the resource in prose, defaulting to the noun with an s.
func (s Spec[O, PO]) plural() string {
	if s.Plural != "" {
		return s.Plural
	}

	return s.Noun + "s"
}

// Command builds the noun command with every verb the spec enables. Callers
// can attach further subcommands to the result, which is how sub-nouns such
// as "resource tag" are wired.
func Command[O any, PO Pointer[O]](env Env, spec Spec[O, PO]) *cobra.Command {
	cmd := Noun(spec.Noun, spec.plural(), spec.Short)

	if spec.List {
		cmd.AddCommand(newListCommand(env, spec))
	}

	if spec.Get {
		cmd.AddCommand(newGetCommand(env, spec))
	}

	return cmd
}

// Group builds a command that only groups children, such as "core", printing
// help when invoked bare.
func Group(use, short string, children ...*cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(children...)

	return cmd
}

// Noun builds the command holding a resource's verbs. It is Group plus the
// plural alias every noun carries, so "core locations list" reaches the same
// command as "core location list". Nouns written against the legacy client
// call this instead of Command to inherit the alias.
func Noun(noun, plural, short string, children ...*cobra.Command) *cobra.Command {
	cmd := Group(noun, short, children...)
	cmd.Aliases = []string{plural}

	return cmd
}
