package cmd

import (
	"context"
	"fmt"

	"github.com/openclaw-rocks/kubectl-openclaw/pkg/kube"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restart NAME",
		Short: "Restart an OpenClaw instance",
		Long: `Restart an OpenClawInstance by deleting its pods. The StatefulSet controller
will automatically recreate them, triggering a fresh start.`,
		Example: `  claw restart my-agent
  claw restart my-agent -n production`,
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

			// Verify instance exists
			_, err = clients.Dynamic.Resource(kube.OpenClawGVR).Namespace(ns).Get(
				context.TODO(), name, metav1.GetOptions{},
			)
			if err != nil {
				return fmt.Errorf("instance %q not found: %w", name, err)
			}

			pods, err := clients.Kube.CoreV1().Pods(ns).List(context.TODO(), metav1.ListOptions{
				LabelSelector: podLabelSelector(name),
			})
			if err != nil {
				return fmt.Errorf("failed to list pods: %w", err)
			}

			if len(pods.Items) == 0 {
				fmt.Printf("No pods found for instance %q — nothing to restart.\n", name)
				return nil
			}

			for _, pod := range pods.Items {
				err := clients.Kube.CoreV1().Pods(ns).Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "Warning: failed to delete pod %s: %v\n", pod.Name, err)
				} else {
					fmt.Printf("Deleted pod %s\n", pod.Name)
				}
			}

			fmt.Printf("\nInstance %q is restarting. Monitor with:\n", name)
			fmt.Printf("  kubectl openclaw status %s\n", name)
			return nil
		},
	}
}
