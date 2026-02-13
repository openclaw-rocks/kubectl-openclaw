package cmd

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/openclaw-rocks/kubectl-openclaw/pkg/kube"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
)

func newListCmd() *cobra.Command {
	var allNamespaces bool

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List OpenClaw instances",
		Long:    "List all OpenClawInstance resources with their phase, readiness, endpoints, and age.",
		Example: `  # List instances in current namespace
  kubectl openclaw list

  # List instances across all namespaces
  kubectl openclaw list -A`,
		RunE: func(cmd *cobra.Command, args []string) error {
			clients, err := kube.NewClients(kubeconfig)
			if err != nil {
				return err
			}

			ns := namespace
			if allNamespaces {
				ns = ""
			} else if ns == "" {
				ns, err = resolveNamespace()
				if err != nil {
					return err
				}
			}

			list, err := clients.Dynamic.Resource(kube.OpenClawGVR).Namespace(ns).List(
				context.TODO(), metav1.ListOptions{},
			)
			if err != nil {
				return fmt.Errorf("failed to list OpenClawInstances: %w", err)
			}

			if len(list.Items) == 0 {
				if allNamespaces {
					fmt.Println("No OpenClaw instances found in any namespace.")
				} else {
					fmt.Printf("No OpenClaw instances found in namespace %q.\n", ns)
				}
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			if allNamespaces {
				fmt.Fprintln(w, "NAMESPACE\tNAME\tPHASE\tREADY\tGATEWAY\tAGE")
			} else {
				fmt.Fprintln(w, "NAME\tPHASE\tREADY\tGATEWAY\tAGE")
			}

			for _, item := range list.Items {
				status, _, _ := unstructuredNestedMap(item.Object, "status")
				phase := getNestedString(status, "phase")
				if phase == "" {
					phase = "Pending"
				}
				gateway := getNestedString(status, "gatewayEndpoint")
				ready := getConditionStatus(status, "Ready")
				age := formatAge(item.GetCreationTimestamp().Time)

				if allNamespaces {
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
						item.GetNamespace(), item.GetName(), phase, ready, gateway, age)
				} else {
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
						item.GetName(), phase, ready, gateway, age)
				}
			}

			return w.Flush()
		},
	}

	cmd.Flags().BoolVarP(&allNamespaces, "all-namespaces", "A", false, "list instances across all namespaces")
	return cmd
}

func getConditionStatus(status map[string]interface{}, condType string) string {
	conditions, ok := status["conditions"]
	if !ok {
		return "Unknown"
	}
	condList, ok := conditions.([]interface{})
	if !ok {
		return "Unknown"
	}
	for _, c := range condList {
		cond, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		if getNestedString(cond, "type") == condType {
			return getNestedString(cond, "status")
		}
	}
	return "Unknown"
}

func formatAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func getNestedString(obj map[string]interface{}, keys ...string) string {
	current := obj
	for i, key := range keys {
		if i == len(keys)-1 {
			val, _ := current[key].(string)
			return val
		}
		next, ok := current[key].(map[string]interface{})
		if !ok {
			return ""
		}
		current = next
	}
	return ""
}

func unstructuredNestedMap(obj map[string]interface{}, fields ...string) (map[string]interface{}, bool, error) {
	current := obj
	for _, field := range fields {
		next, ok := current[field].(map[string]interface{})
		if !ok {
			return nil, false, nil
		}
		current = next
	}
	return current, true, nil
}

func resolveNamespace() (string, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		rules.ExplicitPath = kubeconfig
	}
	ns, _, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		rules,
		&clientcmd.ConfigOverrides{},
	).Namespace()
	if err != nil {
		return "", fmt.Errorf("failed to resolve namespace: %w", err)
	}
	return ns, nil
}
