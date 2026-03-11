package cmd

import (
	"fmt"
	"time"

	"k8s.io/client-go/tools/clientcmd"
)

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

func getNestedBool(obj map[string]interface{}, keys ...string) (bool, bool) {
	current := obj
	for i, key := range keys {
		if i == len(keys)-1 {
			val, ok := current[key].(bool)
			return val, ok
		}
		next, ok := current[key].(map[string]interface{})
		if !ok {
			return false, false
		}
		current = next
	}
	return false, false
}

func getNestedInt64(obj map[string]interface{}, keys ...string) (int64, bool) {
	current := obj
	for i, key := range keys {
		if i == len(keys)-1 {
			switch v := current[key].(type) {
			case int64:
				return v, true
			case float64:
				return int64(v), true
			default:
				return 0, false
			}
		}
		next, ok := current[key].(map[string]interface{})
		if !ok {
			return 0, false
		}
		current = next
	}
	return 0, false
}

func getNestedSlice(obj map[string]interface{}, keys ...string) ([]interface{}, bool) {
	current := obj
	for i, key := range keys {
		if i == len(keys)-1 {
			val, ok := current[key].([]interface{})
			return val, ok
		}
		next, ok := current[key].(map[string]interface{})
		if !ok {
			return nil, false
		}
		current = next
	}
	return nil, false
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

func podLabelSelector(instanceName string) string {
	return fmt.Sprintf("app.kubernetes.io/name=openclaw,app.kubernetes.io/instance=%s", instanceName)
}
