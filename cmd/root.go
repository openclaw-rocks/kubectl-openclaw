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

Tip: Set an alias for a natural CLI experience:
  alias claw="kubectl openclaw"

Lifecycle:
  create         Create a new instance
  delete         Delete an instance
  restart        Restart an instance
  upgrade        Upgrade to a new version

Inspection:
  list           List all instances
  status         Detailed instance status
  logs           Stream pod logs
  events         Show related Kubernetes events
  config         View or edit the configuration

Interaction:
  exec           Shell into an instance pod
  port-forward   Forward gateway and canvas ports locally
  open           Open the instance UI in your browser

Configuration:
  skills         Manage installed skills
  env            Manage environment variables
  enable         Enable a sidecar (chromium, tailscale, ollama, web-terminal)
  disable        Disable a sidecar

Operations:
  backup         View backup status
  restore        Restore from a backup
  doctor         Run diagnostic checks`,
		SilenceUsage: true,
	}

	cmd.PersistentFlags().StringVar(&kubeconfig, "kubeconfig", "", "path to kubeconfig file")
	cmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", "", "kubernetes namespace (defaults to current context namespace)")

	// Lifecycle
	cmd.AddCommand(newCreateCmd())
	cmd.AddCommand(newDeleteCmd())
	cmd.AddCommand(newRestartCmd())
	cmd.AddCommand(newUpgradeCmd())

	// Inspection
	cmd.AddCommand(newListCmd())
	cmd.AddCommand(newStatusCmd())
	cmd.AddCommand(newLogsCmd())
	cmd.AddCommand(newEventsCmd())
	cmd.AddCommand(newConfigCmd())

	// Interaction
	cmd.AddCommand(newExecCmd())
	cmd.AddCommand(newPortForwardCmd())
	cmd.AddCommand(newOpenCmd())

	// Configuration
	cmd.AddCommand(newSkillsCmd())
	cmd.AddCommand(newEnvCmd())
	cmd.AddCommand(newEnableCmd())
	cmd.AddCommand(newDisableCmd())

	// Operations
	cmd.AddCommand(newBackupCmd())
	cmd.AddCommand(newRestoreCmd())
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
