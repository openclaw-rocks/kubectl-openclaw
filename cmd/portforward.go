package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"

	"github.com/openclaw-rocks/kubectl-openclaw/pkg/kube"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

func newPortForwardCmd() *cobra.Command {
	var (
		localGateway int
		localCanvas  int
		address      string
	)

	cmd := &cobra.Command{
		Use:     "port-forward NAME",
		Aliases: []string{"pf"},
		Short:   "Forward local ports to an OpenClaw instance",
		Long: `Forward local ports to the gateway and canvas endpoints of an OpenClawInstance pod.
By default forwards gateway (18789) and canvas (18793) to the same local ports.`,
		Example: `  # Forward default ports (gateway: 18789, canvas: 18793)
  kubectl openclaw port-forward my-agent

  # Custom local ports
  kubectl openclaw port-forward my-agent --gateway-port 8080 --canvas-port 8081

  # Listen on all interfaces
  kubectl openclaw port-forward my-agent --address 0.0.0.0`,
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
				return fmt.Errorf("no pods found for instance %q in namespace %q", name, ns)
			}

			pod := pods.Items[0]
			if len(pods.Items) > 1 {
				fmt.Fprintf(cmd.ErrOrStderr(), "Warning: multiple pods found, using %s\n", pod.Name)
			}

			transport, upgrader, err := spdy.RoundTripperFor(clients.Config)
			if err != nil {
				return fmt.Errorf("failed to create transport: %w", err)
			}

			url := clients.Kube.CoreV1().RESTClient().Post().
				Resource("pods").
				Namespace(ns).
				Name(pod.Name).
				SubResource("portforward").
				URL()

			dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", url)

			ports := []string{
				fmt.Sprintf("%d:18789", localGateway),
				fmt.Sprintf("%d:18793", localCanvas),
			}

			stopCh := make(chan struct{}, 1)
			readyCh := make(chan struct{})

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, os.Interrupt)
			go func() {
				<-sigCh
				close(stopCh)
			}()

			fw, err := portforward.NewOnAddresses(dialer, []string{address}, ports, stopCh, readyCh, os.Stdout, os.Stderr)
			if err != nil {
				return fmt.Errorf("failed to create port forwarder: %w", err)
			}

			fmt.Fprintf(cmd.ErrOrStderr(), "Forwarding from %s:%d -> 18789 (gateway)\n", address, localGateway)
			fmt.Fprintf(cmd.ErrOrStderr(), "Forwarding from %s:%d -> 18793 (canvas)\n", address, localCanvas)
			fmt.Fprintf(cmd.ErrOrStderr(), "Press Ctrl+C to stop\n")

			return fw.ForwardPorts()
		},
	}

	cmd.Flags().IntVar(&localGateway, "gateway-port", 18789, "local port for gateway WebSocket")
	cmd.Flags().IntVar(&localCanvas, "canvas-port", 18793, "local port for canvas HTTP")
	cmd.Flags().StringVar(&address, "address", "127.0.0.1", "address to listen on")

	return cmd
}
