package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"

	"github.com/openclaw-rocks/kubectl-openclaw/pkg/kube"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newLogsCmd() *cobra.Command {
	var (
		follow    bool
		container string
		tail      int64
		previous  bool
	)

	cmd := &cobra.Command{
		Use:   "logs NAME",
		Short: "Tail logs from an OpenClaw instance",
		Long: `Stream logs from the pod belonging to an OpenClawInstance.
Automatically resolves the pod name from the instance name using label selectors.`,
		Example: `  # Tail logs
  kubectl openclaw logs my-agent

  # Follow logs
  kubectl openclaw logs my-agent -f

  # Chromium sidecar logs
  kubectl openclaw logs my-agent -c chromium

  # Last 100 lines
  kubectl openclaw logs my-agent --tail 100

  # Previous container logs (after crash)
  kubectl openclaw logs my-agent --previous`,
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

			// Find the pod for this instance
			pods, err := clients.Kube.CoreV1().Pods(ns).List(context.TODO(), metav1.ListOptions{
				LabelSelector: fmt.Sprintf("app.kubernetes.io/name=openclaw,app.kubernetes.io/instance=%s", name),
			})
			if err != nil {
				return fmt.Errorf("failed to list pods: %w", err)
			}
			if len(pods.Items) == 0 {
				return fmt.Errorf("no pods found for OpenClawInstance %q in namespace %q", name, ns)
			}

			pod := pods.Items[0]
			if len(pods.Items) > 1 {
				fmt.Fprintf(cmd.ErrOrStderr(), "Warning: multiple pods found, using %s\n", pod.Name)
			}

			opts := &corev1.PodLogOptions{
				Follow:   follow,
				Previous: previous,
			}
			if container != "" {
				opts.Container = container
			}
			if tail > 0 {
				opts.TailLines = &tail
			}

			req := clients.Kube.CoreV1().Pods(ns).GetLogs(pod.Name, opts)
			stream, err := req.Stream(context.TODO())
			if err != nil {
				return fmt.Errorf("failed to stream logs from pod %s: %w", pod.Name, err)
			}
			defer stream.Close()

			scanner := bufio.NewScanner(stream)
			// Increase buffer size for potentially long log lines
			scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
			for scanner.Scan() {
				fmt.Fprintln(cmd.OutOrStdout(), scanner.Text())
			}
			if err := scanner.Err(); err != nil && err != io.EOF {
				return fmt.Errorf("error reading logs: %w", err)
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "follow log output")
	cmd.Flags().StringVarP(&container, "container", "c", "", "container name (default: openclaw main container)")
	cmd.Flags().Int64Var(&tail, "tail", 0, "number of lines from the end of the logs to show")
	cmd.Flags().BoolVar(&previous, "previous", false, "show logs from previous terminated container")

	return cmd
}
