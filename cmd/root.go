package cmd

import (
	"github.com/spf13/cobra"
)

var (
	kubeconfig string
	namespace  string
)

func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "kubectl-openclaw",
		Short: "Manage OpenClaw AI agent instances on Kubernetes",
		Long: `kubectl-openclaw is a kubectl plugin for managing OpenClaw AI agent instances.

It provides convenient commands for listing, inspecting, debugging, and
diagnosing OpenClawInstance custom resources and their managed child resources.

Usage:
  kubectl openclaw [command]

Examples:
  # List all instances
  kubectl openclaw list

  # Show detailed status of an instance
  kubectl openclaw status my-agent

  # Tail logs from an instance's pod
  kubectl openclaw logs my-agent

  # Run diagnostics
  kubectl openclaw doctor`,
		SilenceUsage: true,
	}

	cmd.PersistentFlags().StringVar(&kubeconfig, "kubeconfig", "", "path to kubeconfig file")
	cmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", "", "kubernetes namespace (defaults to current context namespace)")

	cmd.AddCommand(newListCmd())
	cmd.AddCommand(newStatusCmd())
	cmd.AddCommand(newLogsCmd())
	cmd.AddCommand(newDoctorCmd())
	cmd.AddCommand(newVersionCmd())

	return cmd
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the plugin version",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Printf("kubectl-openclaw %s\n", Version)
		},
	}
}

var Version = "dev"
