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

func newEnableCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "enable NAME SIDECAR",
		Short: "Enable a sidecar on an OpenClaw instance",
		Long: `Enable a sidecar container on an OpenClawInstance. Available sidecars:

  chromium       Headless Chromium browser for web automation
  tailscale      Tailscale mesh networking (requires --auth-secret)
  ollama         Local LLM inference with Ollama
  web-terminal   Web-based terminal (ttyd) for browser access`,
		Example: `  # Enable Chromium browser
  claw enable my-agent chromium

  # Enable Ollama with specific models
  claw enable my-agent ollama --models llama3,codellama

  # Enable Ollama with GPU
  claw enable my-agent ollama --models llama3 --gpu 1

  # Enable Tailscale
  claw enable my-agent tailscale --auth-secret ts-authkey

  # Enable Tailscale with Funnel (public access)
  claw enable my-agent tailscale --auth-secret ts-authkey --mode funnel

  # Enable web terminal
  claw enable my-agent web-terminal

  # Enable read-only web terminal
  claw enable my-agent web-terminal --read-only`,
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

			var patch map[string]interface{}

			switch sidecar {
			case "chromium", "browser":
				sidecarSpec := map[string]interface{}{"enabled": true}
				persistence, _ := cmd.Flags().GetBool("persistence")
				if persistence {
					sidecarSpec["persistence"] = map[string]interface{}{"enabled": true}
				}
				patch = map[string]interface{}{
					"spec": map[string]interface{}{"chromium": sidecarSpec},
				}

			case "tailscale":
				authSecret, _ := cmd.Flags().GetString("auth-secret")
				if authSecret == "" {
					return fmt.Errorf("--auth-secret is required for tailscale (Secret containing the auth key)")
				}
				sidecarSpec := map[string]interface{}{
					"enabled":          true,
					"authKeySecretRef": map[string]interface{}{"name": authSecret},
				}
				mode, _ := cmd.Flags().GetString("mode")
				if mode != "" {
					sidecarSpec["mode"] = mode
				}
				hostname, _ := cmd.Flags().GetString("hostname")
				if hostname != "" {
					sidecarSpec["hostname"] = hostname
				}
				patch = map[string]interface{}{
					"spec": map[string]interface{}{"tailscale": sidecarSpec},
				}

			case "ollama":
				sidecarSpec := map[string]interface{}{"enabled": true}
				models, _ := cmd.Flags().GetStringSlice("models")
				if len(models) > 0 {
					m := make([]interface{}, len(models))
					for i, model := range models {
						m[i] = model
					}
					sidecarSpec["models"] = m
				}
				gpu, _ := cmd.Flags().GetInt("gpu")
				if gpu > 0 {
					sidecarSpec["gpu"] = gpu
				}
				patch = map[string]interface{}{
					"spec": map[string]interface{}{"ollama": sidecarSpec},
				}

			case "web-terminal", "terminal", "ttyd":
				sidecarSpec := map[string]interface{}{"enabled": true}
				readOnly, _ := cmd.Flags().GetBool("read-only")
				if readOnly {
					sidecarSpec["readOnly"] = true
				}
				patch = map[string]interface{}{
					"spec": map[string]interface{}{"webTerminal": sidecarSpec},
				}

			default:
				return fmt.Errorf("unknown sidecar %q — available: chromium, tailscale, ollama, web-terminal", sidecar)
			}

			patchBytes, err := json.Marshal(patch)
			if err != nil {
				return fmt.Errorf("failed to create patch: %w", err)
			}

			_, err = clients.Dynamic.Resource(kube.OpenClawGVR).Namespace(ns).Patch(
				context.TODO(), name, types.MergePatchType, patchBytes, metav1.PatchOptions{},
			)
			if err != nil {
				return fmt.Errorf("failed to enable %s: %w", sidecar, err)
			}

			fmt.Printf("Enabled %s on %s/%s.\n", sidecar, ns, name)
			return nil
		},
	}

	// Chromium flags
	cmd.Flags().Bool("persistence", false, "enable persistent browser profile (chromium)")

	// Tailscale flags
	cmd.Flags().String("auth-secret", "", "Secret name containing tailscale auth key (required for tailscale)")
	cmd.Flags().String("mode", "", "tailscale mode: serve (tailnet only) or funnel (public)")
	cmd.Flags().String("hostname", "", "tailscale device hostname")

	// Ollama flags
	cmd.Flags().StringSlice("models", nil, "ollama models to pre-pull (comma-separated)")
	cmd.Flags().Int("gpu", 0, "number of NVIDIA GPUs for ollama")

	// Web terminal flags
	cmd.Flags().Bool("read-only", false, "make web terminal read-only")

	return cmd
}
