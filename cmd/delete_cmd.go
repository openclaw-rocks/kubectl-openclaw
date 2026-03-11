package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/openclaw-rocks/kubectl-openclaw/pkg/kube"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func newDeleteCmd() *cobra.Command {
	var (
		yes        bool
		skipBackup bool
	)

	cmd := &cobra.Command{
		Use:   "delete NAME",
		Short: "Delete an OpenClaw instance",
		Long: `Delete an OpenClawInstance custom resource. By default the operator will
create a backup before deleting the instance data.

The PVC is retained (orphaned) by default to prevent data loss.
Use --skip-backup to skip the pre-deletion backup.`,
		Example: `  # Delete with confirmation prompt and pre-deletion backup
  claw delete my-agent

  # Delete without confirmation
  claw delete my-agent --yes

  # Delete and skip the backup
  claw delete my-agent --skip-backup --yes`,
		Args: cobra.ExactArgs(1),
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

			// Verify instance exists and show info
			obj, err := clients.Dynamic.Resource(kube.OpenClawGVR).Namespace(ns).Get(
				context.TODO(), name, metav1.GetOptions{},
			)
			if err != nil {
				return fmt.Errorf("instance %q not found in namespace %q: %w", name, ns, err)
			}

			status, _, _ := unstructuredNestedMap(obj.Object, "status")
			phase := getNestedString(status, "phase")

			if !yes {
				fmt.Printf("Instance:  %s/%s\n", ns, name)
				fmt.Printf("Phase:     %s\n", phase)
				if skipBackup {
					fmt.Printf("Backup:    SKIPPED (--skip-backup)\n")
				} else {
					fmt.Printf("Backup:    will run before deletion\n")
				}
				fmt.Println()
				fmt.Printf("Delete this instance? [y/N]: ")
				reader := bufio.NewReader(os.Stdin)
				answer, _ := reader.ReadString('\n')
				answer = strings.TrimSpace(strings.ToLower(answer))
				if answer != "y" && answer != "yes" {
					fmt.Println("Cancelled.")
					return nil
				}
			}

			// Add skip-backup annotation if requested
			if skipBackup {
				patch := map[string]interface{}{
					"metadata": map[string]interface{}{
						"annotations": map[string]interface{}{
							"openclaw.rocks/skip-backup": "true",
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
					return fmt.Errorf("failed to set skip-backup annotation: %w", err)
				}
			}

			err = clients.Dynamic.Resource(kube.OpenClawGVR).Namespace(ns).Delete(
				context.TODO(), name, metav1.DeleteOptions{},
			)
			if err != nil {
				return fmt.Errorf("failed to delete instance: %w", err)
			}

			fmt.Printf("OpenClawInstance %s/%s deleted.\n", ns, name)
			return nil
		},
	}

	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation prompt")
	cmd.Flags().BoolVar(&skipBackup, "skip-backup", false, "skip pre-deletion backup")

	return cmd
}
