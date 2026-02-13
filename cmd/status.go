package cmd

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/openclaw-rocks/kubectl-openclaw/pkg/kube"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status NAME",
		Short: "Show detailed status of an OpenClaw instance",
		Long: `Display a rich status view of an OpenClawInstance including its phase,
conditions, endpoints, managed resources, image, and pod health.`,
		Example: `  kubectl openclaw status my-agent
  kubectl openclaw status my-agent -n production`,
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
				return fmt.Errorf("failed to get OpenClawInstance %q: %w", name, err)
			}

			spec, _, _ := unstructuredNestedMap(obj.Object, "spec")
			status, _, _ := unstructuredNestedMap(obj.Object, "status")

			phase := getNestedString(status, "phase")
			if phase == "" {
				phase = "Pending"
			}

			// Header
			fmt.Printf("OpenClawInstance: %s/%s\n", ns, name)
			fmt.Printf("Phase:           %s\n", phaseWithIndicator(phase))
			fmt.Printf("Age:             %s\n", formatAge(obj.GetCreationTimestamp().Time))
			fmt.Println()

			// Image
			image := getNestedString(spec, "image", "repository")
			tag := getNestedString(spec, "image", "tag")
			digest := getNestedString(spec, "image", "digest")
			if image == "" {
				image = "ghcr.io/openclaw/openclaw"
			}
			if digest != "" {
				fmt.Printf("Image:    %s@%s\n", image, digest)
			} else {
				if tag == "" {
					tag = "latest"
				}
				fmt.Printf("Image:    %s:%s\n", image, tag)
			}
			fmt.Println()

			// Endpoints
			gateway := getNestedString(status, "gatewayEndpoint")
			canvas := getNestedString(status, "canvasEndpoint")
			if gateway != "" || canvas != "" {
				fmt.Println("Endpoints:")
				if gateway != "" {
					fmt.Printf("  Gateway (WebSocket): %s\n", gateway)
				}
				if canvas != "" {
					fmt.Printf("  Canvas (HTTP):       %s\n", canvas)
				}
				fmt.Println()
			}

			// Conditions
			printConditions(status)

			// Managed Resources
			printManagedResources(status)

			// Pod status
			printPodStatus(clients, ns, name)

			return nil
		},
	}
}

func phaseWithIndicator(phase string) string {
	switch phase {
	case "Running":
		return phase + "  [ok]"
	case "Degraded":
		return phase + "  [warning]"
	case "Failed":
		return phase + "  [error]"
	case "Provisioning":
		return phase + "  [provisioning]"
	default:
		return phase
	}
}

func printConditions(status map[string]interface{}) {
	conditions, ok := status["conditions"]
	if !ok {
		return
	}
	condList, ok := conditions.([]interface{})
	if !ok || len(condList) == 0 {
		return
	}

	fmt.Println("Conditions:")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "  TYPE\tSTATUS\tREASON\tMESSAGE")
	for _, c := range condList {
		cond, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		condType := getNestedString(cond, "type")
		condStatus := getNestedString(cond, "status")
		reason := getNestedString(cond, "reason")
		message := getNestedString(cond, "message")

		indicator := " "
		if condStatus == "True" {
			indicator = "+"
		} else if condStatus == "False" {
			indicator = "-"
		}

		fmt.Fprintf(w, "  %s %s\t%s\t%s\t%s\n", indicator, condType, condStatus, reason, message)
	}
	w.Flush()
	fmt.Println()
}

func printManagedResources(status map[string]interface{}) {
	managed, ok, _ := unstructuredNestedMap(status, "managedResources")
	if !ok {
		return
	}

	resources := []struct {
		label string
		key   string
	}{
		{"Deployment", "deployment"},
		{"Service", "service"},
		{"ConfigMap", "configMap"},
		{"PVC", "pvc"},
		{"NetworkPolicy", "networkPolicy"},
		{"PDB", "podDisruptionBudget"},
		{"ServiceAccount", "serviceAccount"},
		{"Role", "role"},
		{"RoleBinding", "roleBinding"},
	}

	fmt.Println("Managed Resources:")
	for _, r := range resources {
		val := getNestedString(managed, r.key)
		if val != "" {
			fmt.Printf("  %-16s %s\n", r.label+":", val)
		}
	}
	fmt.Println()
}

func printPodStatus(clients *kube.Clients, ns, instanceName string) {
	pods, err := clients.Kube.CoreV1().Pods(ns).List(context.TODO(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app.kubernetes.io/name=openclaw,app.kubernetes.io/instance=%s", instanceName),
	})
	if err != nil {
		fmt.Printf("Pod Status: failed to list pods: %v\n", err)
		return
	}

	if len(pods.Items) == 0 {
		fmt.Println("Pods: none found")
		return
	}

	fmt.Println("Pods:")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "  NAME\tSTATUS\tRESTARTS\tAGE")
	for _, pod := range pods.Items {
		restarts := int32(0)
		for _, cs := range pod.Status.ContainerStatuses {
			restarts += cs.RestartCount
		}
		fmt.Fprintf(w, "  %s\t%s\t%d\t%s\n",
			pod.Name, string(pod.Status.Phase), restarts, formatAge(pod.CreationTimestamp.Time))
	}
	w.Flush()
	fmt.Println()

	// Show container details for the first pod
	pod := pods.Items[0]
	if len(pod.Status.ContainerStatuses) > 0 {
		fmt.Println("Containers:")
		w = tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "  NAME\tREADY\tSTATE\tRESTARTS")
		for _, cs := range pod.Status.ContainerStatuses {
			state := "Unknown"
			if cs.State.Running != nil {
				state = "Running"
			} else if cs.State.Waiting != nil {
				state = "Waiting: " + cs.State.Waiting.Reason
			} else if cs.State.Terminated != nil {
				state = "Terminated: " + cs.State.Terminated.Reason
			}
			ready := "false"
			if cs.Ready {
				ready = "true"
			}
			fmt.Fprintf(w, "  %s\t%s\t%s\t%d\n", cs.Name, ready, state, cs.RestartCount)
		}
		w.Flush()
	}
}
