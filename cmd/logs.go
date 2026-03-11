package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/openclaw-rocks/kubectl-openclaw/pkg/kube"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newLogsCmd() *cobra.Command {
	var (
		follow     bool
		container  string
		tail       int64
		previous   bool
		timestamps bool
		since      string
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

  # Tailscale sidecar logs
  kubectl openclaw logs my-agent -c tailscale

  # Ollama sidecar logs
  kubectl openclaw logs my-agent -c ollama

  # Web Terminal sidecar logs
  kubectl openclaw logs my-agent -c ttyd

  # Last 100 lines
  kubectl openclaw logs my-agent --tail 100

  # Logs with timestamps
  kubectl openclaw logs my-agent --timestamps

  # Logs from the last hour
  kubectl openclaw logs my-agent --since 1h

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

			pods, err := clients.Kube.CoreV1().Pods(ns).List(context.TODO(), metav1.ListOptions{
				LabelSelector: podLabelSelector(name),
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
				Follow:     follow,
				Previous:   previous,
				Timestamps: timestamps,
			}
			if container != "" {
				opts.Container = container
			}
			if tail > 0 {
				opts.TailLines = &tail
			}
			if since != "" {
				duration, err := time.ParseDuration(since)
				if err != nil {
					return fmt.Errorf("invalid --since value %q: %w", since, err)
				}
				sinceSeconds := int64(duration.Seconds())
				opts.SinceSeconds = &sinceSeconds
			}

			req := clients.Kube.CoreV1().Pods(ns).GetLogs(pod.Name, opts)
			stream, err := req.Stream(context.TODO())
			if err != nil {
				return fmt.Errorf("failed to stream logs from pod %s: %w", pod.Name, err)
			}
			defer stream.Close()

			scanner := bufio.NewScanner(stream)
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
	cmd.Flags().BoolVar(&timestamps, "timestamps", false, "include timestamps in log output")
	cmd.Flags().StringVar(&since, "since", "", "show logs since duration (e.g. 1h, 30m, 2h30m)")

	return cmd
}
