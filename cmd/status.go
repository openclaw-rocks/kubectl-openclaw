package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/openclaw-rocks/kubectl-openclaw/pkg/kube"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status NAME",
		Short: "Show detailed status of an OpenClaw instance",
		Long: `Display comprehensive status of an OpenClawInstance including phase,
endpoints, sidecars, conditions, managed resources, backup/restore state,
auto-update status, and pod health.`,
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

			gen := obj.GetGeneration()
			observedGen, ok := getNestedInt64(status, "observedGeneration")
			if ok {
				if gen == observedGen {
					fmt.Printf("Generation:      %d (up to date)\n", gen)
				} else {
					fmt.Printf("Generation:      %d (observed: %d, reconciling...)\n", gen, observedGen)
				}
			}
			fmt.Println()

			printImageInfo(spec)
			printEndpoints(status)
			printSidecars(spec)
			printSkills(spec)
			printRuntimeDeps(spec)
			printResources(spec)
			printStorage(spec)
			printNetworking(spec)
			printBackupSummary(spec, status)
			printAutoUpdate(spec, status)
			printSelfConfigure(spec)
			printObservability(spec)
			printConditions(status)
			printManagedResources(status)
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
	case "Terminating":
		return phase + "  [terminating]"
	case "BackingUp":
		return phase + "  [backup in progress]"
	case "Restoring":
		return phase + "  [restore in progress]"
	case "Updating":
		return phase + "  [updating]"
	default:
		return phase
	}
}

func printImageInfo(spec map[string]interface{}) {
	image := getNestedString(spec, "image", "repository")
	tag := getNestedString(spec, "image", "tag")
	digest := getNestedString(spec, "image", "digest")
	pullPolicy := getNestedString(spec, "image", "pullPolicy")
	registry := getNestedString(spec, "registry")

	if image == "" {
		image = "ghcr.io/openclaw/openclaw"
	}
	if registry != "" {
		image = registry + "/" + image
	}

	if digest != "" {
		fmt.Printf("Image:           %s@%s\n", image, digest)
	} else {
		if tag == "" {
			tag = "latest"
		}
		fmt.Printf("Image:           %s:%s\n", image, tag)
	}
	if pullPolicy != "" {
		fmt.Printf("Pull Policy:     %s\n", pullPolicy)
	}
	fmt.Println()
}

func printEndpoints(status map[string]interface{}) {
	gateway := getNestedString(status, "gatewayEndpoint")
	canvas := getNestedString(status, "canvasEndpoint")
	if gateway == "" && canvas == "" {
		return
	}

	fmt.Println("Endpoints:")
	if gateway != "" {
		fmt.Printf("  Gateway (WebSocket): %s\n", gateway)
	}
	if canvas != "" {
		fmt.Printf("  Canvas (HTTP):       %s\n", canvas)
	}
	fmt.Println()
}

func printSidecars(spec map[string]interface{}) {
	var sidecars []string

	chromiumEnabled, ok := getNestedBool(spec, "chromium", "enabled")
	if ok && chromiumEnabled {
		detail := "enabled"
		persistenceEnabled, pOk := getNestedBool(spec, "chromium", "persistence", "enabled")
		if pOk && persistenceEnabled {
			detail += " [persistent]"
		}
		sidecars = append(sidecars, fmt.Sprintf("  Chromium:      %s", detail))
	}

	tailscaleEnabled, ok := getNestedBool(spec, "tailscale", "enabled")
	if ok && tailscaleEnabled {
		mode := getNestedString(spec, "tailscale", "mode")
		if mode == "" {
			mode = "serve"
		}
		hostname := getNestedString(spec, "tailscale", "hostname")
		detail := fmt.Sprintf("enabled (mode: %s", mode)
		if hostname != "" {
			detail += fmt.Sprintf(", hostname: %s", hostname)
		}
		detail += ")"
		sidecars = append(sidecars, fmt.Sprintf("  Tailscale:     %s", detail))
	}

	ollamaEnabled, ok := getNestedBool(spec, "ollama", "enabled")
	if ok && ollamaEnabled {
		detail := "enabled"
		models, mOk := getNestedSlice(spec, "ollama", "models")
		if mOk && len(models) > 0 {
			var names []string
			for _, m := range models {
				if s, ok := m.(string); ok {
					names = append(names, s)
				}
			}
			detail += fmt.Sprintf(" (models: %s)", strings.Join(names, ", "))
		}
		gpu, gOk := getNestedInt64(spec, "ollama", "gpu")
		if gOk && gpu > 0 {
			detail += fmt.Sprintf(" [%d GPU]", gpu)
		}
		sidecars = append(sidecars, fmt.Sprintf("  Ollama:        %s", detail))
	}

	webTermEnabled, ok := getNestedBool(spec, "webTerminal", "enabled")
	if ok && webTermEnabled {
		detail := "enabled"
		readOnly, rOk := getNestedBool(spec, "webTerminal", "readOnly")
		if rOk && readOnly {
			detail += " [read-only]"
		}
		sidecars = append(sidecars, fmt.Sprintf("  Web Terminal:  %s", detail))
	}

	if len(sidecars) > 0 {
		fmt.Println("Sidecars:")
		for _, s := range sidecars {
			fmt.Println(s)
		}
		fmt.Println()
	}
}

func printSkills(spec map[string]interface{}) {
	skills, ok := getNestedSlice(spec, "skills")
	if !ok || len(skills) == 0 {
		return
	}
	fmt.Println("Skills:")
	for _, s := range skills {
		if str, ok := s.(string); ok {
			fmt.Printf("  - %s\n", str)
		}
	}
	fmt.Println()
}

func printRuntimeDeps(spec map[string]interface{}) {
	pnpm, _ := getNestedBool(spec, "runtimeDeps", "pnpm")
	python, _ := getNestedBool(spec, "runtimeDeps", "python")
	if !pnpm && !python {
		return
	}
	fmt.Println("Runtime Dependencies:")
	if pnpm {
		fmt.Println("  pnpm: installed")
	}
	if python {
		fmt.Println("  python: installed (3.12 + uv)")
	}
	fmt.Println()
}

func printResources(spec map[string]interface{}) {
	reqCPU := getNestedString(spec, "resources", "requests", "cpu")
	reqMem := getNestedString(spec, "resources", "requests", "memory")
	limCPU := getNestedString(spec, "resources", "limits", "cpu")
	limMem := getNestedString(spec, "resources", "limits", "memory")
	if reqCPU == "" && reqMem == "" && limCPU == "" && limMem == "" {
		return
	}

	fmt.Println("Resources:")
	if reqCPU != "" || reqMem != "" {
		fmt.Printf("  Requests:  %s CPU, %s memory\n", reqCPU, reqMem)
	}
	if limCPU != "" || limMem != "" {
		fmt.Printf("  Limits:    %s CPU, %s memory\n", limCPU, limMem)
	}
	fmt.Println()
}

func printStorage(spec map[string]interface{}) {
	enabled, ok := getNestedBool(spec, "storage", "persistence", "enabled")
	if ok && !enabled {
		fmt.Println("Storage: disabled")
		fmt.Println()
		return
	}

	size := getNestedString(spec, "storage", "persistence", "size")
	storageClass := getNestedString(spec, "storage", "persistence", "storageClass")
	existingClaim := getNestedString(spec, "storage", "persistence", "existingClaim")

	if size == "" && existingClaim == "" {
		return
	}

	fmt.Println("Storage:")
	if existingClaim != "" {
		fmt.Printf("  Existing Claim:   %s\n", existingClaim)
	} else {
		fmt.Printf("  Size:             %s\n", size)
	}
	if storageClass != "" {
		fmt.Printf("  Storage Class:    %s\n", storageClass)
	}
	orphan, oOk := getNestedBool(spec, "storage", "persistence", "orphan")
	if oOk {
		fmt.Printf("  Orphan on Delete: %v\n", orphan)
	}
	fmt.Println()
}

func printNetworking(spec map[string]interface{}) {
	svcType := getNestedString(spec, "networking", "service", "type")
	ingressEnabled, ingressOk := getNestedBool(spec, "networking", "ingress", "enabled")
	npEnabled, npOk := getNestedBool(spec, "security", "networkPolicy", "enabled")

	if svcType == "" && !ingressOk && !npOk {
		return
	}

	fmt.Println("Networking:")
	if svcType != "" {
		fmt.Printf("  Service Type:    %s\n", svcType)
	}
	if npOk {
		if npEnabled {
			fmt.Printf("  Network Policy:  enabled\n")
		} else {
			fmt.Printf("  Network Policy:  disabled\n")
		}
	}
	if ingressOk && ingressEnabled {
		hosts, hOk := getNestedSlice(spec, "networking", "ingress", "hosts")
		if hOk && len(hosts) > 0 {
			var hostNames []string
			for _, h := range hosts {
				if hm, ok := h.(map[string]interface{}); ok {
					host := getNestedString(hm, "host")
					if host != "" {
						hostNames = append(hostNames, host)
					}
				}
			}
			fmt.Printf("  Ingress:         enabled (%s)\n", strings.Join(hostNames, ", "))
		} else {
			fmt.Printf("  Ingress:         enabled\n")
		}
	}
	fmt.Println()
}

func printBackupSummary(spec, status map[string]interface{}) {
	schedule := getNestedString(spec, "backup", "schedule")
	lastPath := getNestedString(status, "lastBackupPath")
	lastTime := getNestedString(status, "lastBackupTime")
	restoredFrom := getNestedString(status, "restoredFrom")

	if schedule == "" && lastPath == "" && restoredFrom == "" {
		return
	}

	fmt.Println("Backup:")
	if schedule != "" {
		fmt.Printf("  Schedule:    %s\n", schedule)
	}
	if lastPath != "" {
		fmt.Printf("  Last Path:   %s\n", lastPath)
	}
	if lastTime != "" {
		fmt.Printf("  Last Time:   %s\n", lastTime)
	}
	if restoredFrom != "" {
		fmt.Printf("  Restored:    %s\n", restoredFrom)
	}
	fmt.Println()
}

func printAutoUpdate(spec, status map[string]interface{}) {
	enabled, ok := getNestedBool(spec, "autoUpdate", "enabled")
	if !ok || !enabled {
		return
	}

	fmt.Println("Auto-Update:")
	checkInterval := getNestedString(spec, "autoUpdate", "checkInterval")
	if checkInterval != "" {
		fmt.Printf("  Check Interval:  %s\n", checkInterval)
	}

	autoUpdateStatus, ok, _ := unstructuredNestedMap(status, "autoUpdate")
	if ok {
		current := getNestedString(autoUpdateStatus, "currentVersion")
		latest := getNestedString(autoUpdateStatus, "latestVersion")
		pending := getNestedString(autoUpdateStatus, "pendingVersion")
		updatePhase := getNestedString(autoUpdateStatus, "updatePhase")
		lastErr := getNestedString(autoUpdateStatus, "lastUpdateError")

		if current != "" {
			fmt.Printf("  Current:         %s\n", current)
		}
		if latest != "" {
			fmt.Printf("  Latest:          %s\n", latest)
		}
		if pending != "" {
			fmt.Printf("  Pending:         %s\n", pending)
		}
		if updatePhase != "" {
			fmt.Printf("  Update Phase:    %s\n", updatePhase)
		}
		if lastErr != "" {
			fmt.Printf("  Last Error:      %s\n", lastErr)
		}
	}
	fmt.Println()
}

func printSelfConfigure(spec map[string]interface{}) {
	enabled, ok := getNestedBool(spec, "selfConfigure", "enabled")
	if !ok || !enabled {
		return
	}

	actions, aOk := getNestedSlice(spec, "selfConfigure", "allowedActions")
	if aOk && len(actions) > 0 {
		var actionNames []string
		for _, a := range actions {
			if s, ok := a.(string); ok {
				actionNames = append(actionNames, s)
			}
		}
		fmt.Printf("Self-Configure:  enabled (%s)\n\n", strings.Join(actionNames, ", "))
	} else {
		fmt.Println("Self-Configure:  enabled")
		fmt.Println()
	}
}

func printObservability(spec map[string]interface{}) {
	metricsEnabled, mOk := getNestedBool(spec, "observability", "metrics", "enabled")
	logLevel := getNestedString(spec, "observability", "logging", "level")
	logFormat := getNestedString(spec, "observability", "logging", "format")

	if !mOk && logLevel == "" {
		return
	}

	fmt.Println("Observability:")
	if mOk {
		if metricsEnabled {
			port, pOk := getNestedInt64(spec, "observability", "metrics", "port")
			if pOk {
				fmt.Printf("  Metrics:         enabled (port: %d)\n", port)
			} else {
				fmt.Printf("  Metrics:         enabled\n")
			}
			smEnabled, _ := getNestedBool(spec, "observability", "metrics", "serviceMonitor", "enabled")
			if smEnabled {
				fmt.Printf("  ServiceMonitor:  enabled\n")
			}
			prEnabled, _ := getNestedBool(spec, "observability", "metrics", "prometheusRule", "enabled")
			if prEnabled {
				fmt.Printf("  PrometheusRule:  enabled\n")
			}
			gdEnabled, _ := getNestedBool(spec, "observability", "metrics", "grafanaDashboard", "enabled")
			if gdEnabled {
				fmt.Printf("  Grafana:         enabled\n")
			}
		} else {
			fmt.Printf("  Metrics:         disabled\n")
		}
	}
	if logLevel != "" {
		fmt.Printf("  Log Level:       %s\n", logLevel)
	}
	if logFormat != "" {
		fmt.Printf("  Log Format:      %s\n", logFormat)
	}
	fmt.Println()
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
		{"StatefulSet", "statefulSet"},
		{"Deployment", "deployment"},
		{"Service", "service"},
		{"ConfigMap", "configMap"},
		{"PVC", "pvc"},
		{"Chromium PVC", "chromiumPVC"},
		{"NetworkPolicy", "networkPolicy"},
		{"PDB", "podDisruptionBudget"},
		{"HPA", "horizontalPodAutoscaler"},
		{"ServiceAccount", "serviceAccount"},
		{"Role", "role"},
		{"RoleBinding", "roleBinding"},
		{"Gateway Secret", "gatewayTokenSecret"},
		{"Basic Auth Secret", "basicAuthSecret"},
		{"Tailscale Secret", "tailscaleStateSecret"},
		{"Backup CronJob", "backupCronJob"},
		{"PrometheusRule", "prometheusRule"},
		{"Grafana (Operator)", "grafanaDashboardOperator"},
		{"Grafana (Instance)", "grafanaDashboardInstance"},
	}

	fmt.Println("Managed Resources:")
	found := false
	for _, r := range resources {
		val := getNestedString(managed, r.key)
		if val != "" {
			fmt.Printf("  %-22s %s\n", r.label+":", val)
			found = true
		}
	}
	if !found {
		fmt.Println("  (none)")
	}
	fmt.Println()
}

func printPodStatus(clients *kube.Clients, ns, instanceName string) {
	pods, err := clients.Kube.CoreV1().Pods(ns).List(context.TODO(), metav1.ListOptions{
		LabelSelector: podLabelSelector(instanceName),
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

	pod := pods.Items[0]
	allStatuses := append(pod.Status.InitContainerStatuses, pod.Status.ContainerStatuses...)
	if len(allStatuses) > 0 {
		fmt.Println("Containers:")
		w = tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "  NAME\tREADY\tSTATE\tRESTARTS")
		for _, cs := range allStatuses {
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
