package cli

import (
	"fmt"
	"io"
	"net/url"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"go.anx.io/go-anxcloud/pkg/apis/common"
	corev1 "go.anx.io/go-anxcloud/pkg/apis/core/v1"
	kubernetesv1 "go.anx.io/go-anxcloud/pkg/apis/kubernetes/v1"
	"go.anx.io/go-anxcloud/pkg/utils/pointer"

	"github.com/ProbstenHias/anexia-cli/internal/confirm"
	"github.com/ProbstenHias/anexia-cli/internal/errmap"
	"github.com/ProbstenHias/anexia-cli/internal/resource"
)

// newKubernetesCommand groups cluster and node-pool operations under one
// command tree so both resources use the standard Engine verbs.
func newKubernetesCommand(opts *globalOptions) *cobra.Command {
	return resource.Group("kubernetes", "Kubernetes clusters and node pools",
		newKubernetesClusterCommand(opts),
		newKubernetesNodePoolCommand(opts),
	)
}

// newKubernetesClusterCommand exposes the lifecycle supported by the library.
// Clusters do not support updates in go-anxcloud, so no update verb is added.
func newKubernetesClusterCommand(opts *globalOptions) *cobra.Command {
	cmd := resource.Command(opts, resource.Spec[kubernetesv1.Cluster, *kubernetesv1.Cluster]{
		Noun:   "cluster",
		Short:  "Work with Kubernetes clusters",
		List:   true,
		Get:    true,
		Delete: true,
		Identify: func(cluster *kubernetesv1.Cluster, identifier string) {
			cluster.Identifier = identifier
		},
		CreatePayload: clusterCreateFlags,
		// The library says the resource does not support updates.
		Columns: []resource.Column[kubernetesv1.Cluster]{
			{Name: "identifier", Value: func(cluster *kubernetesv1.Cluster) string { return cluster.Identifier }},
			{Name: "name", Value: func(cluster *kubernetesv1.Cluster) string { return cluster.Name }},
		},
	})
	cmd.AddCommand(newKubernetesKubeconfigCommand(opts))
	return cmd
}

// clusterCreateFlags builds the cluster payload and leaves omitted boolean
// settings unset so the Engine can apply its own defaults.
func clusterCreateFlags(flags *pflag.FlagSet) func(*kubernetesv1.Cluster) error {
	name := flags.String("name", "", "cluster name")
	location := flags.String("location", "", "location identifier where the cluster is created")
	version := flags.String("version", "", "Kubernetes version, empty uses the Engine default")
	needsServiceVMs := flags.Bool("needs-service-vms", false, "create Service VMs")
	natGateways := flags.Bool("enable-nat-gateways", false, "enable NAT gateways")
	lbaas := flags.Bool("enable-lbaas", false, "enable LBaaS")
	autoscaling := flags.Bool("enable-autoscaling", false, "enable autoscaling")
	internalPrefix := flags.String("internal-ipv4-prefix", "", "existing prefix identifier; turns off automatic management of this prefix")
	externalPrefix := flags.String("external-ipv4-prefix", "", "existing prefix identifier; turns off automatic management of this prefix")
	externalIPv6Prefix := flags.String("external-ipv6-prefix", "", "existing prefix identifier; turns off automatic management of this prefix")
	allowlist := flags.String("api-server-allowlist", "", "space-separated CIDRs allowed to access the API server")

	return func(cluster *kubernetesv1.Cluster) error {
		if *name == "" {
			return errmap.Usagef("--name is required")
		}
		if *location == "" {
			return errmap.Usagef("--location is required")
		}

		cluster.Name = *name
		cluster.Location = corev1.Location{Identifier: *location}
		cluster.Version = *version
		cluster.ApiServerAllowlist = *allowlist
		if flags.Changed("needs-service-vms") {
			cluster.NeedsServiceVMs = pointer.Bool(*needsServiceVMs)
		}
		if flags.Changed("enable-nat-gateways") {
			cluster.EnableNATGateways = pointer.Bool(*natGateways)
		}
		if flags.Changed("enable-lbaas") {
			cluster.EnableLBaaS = pointer.Bool(*lbaas)
		}
		if flags.Changed("enable-autoscaling") {
			cluster.EnableAutoscaling = pointer.Bool(*autoscaling)
		}
		if *internalPrefix != "" {
			cluster.InternalIPv4Prefix = &common.PartialResource{Identifier: *internalPrefix}
			cluster.ManageInternalIPv4Prefix = pointer.Bool(false)
		}
		if *externalPrefix != "" {
			cluster.ExternalIPv4Prefix = &common.PartialResource{Identifier: *externalPrefix}
			cluster.ManageExternalIPv4Prefix = pointer.Bool(false)
		}
		if *externalIPv6Prefix != "" {
			cluster.ExternalIPv6Prefix = &common.PartialResource{Identifier: *externalIPv6Prefix}
			cluster.ManageExternalIPv6Prefix = pointer.Bool(false)
		}
		return nil
	}
}

// newKubernetesKubeconfigCommand adds the document operations under a cluster.
func newKubernetesKubeconfigCommand(opts *globalOptions) *cobra.Command {
	return resource.Noun("kubeconfig", "kubeconfigs", "Manage the kubeconfig of a cluster",
		newKubernetesKubeconfigGetCommand(opts),
		newKubernetesKubeconfigDeleteCommand(opts),
	)
}

// newKubernetesKubeconfigGetCommand prints the document verbatim after the
// library requests and polls for it when the cluster has none.
func newKubernetesKubeconfigGetCommand(opts *globalOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <cluster-id>",
		Short: "Get a cluster kubeconfig",
		Long:  "Get a cluster kubeconfig. This triggers the Engine's request-kubeconfig rule and polls until the kubeconfig appears, bounded by --timeout.",
		Args:  cobra.ExactArgs(1),
	}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := validateKubeconfigCluster(args[0]); err != nil {
			return err
		}
		apiClient, err := opts.API(cmd.Flags())
		if err != nil {
			return err
		}
		ctx, cancel := opts.Context(cmd.Context())
		defer cancel()
		config, err := kubernetesv1.GetKubeConfig(ctx, apiClient, args[0])
		if err != nil {
			return opts.Fail(fmt.Errorf("reading kubeconfig of cluster %q: %w", args[0], err))
		}
		if _, err := io.WriteString(cmd.OutOrStdout(), config); err != nil {
			return fmt.Errorf("writing kubeconfig: %w", err)
		}
		return nil
	}
	return cmd
}

// validateKubeconfigCluster is stricter than ValidateIdentifier because
// go-anxcloud interpolates the cluster ID into the kubeconfig rule URL.
func validateKubeconfigCluster(id string) error {
	if err := resource.ValidateIdentifier("cluster", id); err != nil {
		return err
	}
	if url.PathEscape(id) != id {
		return errmap.Usagef("invalid cluster identifier %q", id)
	}
	return nil
}

// newKubernetesKubeconfigDeleteCommand confirms before firing the remove rule.
func newKubernetesKubeconfigDeleteCommand(opts *globalOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "delete <cluster-id>",
		Aliases: []string{"destroy"},
		Short:   "Delete a cluster kubeconfig",
		Args:    cobra.ExactArgs(1),
	}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := validateKubeconfigCluster(args[0]); err != nil {
			return err
		}
		question := fmt.Sprintf("delete kubeconfig of cluster %q", args[0])
		if err := confirm.Prompt(cmd.InOrStdin(), cmd.ErrOrStderr(), question, opts.AssumeYes()); err != nil {
			return err
		}
		apiClient, err := opts.API(cmd.Flags())
		if err != nil {
			return err
		}
		ctx, cancel := opts.Context(cmd.Context())
		defer cancel()
		if err := kubernetesv1.RemoveKubeConfig(ctx, apiClient, args[0]); err != nil {
			return opts.Fail(fmt.Errorf("deleting kubeconfig of cluster %q: %w", args[0], err))
		}
		if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "deleted kubeconfig of cluster %s\n", args[0]); err != nil {
			return fmt.Errorf("writing status: %w", err)
		}
		return nil
	}
	return cmd
}
