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

			fmt.Println("=== Cluster Checks ===")
			results = append(results, checkCRDInstalled(clients))
			results = append(results, checkOperatorRunning(clients))
			results = append(results, checkWebhooks(clients))

			if len(args) > 0 {
				name := args[0]
				fmt.Printf("\n=== Instance Checks: %s ===\n", name)
				results = append(results, checkInstanceExists(clients, ns, name))
				results = append(results, checkInstancePhase(clients, ns, name))
				results = append(results, checkInstancePod(clients, ns, name))
				results = append(results, checkInstanceStorage(clients, ns, name))
				results = append(results, checkInstanceConditions(clients, ns, name)...)
			}

			fmt.Println()
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
			Message: fmt.Sprintf("CRD not found: %v\n          Install with: helm install openclaw-operator oci://ghcr.io/openclaw-rocks/charts/openclaw-operator", err),
		}
	}
	return checkResult{
		Name:   "OpenClawInstance CRD installed",
		Passed: true,
	}
}

func checkOperatorRunning(clients *kube.Clients) checkResult {
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

func checkWebhooks(clients *kube.Clients) checkResult {
	vwcs, err := clients.Kube.AdmissionregistrationV1().ValidatingWebhookConfigurations().List(
		context.TODO(), metav1.ListOptions{},
	)
	if err != nil {
		return checkResult{
			Name:    "Webhooks configured",
			Passed:  false,
			Message: fmt.Sprintf("Failed to list webhooks: %v", err),
		}
	}

	for _, vwc := range vwcs.Items {
		for _, wh := range vwc.Webhooks {
			for _, rule := range wh.Rules {
				for _, group := range rule.APIGroups {
					if group == "openclaw.rocks" {
						return checkResult{
							Name:    "Webhooks configured",
							Passed:  true,
							Message: fmt.Sprintf("Validating webhook: %s", vwc.Name),
						}
					}
				}
			}
		}
	}

	return checkResult{
		Name:    "Webhooks configured",
		Passed:  false,
		Message: "No validating webhooks found for openclaw.rocks API group",
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
		LabelSelector: podLabelSelector(name),
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
			Message: "No pods found. Run: kubectl openclaw events " + name,
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

func checkInstanceStorage(clients *kube.Clients, ns, name string) checkResult {
	obj, err := clients.Dynamic.Resource(kube.OpenClawGVR).Namespace(ns).Get(
		context.TODO(), name, metav1.GetOptions{},
	)
	if err != nil {
		return checkResult{
			Name:    fmt.Sprintf("Storage for %q is ready", name),
			Passed:  false,
			Message: err.Error(),
		}
	}

	status, _, _ := unstructuredNestedMap(obj.Object, "status")
	managed, _, _ := unstructuredNestedMap(status, "managedResources")
	pvcName := getNestedString(managed, "pvc")
	if pvcName == "" {
		spec, _, _ := unstructuredNestedMap(obj.Object, "spec")
		enabled, ok := getNestedBool(spec, "storage", "persistence", "enabled")
		if ok && !enabled {
			return checkResult{
				Name:    fmt.Sprintf("Storage for %q is ready", name),
				Passed:  true,
				Message: "Persistence disabled",
			}
		}
		return checkResult{
			Name:    fmt.Sprintf("Storage for %q is ready", name),
			Passed:  false,
			Message: "No PVC found in managed resources",
		}
	}

	pvc, err := clients.Kube.CoreV1().PersistentVolumeClaims(ns).Get(
		context.TODO(), pvcName, metav1.GetOptions{},
	)
	if err != nil {
		return checkResult{
			Name:    fmt.Sprintf("Storage for %q is ready", name),
			Passed:  false,
			Message: fmt.Sprintf("PVC %s not found: %v", pvcName, err),
		}
	}

	if pvc.Status.Phase == "Bound" {
		size := ""
		if qty, ok := pvc.Spec.Resources.Requests["storage"]; ok {
			size = qty.String()
		}
		return checkResult{
			Name:    fmt.Sprintf("Storage for %q is ready", name),
			Passed:  true,
			Message: fmt.Sprintf("PVC %s bound (%s)", pvcName, size),
		}
	}

	return checkResult{
		Name:    fmt.Sprintf("Storage for %q is ready", name),
		Passed:  false,
		Message: fmt.Sprintf("PVC %s is in phase %s", pvcName, pvc.Status.Phase),
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
