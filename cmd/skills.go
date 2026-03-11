package cmd

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/openclaw-rocks/kubectl-openclaw/pkg/kube"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func newSkillsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skills [NAME]",
		Short: "Manage skills for an OpenClaw instance",
		Long: `List, add, or remove skills from an OpenClawInstance.
Skills can be ClawHub skill names or npm packages prefixed with "npm:".

Without a subcommand, lists the currently installed skills.`,
		Example: `  # List installed skills
  claw skills my-agent

  # Add skills
  claw skills add my-agent web-search code-analysis

  # Add an npm package as a skill
  claw skills add my-agent npm:@anthropic/tool-use

  # Remove a skill
  claw skills remove my-agent web-search`,
		Args: cobra.ExactArgs(1),
		RunE: skillsListRunE,
	}

	cmd.AddCommand(newSkillsAddCmd())
	cmd.AddCommand(newSkillsRemoveCmd())

	return cmd
}

func skillsListRunE(cmd *cobra.Command, args []string) error {
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
	skills, ok := getNestedSlice(spec, "skills")
	if !ok || len(skills) == 0 {
		fmt.Printf("No skills installed on %q.\n", name)
		fmt.Printf("\nAdd skills with:\n  kubectl openclaw skills add %s <skill-name>\n", name)
		return nil
	}

	fmt.Printf("Skills for %s/%s:\n", ns, name)
	for _, s := range skills {
		if str, ok := s.(string); ok {
			fmt.Printf("  - %s\n", str)
		}
	}

	// Show condition
	status, _, _ := unstructuredNestedMap(obj.Object, "status")
	condStatus := getConditionStatus(status, "SkillPacksReady")
	if condStatus == "False" {
		fmt.Printf("\nWarning: SkillPacksReady condition is False — some skills may not be resolved.\n")
	}

	return nil
}

func newSkillsAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add NAME SKILL [SKILL...]",
		Short: "Add skills to an OpenClaw instance",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			newSkills := args[1:]

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
			existing, _ := getNestedSlice(spec, "skills")

			// Dedup: build set of existing skills
			seen := make(map[string]bool)
			for _, s := range existing {
				if str, ok := s.(string); ok {
					seen[str] = true
				}
			}

			merged := make([]interface{}, len(existing))
			copy(merged, existing)

			added := 0
			for _, s := range newSkills {
				if !seen[s] {
					merged = append(merged, s)
					seen[s] = true
					added++
				}
			}

			if added == 0 {
				fmt.Println("All specified skills are already installed.")
				return nil
			}

			patch := map[string]interface{}{
				"spec": map[string]interface{}{
					"skills": merged,
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
				return fmt.Errorf("failed to update skills: %w", err)
			}

			fmt.Printf("Added %d skill(s) to %s/%s.\n", added, ns, name)
			return nil
		},
	}
}

func newSkillsRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove NAME SKILL [SKILL...]",
		Short: "Remove skills from an OpenClaw instance",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			toRemove := make(map[string]bool)
			for _, s := range args[1:] {
				toRemove[s] = true
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
			existing, _ := getNestedSlice(spec, "skills")

			var filtered []interface{}
			removed := 0
			for _, s := range existing {
				if str, ok := s.(string); ok && toRemove[str] {
					removed++
				} else {
					filtered = append(filtered, s)
				}
			}

			if removed == 0 {
				fmt.Println("None of the specified skills were found.")
				return nil
			}

			// Use empty slice instead of nil to clear all skills
			if filtered == nil {
				filtered = []interface{}{}
			}

			patch := map[string]interface{}{
				"spec": map[string]interface{}{
					"skills": filtered,
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
				return fmt.Errorf("failed to update skills: %w", err)
			}

			fmt.Printf("Removed %d skill(s) from %s/%s.\n", removed, ns, name)
			return nil
		},
	}
}
