package cli

import (
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	corev1 "go.anx.io/go-anxcloud/pkg/apis/core/v1"

	"github.com/ProbstenHias/anexia-cli/internal/resource"
)

// newCoreCommand builds the "core" group, covering the Engine's cross-cutting
// objects: where things run, what exists, and how it is labeled.
func newCoreCommand(opts *globalOptions) *cobra.Command {
	return resource.Group("core", "Locations, resources, tags, and services",
		newCoreLocationCommand(opts),
		newCoreResourceCommand(opts),
		newCoreTagCommand(opts),
		newCoreServiceCommand(opts),
	)
}

// newCoreLocationCommand builds "core location". Locations are read-only in
// the Engine, so only list and get are wired.
func newCoreLocationCommand(opts *globalOptions) *cobra.Command {
	return resource.Command(opts, resource.Spec[corev1.Location, *corev1.Location]{
		Noun:    "location",
		Aliases: []string{"locations"},
		Short:   "Work with Anexia locations",
		List:    true,
		Get:     true,
		Identify: func(l *corev1.Location, id string) {
			l.Identifier = id
		},
		Columns: []resource.Column[corev1.Location]{
			{Name: "identifier", Value: func(l *corev1.Location) string { return l.Identifier }},
			{Name: "code", Value: func(l *corev1.Location) string { return l.Code }},
			{Name: "name", Value: func(l *corev1.Location) string { return l.Name }},
			{Name: "country", Value: func(l *corev1.Location) string { return l.CountryCode }},
			{Name: "city", Value: func(l *corev1.Location) string { return l.CityCode }},
		},
	})
}

// newCoreResourceCommand builds "core resource". Resources are the Engine's
// generic handle on anything provisioned, and are read-only apart from their
// tags, which get their own sub-noun.
func newCoreResourceCommand(opts *globalOptions) *cobra.Command {
	cmd := resource.Command(opts, resource.Spec[corev1.Resource, *corev1.Resource]{
		Noun:    "resource",
		Aliases: []string{"resources"},
		Short:   "Work with Anexia resources",
		List:    true,
		Get:     true,
		Identify: func(r *corev1.Resource, id string) {
			r.Identifier = id
		},
		Filters: func(flags *pflag.FlagSet) func(*corev1.Resource) {
			tag := flags.String("tag", "", "only list resources carrying this tag")

			return func(r *corev1.Resource) {
				if *tag != "" {
					r.Tags = []string{*tag}
				}
			}
		},
		Columns: []resource.Column[corev1.Resource]{
			{Name: "identifier", Value: func(r *corev1.Resource) string { return r.Identifier }},
			{Name: "name", Value: func(r *corev1.Resource) string { return r.Name }},
			{Name: "type", Value: func(r *corev1.Resource) string { return r.Type.Name }},
			{Name: "created", Value: func(r *corev1.Resource) string { return r.CreatedAt }},
		},
	})

	cmd.AddCommand(newCoreResourceTagCommand(opts))

	return cmd
}
