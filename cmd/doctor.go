package cmd

import (
	"context"
	"fmt"

	"github.com/openclaw-rocks/kubectl-openclaw/pkg/kube"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type checkResult struct {
	Name    string
	Passed  bool
	Message string
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor [NAME]",
		Short: "Run diagnostics on the OpenClaw setup",
		Long: `Run a series of diagnostic checks to verify that the OpenClaw operator
and instances are properly configured and healthy.

Without a NAME argument, checks the operator installation.
With a NAME argument, also checks the specific instance.`,
		Example: `  # Check operator health
  kubectl openclaw doctor

  # Check operator + specific instance
  kubectl openclaw doctor my-agent`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
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

			var results []checkResult

			// Cluster-level checks
			results = append(results, checkCRDInstalled(clients))
			results = append(results, checkOperatorRunning(clients))

			// Instance-level checks
			if len(args) > 0 {
				name := args[0]
				results = append(results, checkInstanceExists(clients, ns, name))
				results = append(results, checkInstancePhase(clients, ns, name))
				results = append(results, checkInstancePod(clients, ns, name))
				results = append(results, checkInstanceConditions(clients, ns, name)...)
			}

			// Print results
			passed := 0
			failed := 0
			for _, r := range results {
				if r.Passed {
					fmt.Printf("  [PASS]  %s\n", r.Name)
					passed++
				} else {
					fmt.Printf("  [FAIL]  %s\n", r.Name)
					failed++
				}
				if r.Message != "" {
					fmt.Printf("          %s\n", r.Message)
				}
			}

			fmt.Println()
			fmt.Printf("Results: %d passed, %d failed\n", passed, failed)

			if failed > 0 {
				return fmt.Errorf("%d check(s) failed", failed)
			}
			return nil
		},
	}
}

func checkCRDInstalled(clients *kube.Clients) checkResult {
	_, err := clients.Dynamic.Resource(kube.OpenClawGVR).List(
		context.TODO(), metav1.ListOptions{Limit: 1},
	)
	if err != nil {
		return checkResult{
			Name:    "OpenClawInstance CRD installed",
			Passed:  false,
			Message: fmt.Sprintf("CRD not found: %v. Install with: helm install openclaw-operator oci://ghcr.io/openclaw-rocks/charts/openclaw-operator", err),
		}
	}
	return checkResult{
		Name:   "OpenClawInstance CRD installed",
		Passed: true,
	}
}

func checkOperatorRunning(clients *kube.Clients) checkResult {
	// Check common operator namespaces
	operatorNamespaces := []string{
		"openclaw-operator-system",
		"openclaw-system",
	}

	for _, ns := range operatorNamespaces {
		pods, err := clients.Kube.CoreV1().Pods(ns).List(context.TODO(), metav1.ListOptions{
			LabelSelector: "control-plane=controller-manager",
		})
		if err != nil {
			continue
		}
		for _, pod := range pods.Items {
			if pod.Status.Phase == "Running" {
				return checkResult{
					Name:    "OpenClaw operator running",
					Passed:  true,
					Message: fmt.Sprintf("Found in %s/%s", ns, pod.Name),
				}
			}
		}
	}

	return checkResult{
		Name:    "OpenClaw operator running",
		Passed:  false,
		Message: "No running operator pod found in openclaw-operator-system or openclaw-system",
	}
}

func checkInstanceExists(clients *kube.Clients, ns, name string) checkResult {
	_, err := clients.Dynamic.Resource(kube.OpenClawGVR).Namespace(ns).Get(
		context.TODO(), name, metav1.GetOptions{},
	)
	if err != nil {
		return checkResult{
			Name:    fmt.Sprintf("Instance %q exists", name),
			Passed:  false,
			Message: err.Error(),
		}
	}
	return checkResult{
		Name:   fmt.Sprintf("Instance %q exists", name),
		Passed: true,
	}
}

func checkInstancePhase(clients *kube.Clients, ns, name string) checkResult {
	obj, err := clients.Dynamic.Resource(kube.OpenClawGVR).Namespace(ns).Get(
		context.TODO(), name, metav1.GetOptions{},
	)
	if err != nil {
		return checkResult{
			Name:    fmt.Sprintf("Instance %q phase is Running", name),
			Passed:  false,
			Message: err.Error(),
		}
	}

	phase, _, _ := unstructured.NestedString(obj.Object, "status", "phase")
	if phase == "Running" {
		return checkResult{
			Name:   fmt.Sprintf("Instance %q phase is Running", name),
			Passed: true,
		}
	}
	return checkResult{
		Name:    fmt.Sprintf("Instance %q phase is Running", name),
		Passed:  false,
		Message: fmt.Sprintf("Current phase: %s", phase),
	}
}

func checkInstancePod(clients *kube.Clients, ns, name string) checkResult {
	pods, err := clients.Kube.CoreV1().Pods(ns).List(context.TODO(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app.kubernetes.io/name=openclaw,app.kubernetes.io/instance=%s", name),
	})
	if err != nil {
		return checkResult{
			Name:    fmt.Sprintf("Pod for %q is healthy", name),
			Passed:  false,
			Message: err.Error(),
		}
	}
	if len(pods.Items) == 0 {
		return checkResult{
			Name:    fmt.Sprintf("Pod for %q is healthy", name),
			Passed:  false,
			Message: "No pods found. Check events: kubectl describe openclawinstance " + name,
		}
	}

	pod := pods.Items[0]
	if pod.Status.Phase != "Running" {
		return checkResult{
			Name:    fmt.Sprintf("Pod for %q is healthy", name),
			Passed:  false,
			Message: fmt.Sprintf("Pod %s is in phase %s", pod.Name, pod.Status.Phase),
		}
	}

	for _, cs := range pod.Status.ContainerStatuses {
		if !cs.Ready {
			return checkResult{
				Name:    fmt.Sprintf("Pod for %q is healthy", name),
				Passed:  false,
				Message: fmt.Sprintf("Container %s is not ready", cs.Name),
			}
		}
		if cs.RestartCount > 5 {
			return checkResult{
				Name:    fmt.Sprintf("Pod for %q is healthy", name),
				Passed:  false,
				Message: fmt.Sprintf("Container %s has %d restarts (possible crash loop)", cs.Name, cs.RestartCount),
			}
		}
	}

	return checkResult{
		Name:   fmt.Sprintf("Pod for %q is healthy", name),
		Passed: true,
	}
}

func checkInstanceConditions(clients *kube.Clients, ns, name string) []checkResult {
	obj, err := clients.Dynamic.Resource(kube.OpenClawGVR).Namespace(ns).Get(
		context.TODO(), name, metav1.GetOptions{},
	)
	if err != nil {
		return nil
	}

	status, _, _ := unstructuredNestedMap(obj.Object, "status")
	conditions, ok := status["conditions"]
	if !ok {
		return nil
	}
	condList, ok := conditions.([]interface{})
	if !ok {
		return nil
	}

	var results []checkResult
	for _, c := range condList {
		cond, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		condType := getNestedString(cond, "type")
		condStatus := getNestedString(cond, "status")
		message := getNestedString(cond, "message")

		passed := condStatus == "True"
		r := checkResult{
			Name:   fmt.Sprintf("Condition %s", condType),
			Passed: passed,
		}
		if !passed && message != "" {
			r.Message = message
		}
		results = append(results, r)
	}
	return results
}
