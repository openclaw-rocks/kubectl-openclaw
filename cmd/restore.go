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

func newRestoreCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restore NAME PATH",
		Short: "Restore an OpenClaw instance from a backup",
		Long: `Trigger a restore of an OpenClawInstance by setting the restoreFrom field
to the specified S3 backup path. The operator will handle the restore process.

The instance will enter the "Restoring" phase while the restore is in progress.
The restoreFrom field is automatically cleared after a successful restore.`,
		Example: `  # Restore from a specific backup path
  kubectl openclaw restore my-agent s3://my-bucket/backups/my-agent/2024-01-15T020000Z

  # Check restore progress
  kubectl openclaw status my-agent

  # View the last backup path (to know what to restore from)
  kubectl openclaw backup my-agent`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			path := args[1]

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

			// Verify instance exists
			_, err = clients.Dynamic.Resource(kube.OpenClawGVR).Namespace(ns).Get(
				context.TODO(), name, metav1.GetOptions{},
			)
			if err != nil {
				return fmt.Errorf("instance %q not found in namespace %q: %w", name, ns, err)
			}

			patch := map[string]interface{}{
				"spec": map[string]interface{}{
					"restoreFrom": path,
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
				return fmt.Errorf("failed to trigger restore: %w", err)
			}

			fmt.Printf("Restore triggered for %s/%s from:\n  %s\n", ns, name, path)
			fmt.Printf("\nMonitor progress with:\n  kubectl openclaw status %s\n", name)
			return nil
		},
	}
}
