package cmd

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/openclaw-rocks/kubectl-openclaw/pkg/kube"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func newDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disable NAME SIDECAR",
		Short: "Disable a sidecar on an OpenClaw instance",
		Long: `Disable a sidecar container on an OpenClawInstance.

Available sidecars: chromium, tailscale, ollama, web-terminal`,
		Example: `  claw disable my-agent chromium
  claw disable my-agent ollama
  claw disable my-agent tailscale
  claw disable my-agent web-terminal`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			sidecar := args[1]

			clients, err := kube.NewClients(kubeconfig)
			if err != nil {
				return err
			}

			ns := namespace
			if ns == "" {
				ns, err = resolveNamespace()
				if err != nil {
					return err
				}
			}

			var specKey string
			switch sidecar {
			case "chromium", "browser":
				specKey = "chromium"
			case "tailscale":
				specKey = "tailscale"
			case "ollama":
				specKey = "ollama"
			case "web-terminal", "terminal", "ttyd":
				specKey = "webTerminal"
			default:
				return fmt.Errorf("unknown sidecar %q — available: chromium, tailscale, ollama, web-terminal", sidecar)
			}

			patch := map[string]interface{}{
				"spec": map[string]interface{}{
					specKey: map[string]interface{}{
						"enabled": false,
					},
				},
			}

			patchBytes, err := json.Marshal(patch)
			if err != nil {
				return fmt.Errorf("failed to create patch: %w", err)
			}

			_, err = clients.Dynamic.Resource(kube.OpenClawGVR).Namespace(ns).Patch(
				context.TODO(), name, types.MergePatchType, patchBytes, metav1.PatchOptions{},
			)
			if err != nil {
				return fmt.Errorf("failed to disable %s: %w", sidecar, err)
			}

			fmt.Printf("Disabled %s on %s/%s.\n", sidecar, ns, name)
			return nil
		},
	}
}
