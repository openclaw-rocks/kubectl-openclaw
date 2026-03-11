package cmd

import (
	"context"
	"fmt"

	"github.com/openclaw-rocks/kubectl-openclaw/pkg/kube"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func newCreateCmd() *cobra.Command {
	var (
		imageRepo      string
		imageTag       string
		skills         []string
		storageSize    string
		storageClass   string
		cpuRequest     string
		memRequest     string
		chromium       bool
		ollama         bool
		ollamaModels   []string
		webTerminal    bool
		pnpm           bool
		python         bool
		selfConfigure  bool
	)

	cmd := &cobra.Command{
		Use:   "create NAME",
		Short: "Create a new OpenClaw instance",
		Long: `Create a new OpenClawInstance custom resource. The operator will provision
all managed resources (StatefulSet, Service, PVC, etc.) automatically.

Webhook defaults are applied for any values not explicitly set.`,
		Example: `  # Create with defaults
  claw create my-agent

  # Create with a specific version
  claw create my-agent --tag v1.2.3

  # Create with skills and chromium browser
  claw create my-agent --skills web-search,code-analysis --chromium

  # Create with custom resources
  claw create my-agent --cpu 1 --memory 2Gi --storage 20Gi

  # Create with Ollama for local models
  claw create my-agent --ollama --ollama-models llama3,codellama

  # Create with runtime dependencies
  claw create my-agent --pnpm --python`,
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

			spec := map[string]interface{}{}

			if imageRepo != "" || imageTag != "" {
				image := map[string]interface{}{}
				if imageRepo != "" {
					image["repository"] = imageRepo
				}
				if imageTag != "" {
					image["tag"] = imageTag
				}
				spec["image"] = image
			}

			if len(skills) > 0 {
				s := make([]interface{}, len(skills))
				for i, sk := range skills {
					s[i] = sk
				}
				spec["skills"] = s
			}

			if storageSize != "" || storageClass != "" {
				persistence := map[string]interface{}{}
				if storageSize != "" {
					persistence["size"] = storageSize
				}
				if storageClass != "" {
					persistence["storageClass"] = storageClass
				}
				spec["storage"] = map[string]interface{}{
					"persistence": persistence,
				}
			}

			if cpuRequest != "" || memRequest != "" {
				requests := map[string]interface{}{}
				if cpuRequest != "" {
					requests["cpu"] = cpuRequest
				}
				if memRequest != "" {
					requests["memory"] = memRequest
				}
				spec["resources"] = map[string]interface{}{
					"requests": requests,
				}
			}

			if chromium {
				spec["chromium"] = map[string]interface{}{"enabled": true}
			}

			if ollama {
				ollamaSpec := map[string]interface{}{"enabled": true}
				if len(ollamaModels) > 0 {
					models := make([]interface{}, len(ollamaModels))
					for i, m := range ollamaModels {
						models[i] = m
					}
					ollamaSpec["models"] = models
				}
				spec["ollama"] = ollamaSpec
			}

			if webTerminal {
				spec["webTerminal"] = map[string]interface{}{"enabled": true}
			}

			if pnpm || python {
				deps := map[string]interface{}{}
				if pnpm {
					deps["pnpm"] = true
				}
				if python {
					deps["python"] = true
				}
				spec["runtimeDeps"] = deps
			}

			if selfConfigure {
				spec["selfConfigure"] = map[string]interface{}{
					"enabled": true,
					"allowedActions": []interface{}{"skills", "config", "workspaceFiles", "envVars"},
				}
			}

			instance := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "openclaw.rocks/v1alpha1",
					"kind":       "OpenClawInstance",
					"metadata": map[string]interface{}{
						"name":      name,
						"namespace": ns,
					},
					"spec": spec,
				},
			}

			_, err = clients.Dynamic.Resource(kube.OpenClawGVR).Namespace(ns).Create(
				context.TODO(), instance, metav1.CreateOptions{},
			)
			if err != nil {
				return fmt.Errorf("failed to create instance: %w", err)
			}

			fmt.Printf("OpenClawInstance %s/%s created.\n", ns, name)
			fmt.Printf("\nMonitor provisioning:\n")
			fmt.Printf("  kubectl openclaw status %s\n", name)
			fmt.Printf("  kubectl openclaw events %s\n", name)
			return nil
		},
	}

	cmd.Flags().StringVar(&imageRepo, "image", "", "container image repository")
	cmd.Flags().StringVar(&imageTag, "tag", "", "image tag (default: latest)")
	cmd.Flags().StringSliceVar(&skills, "skills", nil, "skills to install (comma-separated)")
	cmd.Flags().StringVar(&storageSize, "storage", "", "PVC storage size (e.g. 10Gi)")
	cmd.Flags().StringVar(&storageClass, "storage-class", "", "storage class name")
	cmd.Flags().StringVar(&cpuRequest, "cpu", "", "CPU request (e.g. 500m, 1)")
	cmd.Flags().StringVar(&memRequest, "memory", "", "memory request (e.g. 1Gi)")
	cmd.Flags().BoolVar(&chromium, "chromium", false, "enable Chromium browser sidecar")
	cmd.Flags().BoolVar(&ollama, "ollama", false, "enable Ollama sidecar for local models")
	cmd.Flags().StringSliceVar(&ollamaModels, "ollama-models", nil, "Ollama models to pre-pull (comma-separated)")
	cmd.Flags().BoolVar(&webTerminal, "web-terminal", false, "enable web terminal sidecar")
	cmd.Flags().BoolVar(&pnpm, "pnpm", false, "install pnpm runtime dependency")
	cmd.Flags().BoolVar(&python, "python", false, "install Python 3.12 + uv runtime dependency")
	cmd.Flags().BoolVar(&selfConfigure, "self-configure", false, "allow the agent to modify its own configuration")

	return cmd
}
