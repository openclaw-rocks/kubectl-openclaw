package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/openclaw-rocks/kubectl-openclaw/pkg/kube"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	"golang.org/x/term"
)

type sizeQueue struct {
	ch chan remotecommand.TerminalSize
}

func (sq *sizeQueue) Next() *remotecommand.TerminalSize {
	size, ok := <-sq.ch
	if !ok {
		return nil
	}
	return &size
}

func newExecCmd() *cobra.Command {
	var (
		container string
		useTTY    bool
	)

	cmd := &cobra.Command{
		Use:   "exec NAME [-- COMMAND [ARGS...]]",
		Short: "Execute a command in an OpenClaw instance pod",
		Long: `Execute a command in the pod belonging to an OpenClawInstance.
By default opens an interactive shell (/bin/sh). Use -- to pass a custom command.`,
		Example: `  # Shell into an instance
  kubectl openclaw exec my-agent

  # Run a specific command
  kubectl openclaw exec my-agent -- ls -la /workspace

  # Exec into the chromium sidecar
  kubectl openclaw exec my-agent -c chromium -- /bin/sh

  # Non-interactive command
  kubectl openclaw exec my-agent -t=false -- cat /etc/openclaw/openclaw.json`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			execCmd := []string{"/bin/sh"}
			if dash := cmd.ArgsLenAtDash(); dash != -1 {
				if dashArgs := args[dash:]; len(dashArgs) > 0 {
					execCmd = dashArgs
				}
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

			pods, err := clients.Kube.CoreV1().Pods(ns).List(context.TODO(), metav1.ListOptions{
				LabelSelector: podLabelSelector(name),
			})
			if err != nil {
				return fmt.Errorf("failed to list pods: %w", err)
			}
			if len(pods.Items) == 0 {
				return fmt.Errorf("no pods found for instance %q in namespace %q", name, ns)
			}

			pod := pods.Items[0]
			if len(pods.Items) > 1 {
				fmt.Fprintf(cmd.ErrOrStderr(), "Warning: multiple pods found, using %s\n", pod.Name)
			}

			execOpts := &corev1.PodExecOptions{
				Command: execCmd,
				Stdin:   true,
				Stdout:  true,
				Stderr:  !useTTY,
				TTY:     useTTY,
			}
			if container != "" {
				execOpts.Container = container
			}

			req := clients.Kube.CoreV1().RESTClient().Post().
				Resource("pods").
				Name(pod.Name).
				Namespace(ns).
				SubResource("exec").
				VersionedParams(execOpts, scheme.ParameterCodec)

			exec, err := remotecommand.NewSPDYExecutor(clients.Config, "POST", req.URL())
			if err != nil {
				return fmt.Errorf("failed to create executor: %w", err)
			}

			if useTTY {
				fd := int(os.Stdin.Fd())
				if term.IsTerminal(fd) {
					oldState, err := term.MakeRaw(fd)
					if err != nil {
						return fmt.Errorf("failed to set terminal raw mode: %w", err)
					}
					defer term.Restore(fd, oldState)
				}

				sq := &sizeQueue{ch: make(chan remotecommand.TerminalSize, 1)}

				width, height, err := term.GetSize(fd)
				if err == nil {
					sq.ch <- remotecommand.TerminalSize{Width: uint16(width), Height: uint16(height)}
				}

				sigCh := make(chan os.Signal, 1)
				signal.Notify(sigCh, syscall.SIGWINCH)
				go func() {
					for range sigCh {
						w, h, err := term.GetSize(fd)
						if err == nil {
							sq.ch <- remotecommand.TerminalSize{Width: uint16(w), Height: uint16(h)}
						}
					}
				}()
				defer signal.Stop(sigCh)

				return exec.StreamWithContext(context.TODO(), remotecommand.StreamOptions{
					Stdin:             os.Stdin,
					Stdout:            os.Stdout,
					Tty:               true,
					TerminalSizeQueue: sq,
				})
			}

			return exec.StreamWithContext(context.TODO(), remotecommand.StreamOptions{
				Stdin:  os.Stdin,
				Stdout: os.Stdout,
				Stderr: os.Stderr,
			})
		},
	}

	cmd.Flags().StringVarP(&container, "container", "c", "", "container name (default: main openclaw container)")
	cmd.Flags().BoolVarP(&useTTY, "tty", "t", true, "allocate a pseudo-TTY")

	return cmd
}
