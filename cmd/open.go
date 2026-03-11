package cmd

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"

	"github.com/openclaw-rocks/kubectl-openclaw/pkg/kube"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newOpenCmd() *cobra.Command {
	var gateway bool

	cmd := &cobra.Command{
		Use:   "open NAME",
		Short: "Open an OpenClaw instance in the browser",
		Long: `Open the canvas UI or gateway endpoint for an OpenClawInstance in your default browser.
Detects the URL from ingress configuration, LoadBalancer, or suggests port-forward.`,
		Example: `  # Open the canvas UI
  claw open my-agent

  # Open the gateway endpoint instead
  claw open my-agent --gateway`,
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

			obj, err := clients.Dynamic.Resource(kube.OpenClawGVR).Namespace(ns).Get(
				context.TODO(), name, metav1.GetOptions{},
			)
			if err != nil {
				return fmt.Errorf("instance %q not found: %w", name, err)
			}

			spec, _, _ := unstructuredNestedMap(obj.Object, "spec")
			status, _, _ := unstructuredNestedMap(obj.Object, "status")

			// Try ingress hosts first
			ingressEnabled, iOk := getNestedBool(spec, "networking", "ingress", "enabled")
			if iOk && ingressEnabled {
				hosts, hOk := getNestedSlice(spec, "networking", "ingress", "hosts")
				if hOk && len(hosts) > 0 {
					if hm, ok := hosts[0].(map[string]interface{}); ok {
						host := getNestedString(hm, "host")
						if host != "" {
							url := "https://" + host
							fmt.Printf("Opening %s\n", url)
							return openBrowser(url)
						}
					}
				}
			}

			// Try LoadBalancer service
			managed, _, _ := unstructuredNestedMap(status, "managedResources")
			svcName := getNestedString(managed, "service")
			if svcName != "" {
				svc, err := clients.Kube.CoreV1().Services(ns).Get(context.TODO(), svcName, metav1.GetOptions{})
				if err == nil && svc.Spec.Type == "LoadBalancer" {
					for _, ingress := range svc.Status.LoadBalancer.Ingress {
						host := ingress.Hostname
						if host == "" {
							host = ingress.IP
						}
						if host != "" {
							port := "18793"
							if gateway {
								port = "18789"
							}
							url := fmt.Sprintf("http://%s:%s", host, port)
							fmt.Printf("Opening %s\n", url)
							return openBrowser(url)
						}
					}
				}
			}

			// Suggest port-forward
			port := "18793"
			label := "canvas"
			if gateway {
				port = "18789"
				label = "gateway"
			}
			fmt.Printf("No external endpoint found for %q.\n\n", name)
			fmt.Printf("Use port-forward to access the %s locally:\n", label)
			fmt.Printf("  kubectl openclaw port-forward %s\n", name)
			fmt.Printf("  open http://127.0.0.1:%s\n", port)
			return nil
		},
	}

	cmd.Flags().BoolVar(&gateway, "gateway", false, "open gateway endpoint instead of canvas")
	return cmd
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		return fmt.Errorf("unsupported platform %s — open %s manually", runtime.GOOS, url)
	}
	return cmd.Start()
}
