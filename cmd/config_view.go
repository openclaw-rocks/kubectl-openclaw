package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"github.com/openclaw-rocks/kubectl-openclaw/pkg/kube"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config [NAME]",
		Short: "View or edit the configuration of an OpenClaw instance",
		Long: `View the effective openclaw.json configuration from the managed ConfigMap.

Use "config edit" to interactively edit the instance's inline configuration.`,
		Example: `  # View the effective config
  claw config my-agent

  # Edit the config in your editor
  claw config edit my-agent`,
		Args: cobra.ExactArgs(1),
		RunE: configViewRunE,
	}

	cmd.AddCommand(newConfigEditCmd())

	return cmd
}

func configViewRunE(cmd *cobra.Command, args []string) error {
	name := args[0]

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

	obj, err := clients.Dynamic.Resource(kube.OpenClawGVR).Namespace(ns).Get(
		context.TODO(), name, metav1.GetOptions{},
	)
	if err != nil {
		return fmt.Errorf("failed to get instance %q: %w", name, err)
	}

	status, _, _ := unstructuredNestedMap(obj.Object, "status")
	managed, _, _ := unstructuredNestedMap(status, "managedResources")
	cmName := getNestedString(managed, "configMap")
	if cmName == "" {
		cmName = name
	}

	cm, err := clients.Kube.CoreV1().ConfigMaps(ns).Get(context.TODO(), cmName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get ConfigMap %q: %w", cmName, err)
	}

	if config, ok := cm.Data["openclaw.json"]; ok {
		var parsed interface{}
		if err := json.Unmarshal([]byte(config), &parsed); err == nil {
			pretty, _ := json.MarshalIndent(parsed, "", "  ")
			fmt.Println(string(pretty))
		} else {
			fmt.Println(config)
		}
		return nil
	}

	if len(cm.Data) == 0 {
		fmt.Printf("ConfigMap %q has no data entries.\n", cmName)
		return nil
	}

	for key, value := range cm.Data {
		fmt.Printf("--- %s ---\n%s\n", key, value)
	}
	return nil
}

func newConfigEditCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "edit NAME",
		Short: "Edit instance configuration in your editor",
		Long: `Open the instance's inline configuration (spec.config.raw) in your editor.
Changes are applied to the OpenClawInstance CR and the operator will reconcile.

Uses $EDITOR, $VISUAL, or falls back to vi.`,
		Example: `  claw config edit my-agent`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

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

			obj, err := clients.Dynamic.Resource(kube.OpenClawGVR).Namespace(ns).Get(
				context.TODO(), name, metav1.GetOptions{},
			)
			if err != nil {
				return fmt.Errorf("instance %q not found: %w", name, err)
			}

			// Read current spec.config.raw
			spec, _, _ := unstructuredNestedMap(obj.Object, "spec")
			configSpec, _, _ := unstructuredNestedMap(spec, "config")

			var configData []byte
			if rawConfig, ok := configSpec["raw"]; ok {
				configData, err = json.MarshalIndent(rawConfig, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal config: %w", err)
				}
			} else {
				// Try to read from managed ConfigMap as starting point
				status, _, _ := unstructuredNestedMap(obj.Object, "status")
				managed, _, _ := unstructuredNestedMap(status, "managedResources")
				cmName := getNestedString(managed, "configMap")
				if cmName != "" {
					cm, err := clients.Kube.CoreV1().ConfigMaps(ns).Get(context.TODO(), cmName, metav1.GetOptions{})
					if err == nil {
						if data, ok := cm.Data["openclaw.json"]; ok {
							var parsed interface{}
							if json.Unmarshal([]byte(data), &parsed) == nil {
								configData, _ = json.MarshalIndent(parsed, "", "  ")
							}
						}
					}
				}
				if configData == nil {
					configData = []byte("{}\n")
				}
			}

			// Write to temp file
			tmpFile, err := os.CreateTemp("", fmt.Sprintf("openclaw-%s-*.json", name))
			if err != nil {
				return fmt.Errorf("failed to create temp file: %w", err)
			}
			tmpPath := tmpFile.Name()
			defer os.Remove(tmpPath)

			if _, err := tmpFile.Write(configData); err != nil {
				tmpFile.Close()
				return fmt.Errorf("failed to write temp file: %w", err)
			}
			tmpFile.Close()

			// Open editor
			editor := os.Getenv("EDITOR")
			if editor == "" {
				editor = os.Getenv("VISUAL")
			}
			if editor == "" {
				editor = "vi"
			}

			editorCmd := exec.Command(editor, tmpPath)
			editorCmd.Stdin = os.Stdin
			editorCmd.Stdout = os.Stdout
			editorCmd.Stderr = os.Stderr
			if err := editorCmd.Run(); err != nil {
				return fmt.Errorf("editor exited with error: %w", err)
			}

			// Read back
			newData, err := os.ReadFile(tmpPath)
			if err != nil {
				return fmt.Errorf("failed to read edited file: %w", err)
			}

			// Check if anything changed
			if string(newData) == string(configData) {
				fmt.Println("No changes made.")
				return nil
			}

			// Validate JSON
			var parsed interface{}
			if err := json.Unmarshal(newData, &parsed); err != nil {
				return fmt.Errorf("invalid JSON: %w", err)
			}

			// Patch the CR
			patch := map[string]interface{}{
				"spec": map[string]interface{}{
					"config": map[string]interface{}{
						"raw": parsed,
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
				return fmt.Errorf("failed to apply config: %w", err)
			}

			fmt.Printf("Configuration updated for %s/%s.\n", ns, name)
			return nil
		},
	}
}
