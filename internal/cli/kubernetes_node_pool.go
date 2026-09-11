package cli

import (
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	kubernetesv1 "go.anx.io/go-anxcloud/pkg/apis/kubernetes/v1"
	"go.anx.io/go-anxcloud/pkg/utils/pointer"

	"github.com/ProbstenHias/anexia-cli/internal/errmap"
	"github.com/ProbstenHias/anexia-cli/internal/resource"
)

const (
	gibibyte     = 1 << 30
	minCPUs      = 1
	maxCPUs      = 16
	minMemoryGiB = 2
	maxMemoryGiB = 64
	minDiskGiB   = 20
	maxDiskGiB   = 1600
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

	return func(nodePool *kubernetesv1.NodePool) error {
		if *name == "" {
			return errmap.Usagef("--name is required")
		}
		if *cluster == "" {
			return errmap.Usagef("--cluster is required")
		}
		if *cpus < minCPUs || *cpus > maxCPUs {
			return errmap.Usagef("--cpus must be between %d and %d", minCPUs, maxCPUs)
		}
		if *memory < minMemoryGiB || *memory > maxMemoryGiB {
			return errmap.Usagef("--memory must be between %d and %d", minMemoryGiB, maxMemoryGiB)
		}
		if *disk < minDiskGiB || *disk > maxDiskGiB {
			return errmap.Usagef("--disk must be between %d and %d", minDiskGiB, maxDiskGiB)
		}

		nodePool.Name = *name
		nodePool.Cluster.Identifier = *cluster
		nodePool.CPUs = *cpus
		nodePool.Memory = *memory * gibibyte
		nodePool.DiskSize = *disk * gibibyte
		if flags.Changed("replicas") {
			nodePool.Replicas = pointer.Int(*replicas)
		}
		return nil
	}
}
