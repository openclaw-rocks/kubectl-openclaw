package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/openclaw-rocks/kubectl-openclaw/pkg/kube"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func newEnvCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "env [NAME]",
		Short: "Manage environment variables for an OpenClaw instance",
		Long: `List, set, or unset environment variables on an OpenClawInstance.

Without a subcommand, lists all configured env vars and envFrom sources.
For sensitive values like API keys, consider using Kubernetes Secrets
referenced via envFrom instead of plain-text env vars.`,
		Example: `  # List environment variables
  claw env my-agent

  # Set an environment variable
  claw env set my-agent ANTHROPIC_API_KEY=sk-ant-...

  # Set multiple variables
  claw env set my-agent KEY1=val1 KEY2=val2

  # Remove an environment variable
  claw env unset my-agent ANTHROPIC_API_KEY

  # Add a Secret as environment source
  claw env add-secret my-agent my-api-keys

  # Remove an environment source
  claw env remove-secret my-agent my-api-keys`,
		Args: cobra.ExactArgs(1),
		RunE: envListRunE,
	}

	cmd.AddCommand(newEnvSetCmd())
	cmd.AddCommand(newEnvUnsetCmd())
	cmd.AddCommand(newEnvAddSecretCmd())
	cmd.AddCommand(newEnvRemoveSecretCmd())

	return cmd
}

func envListRunE(cmd *cobra.Command, args []string) error {
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

	// Show env vars
	envVars, envOk := getNestedSlice(spec, "env")
	if envOk && len(envVars) > 0 {
		fmt.Printf("Environment Variables (%s/%s):\n", ns, name)
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "  NAME\tVALUE")
		for _, e := range envVars {
			em, ok := e.(map[string]interface{})
			if !ok {
				continue
			}
			eName := getNestedString(em, "name")
			eValue := getNestedString(em, "value")
			// Check for valueFrom
			if _, hasValueFrom := em["valueFrom"]; hasValueFrom {
				eValue = "(from secret/configmap)"
			}
			fmt.Fprintf(w, "  %s\t%s\n", eName, eValue)
		}
		w.Flush()
	} else {
		fmt.Printf("No environment variables set on %q.\n", name)
	}

	// Show envFrom sources
	envFrom, fromOk := getNestedSlice(spec, "envFrom")
	if fromOk && len(envFrom) > 0 {
		fmt.Println("\nEnvironment Sources:")
		for _, ef := range envFrom {
			efm, ok := ef.(map[string]interface{})
			if !ok {
				continue
			}
			if secretRef, ok := efm["secretRef"].(map[string]interface{}); ok {
				fmt.Printf("  Secret/%s\n", getNestedString(secretRef, "name"))
			}
			if cmRef, ok := efm["configMapRef"].(map[string]interface{}); ok {
				fmt.Printf("  ConfigMap/%s\n", getNestedString(cmRef, "name"))
			}
		}
	}

	if (!envOk || len(envVars) == 0) && (!fromOk || len(envFrom) == 0) {
		fmt.Printf("\nSet variables with:\n  kubectl openclaw env set %s KEY=VALUE\n", name)
		fmt.Printf("\nOr reference a Secret:\n  kubectl openclaw env add-secret %s SECRET_NAME\n", name)
	}

	return nil
}

func newEnvSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set NAME KEY=VALUE [KEY=VALUE...]",
		Short: "Set environment variables on an OpenClaw instance",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			pairs := args[1:]

			// Parse KEY=VALUE pairs
			newVars := make(map[string]string)
			for _, pair := range pairs {
				parts := strings.SplitN(pair, "=", 2)
				if len(parts) != 2 {
					return fmt.Errorf("invalid format %q — use KEY=VALUE", pair)
				}
				newVars[parts[0]] = parts[1]
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

			obj, err := clients.Dynamic.Resource(kube.OpenClawGVR).Namespace(ns).Get(
				context.TODO(), name, metav1.GetOptions{},
			)
			if err != nil {
				return fmt.Errorf("instance %q not found: %w", name, err)
			}

			spec, _, _ := unstructuredNestedMap(obj.Object, "spec")
			existing, _ := getNestedSlice(spec, "env")

			// Update existing or build new list
			updated := make(map[string]bool)
			var envList []interface{}
			for _, e := range existing {
				em, ok := e.(map[string]interface{})
				if !ok {
					envList = append(envList, e)
					continue
				}
				eName := getNestedString(em, "name")
				if val, found := newVars[eName]; found {
					envList = append(envList, map[string]interface{}{"name": eName, "value": val})
					updated[eName] = true
				} else {
					envList = append(envList, e)
				}
			}

			// Add new vars that weren't updates
			for k, v := range newVars {
				if !updated[k] {
					envList = append(envList, map[string]interface{}{"name": k, "value": v})
				}
			}

			patch := map[string]interface{}{
				"spec": map[string]interface{}{
					"env": envList,
				},
			}
			patchBytes, err := json.Marshal(patch)
			if err != nil {
				return fmt.Errorf("failed to create patch: %w", err)
			}

			_, err = clients.Dynamic.Resource(kube.OpenClawGVR).Namespace(ns).Patch(
				context.TODO(), name, types.MergePatchType, patchBytes, metav1.PatchOptions{},
			)
			if err != nil {
				return fmt.Errorf("failed to update env vars: %w", err)
			}

			fmt.Printf("Set %d environment variable(s) on %s/%s.\n", len(newVars), ns, name)
			return nil
		},
	}
}

func newEnvUnsetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unset NAME KEY [KEY...]",
		Short: "Remove environment variables from an OpenClaw instance",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			toRemove := make(map[string]bool)
			for _, k := range args[1:] {
				toRemove[k] = true
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

			obj, err := clients.Dynamic.Resource(kube.OpenClawGVR).Namespace(ns).Get(
				context.TODO(), name, metav1.GetOptions{},
			)
			if err != nil {
				return fmt.Errorf("instance %q not found: %w", name, err)
			}

			spec, _, _ := unstructuredNestedMap(obj.Object, "spec")
			existing, _ := getNestedSlice(spec, "env")

			var filtered []interface{}
			removed := 0
			for _, e := range existing {
				em, ok := e.(map[string]interface{})
				if ok {
					eName := getNestedString(em, "name")
					if toRemove[eName] {
						removed++
						continue
					}
				}
				filtered = append(filtered, e)
			}

			if removed == 0 {
				fmt.Println("None of the specified variables were found.")
				return nil
			}

			if filtered == nil {
				filtered = []interface{}{}
			}

			patch := map[string]interface{}{
				"spec": map[string]interface{}{
					"env": filtered,
				},
			}
			patchBytes, err := json.Marshal(patch)
			if err != nil {
				return fmt.Errorf("failed to create patch: %w", err)
			}

			_, err = clients.Dynamic.Resource(kube.OpenClawGVR).Namespace(ns).Patch(
				context.TODO(), name, types.MergePatchType, patchBytes, metav1.PatchOptions{},
			)
			if err != nil {
				return fmt.Errorf("failed to update env vars: %w", err)
			}

			fmt.Printf("Removed %d environment variable(s) from %s/%s.\n", removed, ns, name)
			return nil
		},
	}
}

func newEnvAddSecretCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add-secret NAME SECRET_NAME",
		Short: "Add a Secret as an environment source",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			secretName := args[1]

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
			existing, _ := getNestedSlice(spec, "envFrom")

			// Check if already referenced
			for _, ef := range existing {
				efm, ok := ef.(map[string]interface{})
				if !ok {
					continue
				}
				if secretRef, ok := efm["secretRef"].(map[string]interface{}); ok {
					if getNestedString(secretRef, "name") == secretName {
						fmt.Printf("Secret %q is already referenced.\n", secretName)
						return nil
					}
				}
			}

			newRef := map[string]interface{}{
				"secretRef": map[string]interface{}{
					"name": secretName,
				},
			}
			existing = append(existing, newRef)

			patch := map[string]interface{}{
				"spec": map[string]interface{}{
					"envFrom": existing,
				},
			}
			patchBytes, err := json.Marshal(patch)
			if err != nil {
				return fmt.Errorf("failed to create patch: %w", err)
			}

			_, err = clients.Dynamic.Resource(kube.OpenClawGVR).Namespace(ns).Patch(
				context.TODO(), name, types.MergePatchType, patchBytes, metav1.PatchOptions{},
			)
			if err != nil {
				return fmt.Errorf("failed to update envFrom: %w", err)
			}

			fmt.Printf("Added Secret/%s as environment source on %s/%s.\n", secretName, ns, name)
			return nil
		},
	}
}

func newEnvRemoveSecretCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove-secret NAME SECRET_NAME",
		Short: "Remove a Secret from environment sources",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			secretName := args[1]

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
			existing, _ := getNestedSlice(spec, "envFrom")

			var filtered []interface{}
			found := false
			for _, ef := range existing {
				efm, ok := ef.(map[string]interface{})
				if ok {
					if secretRef, ok := efm["secretRef"].(map[string]interface{}); ok {
						if getNestedString(secretRef, "name") == secretName {
							found = true
							continue
						}
					}
				}
				filtered = append(filtered, ef)
			}

			if !found {
				fmt.Printf("Secret %q not found in environment sources.\n", secretName)
				return nil
			}

			if filtered == nil {
				filtered = []interface{}{}
			}

			patch := map[string]interface{}{
				"spec": map[string]interface{}{
					"envFrom": filtered,
				},
			}
			patchBytes, err := json.Marshal(patch)
			if err != nil {
				return fmt.Errorf("failed to create patch: %w", err)
			}

			_, err = clients.Dynamic.Resource(kube.OpenClawGVR).Namespace(ns).Patch(
				context.TODO(), name, types.MergePatchType, patchBytes, metav1.PatchOptions{},
			)
			if err != nil {
				return fmt.Errorf("failed to update envFrom: %w", err)
			}

			fmt.Printf("Removed Secret/%s from environment sources on %s/%s.\n", secretName, ns, name)
			return nil
		},
	}
}
