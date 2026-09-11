package cli

import (
	"strconv"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	kubernetesv1 "go.anx.io/go-anxcloud/pkg/apis/kubernetes/v1"
	"go.anx.io/go-anxcloud/pkg/utils/pointer"

	"github.com/ProbstenHias/anexia-cli/internal/errmap"
	"github.com/ProbstenHias/anexia-cli/internal/resource"
)

// newKubernetesNodePoolCommand exposes node-pool lifecycle verbs. The library
// says node pools do not support updates, so update is intentionally absent.
func newKubernetesNodePoolCommand(opts *globalOptions) *cobra.Command {
	return resource.Command(opts, resource.Spec[kubernetesv1.NodePool, *kubernetesv1.NodePool]{
		Noun:   "node-pool",
		Short:  "Work with Kubernetes node pools",
		List:   true,
		Get:    true,
		Delete: true,
		Identify: func(nodePool *kubernetesv1.NodePool, identifier string) {
			nodePool.Identifier = identifier
		},
		CreatePayload: nodePoolCreateFlags,
		Filters: func(flags *pflag.FlagSet) func(*kubernetesv1.NodePool) {
			cluster := flags.String("cluster", "", "only list node pools in this cluster")
			return func(nodePool *kubernetesv1.NodePool) {
				if *cluster != "" {
					nodePool.Cluster.Identifier = *cluster
				}
			}
		},
		Columns: []resource.Column[kubernetesv1.NodePool]{
			{Name: "identifier", Value: func(nodePool *kubernetesv1.NodePool) string { return nodePool.Identifier }},
			{Name: "name", Value: func(nodePool *kubernetesv1.NodePool) string { return nodePool.Name }},
			{Name: "cluster", Value: func(nodePool *kubernetesv1.NodePool) string {
				if nodePool.Cluster.Name != "" {
					return nodePool.Cluster.Name
				}
				return nodePool.Cluster.Identifier
			}},
			{Name: "replicas", Value: func(nodePool *kubernetesv1.NodePool) string {
				if nodePool.Replicas == nil {
					return ""
				}
				return strconv.Itoa(*nodePool.Replicas)
			}},
			{Name: "cpus", Value: func(nodePool *kubernetesv1.NodePool) string { return strconv.Itoa(nodePool.CPUs) }},
			{Name: "memory", Value: func(nodePool *kubernetesv1.NodePool) string { return gibibytes(nodePool.Memory) }},
			{Name: "disk", Value: func(nodePool *kubernetesv1.NodePool) string { return gibibytes(nodePool.DiskSize) }},
			{Name: "state", Value: func(nodePool *kubernetesv1.NodePool) string {
				if nodePool.State.Text != "" {
					return nodePool.State.Text
				}
				return nodePool.State.ID
			}},
		},
	})
}

// nodePoolCreateFlags validates human-sized resource values and converts them
// to the byte representation required by the Engine.
func nodePoolCreateFlags(flags *pflag.FlagSet) func(*kubernetesv1.NodePool) error {
	name := flags.String("name", "", "node pool name")
	cluster := flags.String("cluster", "", "cluster identifier")
	cpus := flags.Int("cpus", 0, "CPU cores per node")
	memory := flags.Int("memory", 0, "memory per node in GiB")
	disk := flags.Int("disk", 0, "disk per node in GiB")
	replicas := flags.Int("replicas", 0, "number of node replicas")
	operatingSystem := flags.String("operating-system", string(kubernetesv1.FlatcarLinux), "operating system for nodes")

	return func(nodePool *kubernetesv1.NodePool) error {
		if *name == "" {
			return errmap.Usagef("--name is required")
		}
		if *cluster == "" {
			return errmap.Usagef("--cluster is required")
		}
		if *cpus <= 0 {
			return errmap.Usagef("--cpus must be greater than zero")
		}
		if *memory <= 0 {
			return errmap.Usagef("--memory must be greater than zero")
		}
		if *disk <= 0 {
			return errmap.Usagef("--disk must be greater than zero")
		}

		nodePool.Name = *name
		nodePool.Cluster.Identifier = *cluster
		nodePool.CPUs = *cpus
		nodePool.Memory = *memory * (1 << 30)
		nodePool.DiskSize = *disk * (1 << 30)
		nodePool.OperatingSystem = kubernetesv1.OperatingSystem(*operatingSystem)
		if flags.Changed("replicas") {
			nodePool.Replicas = pointer.Int(*replicas)
		}
		return nil
	}
}

// gibibytes renders a byte value using the CLI's GiB display unit.
func gibibytes(bytes int) string {
	return strconv.Itoa(bytes/(1<<30)) + "Gi"
}
