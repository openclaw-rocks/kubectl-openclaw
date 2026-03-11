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

func newUpgradeCmd() *cobra.Command {
	var (
		digest string
		image  string
	)

	cmd := &cobra.Command{
		Use:   "upgrade NAME [TAG]",
		Short: "Upgrade an OpenClaw instance to a new version",
		Long: `Update the container image tag or digest for an OpenClawInstance.
The operator will perform a rolling update of the StatefulSet.

If auto-update is enabled, upgrades are handled automatically.`,
		Example: `  # Upgrade to a specific tag
  claw upgrade my-agent v1.2.3

  # Upgrade to latest
  claw upgrade my-agent latest

  # Pin to a specific digest
  claw upgrade my-agent --digest sha256:abc123...

  # Change the image repository
  claw upgrade my-agent v1.2.3 --image ghcr.io/custom/openclaw`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			tag := ""
			if len(args) > 1 {
				tag = args[1]
			}

			if tag == "" && digest == "" {
				return fmt.Errorf("provide a TAG argument or --digest flag")
			}

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

			// Get current instance to show what's changing
			obj, err := clients.Dynamic.Resource(kube.OpenClawGVR).Namespace(ns).Get(
				context.TODO(), name, metav1.GetOptions{},
			)
			if err != nil {
				return fmt.Errorf("instance %q not found: %w", name, err)
			}

			spec, _, _ := unstructuredNestedMap(obj.Object, "spec")
			currentTag := getNestedString(spec, "image", "tag")
			currentDigest := getNestedString(spec, "image", "digest")

			imageSpec := map[string]interface{}{}

			if image != "" {
				imageSpec["repository"] = image
			}

			if digest != "" {
				imageSpec["digest"] = digest
				// Clear tag when setting digest
				imageSpec["tag"] = ""
				fmt.Printf("Upgrading %s/%s:\n", ns, name)
				if currentDigest != "" {
					fmt.Printf("  digest: %s -> %s\n", truncateDigest(currentDigest), truncateDigest(digest))
				} else {
					fmt.Printf("  %s -> %s\n", currentTag, truncateDigest(digest))
				}
			} else {
				imageSpec["tag"] = tag
				// Clear digest when setting tag
				imageSpec["digest"] = ""
				fmt.Printf("Upgrading %s/%s:\n", ns, name)
				if currentTag != "" {
					fmt.Printf("  %s -> %s\n", currentTag, tag)
				} else if currentDigest != "" {
					fmt.Printf("  %s -> %s\n", truncateDigest(currentDigest), tag)
				} else {
					fmt.Printf("  (default) -> %s\n", tag)
				}
			}

			patch := map[string]interface{}{
				"spec": map[string]interface{}{
					"image": imageSpec,
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
				return fmt.Errorf("failed to upgrade: %w", err)
			}

			fmt.Printf("\nUpgrade initiated. Monitor with:\n")
			fmt.Printf("  kubectl openclaw status %s\n", name)
			return nil
		},
	}

	cmd.Flags().StringVar(&digest, "digest", "", "pin to a specific image digest")
	cmd.Flags().StringVar(&image, "image", "", "change the image repository")

	return cmd
}

func truncateDigest(digest string) string {
	if len(digest) > 19 {
		return digest[:19] + "..."
	}
	return digest
}
