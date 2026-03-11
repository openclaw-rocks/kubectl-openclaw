package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/openclaw-rocks/kubectl-openclaw/pkg/kube"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newBackupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "backup NAME",
		Short: "Show backup status for an OpenClaw instance",
		Long: `Display backup configuration and status for an OpenClawInstance including
the schedule, last backup time and path, active backup/restore jobs,
and CronJob information.`,
		Example: `  # View backup status
  kubectl openclaw backup my-agent

  # View backup status in specific namespace
  kubectl openclaw backup my-agent -n production`,
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
				return fmt.Errorf("failed to get instance %q: %w", name, err)
			}

			spec, _, _ := unstructuredNestedMap(obj.Object, "spec")
			status, _, _ := unstructuredNestedMap(obj.Object, "status")

			fmt.Printf("Backup Status: %s/%s\n\n", ns, name)

			// Schedule
			schedule := getNestedString(spec, "backup", "schedule")
			if schedule != "" {
				fmt.Printf("Schedule:          %s\n", schedule)
				historyLimit, ok := getNestedInt64(spec, "backup", "historyLimit")
				if ok {
					fmt.Printf("History Limit:     %d\n", historyLimit)
				}
				failedLimit, ok := getNestedInt64(spec, "backup", "failedHistoryLimit")
				if ok {
					fmt.Printf("Failed Limit:      %d\n", failedLimit)
				}
				timeout := getNestedString(spec, "backup", "timeout")
				if timeout != "" {
					fmt.Printf("Timeout:           %s\n", timeout)
				}
			} else {
				fmt.Println("Schedule:          (none - periodic backups disabled)")
			}
			fmt.Println()

			// Last backup
			lastPath := getNestedString(status, "lastBackupPath")
			lastTime := getNestedString(status, "lastBackupTime")
			if lastPath != "" {
				fmt.Println("Last Backup:")
				fmt.Printf("  Path:  %s\n", lastPath)
				if lastTime != "" {
					t, err := time.Parse(time.RFC3339, lastTime)
					if err == nil {
						fmt.Printf("  Time:  %s (%s ago)\n", lastTime, formatAge(t))
					} else {
						fmt.Printf("  Time:  %s\n", lastTime)
					}
				}
			} else {
				fmt.Println("Last Backup:       (none)")
			}
			fmt.Println()

			// Active jobs
			backupJob := getNestedString(status, "backupJobName")
			if backupJob != "" {
				fmt.Printf("Active Backup Job: %s\n", backupJob)
				backingSince := getNestedString(status, "backingUpSince")
				if backingSince != "" {
					fmt.Printf("  Started:         %s\n", backingSince)
				}
			}

			restoreJob := getNestedString(status, "restoreJobName")
			if restoreJob != "" {
				fmt.Printf("Active Restore Job: %s\n", restoreJob)
			}

			restoredFrom := getNestedString(status, "restoredFrom")
			if restoredFrom != "" {
				fmt.Printf("Restored From:     %s\n", restoredFrom)
			}

			// CronJob
			managed, ok, _ := unstructuredNestedMap(status, "managedResources")
			if ok {
				cronJob := getNestedString(managed, "backupCronJob")
				if cronJob != "" {
					fmt.Printf("\nCronJob:           %s\n", cronJob)
				}
			}

			return nil
		},
	}
}
