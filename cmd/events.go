package cmd

import (
	"context"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/openclaw-rocks/kubectl-openclaw/pkg/kube"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newEventsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "events NAME",
		Short: "Show events for an OpenClaw instance",
		Long: `Display Kubernetes events related to an OpenClawInstance and its managed pods.
Useful for debugging provisioning failures, crash loops, and reconciliation issues.`,
		Example: `  # Show events for an instance
  kubectl openclaw events my-agent

  # Show events in a specific namespace
  kubectl openclaw events my-agent -n production`,
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

			events, err := clients.Kube.CoreV1().Events(ns).List(context.TODO(), metav1.ListOptions{
				FieldSelector: fmt.Sprintf("involvedObject.name=%s", name),
			})
			if err != nil {
				return fmt.Errorf("failed to list events: %w", err)
			}

			pods, _ := clients.Kube.CoreV1().Pods(ns).List(context.TODO(), metav1.ListOptions{
				LabelSelector: podLabelSelector(name),
			})
			if pods != nil {
				for _, pod := range pods.Items {
					podEvents, err := clients.Kube.CoreV1().Events(ns).List(context.TODO(), metav1.ListOptions{
						FieldSelector: fmt.Sprintf("involvedObject.name=%s", pod.Name),
					})
					if err == nil {
						events.Items = append(events.Items, podEvents.Items...)
					}
				}
			}

			// Get events for managed StatefulSet/Service
			obj, err := clients.Dynamic.Resource(kube.OpenClawGVR).Namespace(ns).Get(
				context.TODO(), name, metav1.GetOptions{},
			)
			if err == nil {
				status, _, _ := unstructuredNestedMap(obj.Object, "status")
				managed, ok, _ := unstructuredNestedMap(status, "managedResources")
				if ok {
					for _, key := range []string{"statefulSet", "deployment"} {
						resName := getNestedString(managed, key)
						if resName != "" {
							resEvents, err := clients.Kube.CoreV1().Events(ns).List(context.TODO(), metav1.ListOptions{
								FieldSelector: fmt.Sprintf("involvedObject.name=%s", resName),
							})
							if err == nil {
								events.Items = append(events.Items, resEvents.Items...)
							}
						}
					}
				}
			}

			if len(events.Items) == 0 {
				fmt.Printf("No events found for instance %q in namespace %q.\n", name, ns)
				return nil
			}

			// Deduplicate by UID
			seen := make(map[string]bool)
			var deduped []int
			for i, e := range events.Items {
				uid := string(e.UID)
				if !seen[uid] {
					seen[uid] = true
					deduped = append(deduped, i)
				}
			}

			sort.Slice(deduped, func(i, j int) bool {
				ei := events.Items[deduped[i]]
				ej := events.Items[deduped[j]]
				ti := ei.LastTimestamp.Time
				tj := ej.LastTimestamp.Time
				if ti.IsZero() {
					ti = ei.FirstTimestamp.Time
				}
				if tj.IsZero() {
					tj = ej.FirstTimestamp.Time
				}
				return ti.Before(tj)
			})

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "LAST SEEN\tTYPE\tREASON\tOBJECT\tMESSAGE")
			for _, idx := range deduped {
				e := events.Items[idx]
				age := formatAge(e.LastTimestamp.Time)
				if e.LastTimestamp.IsZero() {
					if !e.FirstTimestamp.IsZero() {
						age = formatAge(e.FirstTimestamp.Time)
					} else {
						age = "<unknown>"
					}
				}
				obj := fmt.Sprintf("%s/%s", e.InvolvedObject.Kind, e.InvolvedObject.Name)
				msg := e.Message
				if e.Count > 1 {
					msg = fmt.Sprintf("(x%d) %s", e.Count, msg)
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", age, e.Type, e.Reason, obj, msg)
			}
			return w.Flush()
		},
	}
}
