package cluster

// controller.go
//
// Ergänzt Observer (nur-lesend) um Schreib-Operationen. Nutzt denselben
// ServiceAccount-Token / TLS-Client wie observer.go — daher im selben
// Package, keine neue Dependency (kein client-go nötig).
//
// RBAC: die ServiceAccount, unter der control-api läuft, braucht
// zusätzlich zu den Lese-Rechten aus observer.go:
//   apps/deployments:            patch
//   core/pods:                   list, delete
//   autoscaling/horizontalpodautoscalers: get, list
// siehe rbac-control-api-admin.yaml

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// podLabelSelector maps a deployment name to the label selector that
// actually matches its pods in this project. control-api and
// order-worker use app.kubernetes.io/name=<deployment>. The three
// restaurant deployments all share app.kubernetes.io/name=restaurant-worker
// (one Deployment template, reused per restaurant), so they must be
// selected via app.kubernetes.io/instance=<deployment> instead.
func podLabelSelector(deploymentName string) string {
	var selector string
	if strings.HasPrefix(deploymentName, "restaurant-") {
		selector = "app.kubernetes.io/instance=" + deploymentName
	} else {
		selector = "app.kubernetes.io/name=" + deploymentName
	}
	return url.QueryEscape(selector)
}

// RestartDeployment triggers a rolling restart, equivalent to
// "kubectl rollout restart deployment/<name>".
func (o *Observer) RestartDeployment(ctx context.Context, name string) error {
	patch := fmt.Sprintf(
		`{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":%q}}}}}`,
		time.Now().Format(time.RFC3339),
	)
	path := fmt.Sprintf("/apis/apps/v1/namespaces/%s/deployments/%s", o.namespace, name)
	return o.write(ctx, http.MethodPatch, path, "application/strategic-merge-patch+json", []byte(patch), nil)
}

// ScaleDeployment sets spec.replicas directly.
func (o *Observer) ScaleDeployment(ctx context.Context, name string, replicas int) error {
	patch := fmt.Sprintf(`{"spec":{"replicas":%d}}`, replicas)
	path := fmt.Sprintf("/apis/apps/v1/namespaces/%s/deployments/%s", o.namespace, name)
	return o.write(ctx, http.MethodPatch, path, "application/merge-patch+json", []byte(patch), nil)
}

// PodInstance is one running pod belonging to a controllable deployment.
type PodInstance struct {
	Name  string `json:"name"`
	Ready bool   `json:"ready"`
}

// PodsStatus is the live, authoritative pod state for one deployment,
// read directly from the Kubernetes API (no relay/poll delay, unlike
// the components list that flows through cluster-observer).
type PodsStatus struct {
	Deployment string        `json:"deployment"`
	Desired    int           `json:"desired"`
	Ready      int           `json:"ready"`
	Pods       []PodInstance `json:"pods"`
}

// PodInstances returns the live pods for a deployment plus its true
// spec.replicas, fetched directly from Kubernetes (bypasses the
// cluster-observer relay, so the dashboard can show an up-to-the-second
// count and the exact pod names affected by an admin action).
func (o *Observer) PodInstances(ctx context.Context, deploymentName string) (PodsStatus, error) {
	status := PodsStatus{Deployment: deploymentName, Pods: []PodInstance{}}

	deployment := struct {
		Spec struct {
			Replicas *int `json:"replicas"`
		} `json:"spec"`
	}{}
	deployPath := fmt.Sprintf("/apis/apps/v1/namespaces/%s/deployments/%s", o.namespace, deploymentName)
	if err := o.get(ctx, deployPath, &deployment); err != nil {
		return status, err
	}
	if deployment.Spec.Replicas != nil {
		status.Desired = *deployment.Spec.Replicas
	}

	list := struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Status struct {
				Conditions []struct {
					Type   string `json:"type"`
					Status string `json:"status"`
				} `json:"conditions"`
			} `json:"status"`
		} `json:"items"`
	}{}
	listPath := fmt.Sprintf("/api/v1/namespaces/%s/pods?labelSelector=%s", o.namespace, podLabelSelector(deploymentName))
	if err := o.get(ctx, listPath, &list); err != nil {
		return status, err
	}
	for _, item := range list.Items {
		ready := false
		for _, condition := range item.Status.Conditions {
			if condition.Type == "Ready" && condition.Status == "True" {
				ready = true
				break
			}
		}
		status.Pods = append(status.Pods, PodInstance{Name: item.Metadata.Name, Ready: ready})
		if ready {
			status.Ready++
		}
	}
	return status, nil
}

// ChaosKillPod deletes one random running pod belonging to the given
// deployment (selected via the project's pod labels), simulating a
// node/process failure so students can observe self-healing.
func (o *Observer) ChaosKillPod(ctx context.Context, deploymentName string) (killedPod string, err error) {
	list := struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
		} `json:"items"`
	}{}
	listPath := fmt.Sprintf("/api/v1/namespaces/%s/pods?labelSelector=%s", o.namespace, podLabelSelector(deploymentName))
	if err := o.get(ctx, listPath, &list); err != nil {
		return "", err
	}
	if len(list.Items) == 0 {
		return "", fmt.Errorf("keine Pods mit Label app=%s gefunden", deploymentName)
	}
	victim := list.Items[rand.Intn(len(list.Items))].Metadata.Name
	deletePath := fmt.Sprintf("/api/v1/namespaces/%s/pods/%s", o.namespace, victim)
	if err := o.write(ctx, http.MethodDelete, deletePath, "", nil, nil); err != nil {
		return "", err
	}
	return victim, nil
}

// HPAStatus is a trimmed view of a HorizontalPodAutoscaler for the dashboard.
type HPAStatus struct {
	Name            string `json:"name"`
	CurrentReplicas int    `json:"current_replicas"`
	DesiredReplicas int    `json:"desired_replicas"`
	MinReplicas     int    `json:"min_replicas"`
	MaxReplicas     int    `json:"max_replicas"`
	CPUPercent      *int   `json:"cpu_percent,omitempty"`
}

// HorizontalPodAutoscalers lists current HPA status in the namespace.
func (o *Observer) HorizontalPodAutoscalers(ctx context.Context) ([]HPAStatus, error) {
	raw := struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Spec struct {
				MinReplicas *int `json:"minReplicas"`
				MaxReplicas int  `json:"maxReplicas"`
			} `json:"spec"`
			Status struct {
				CurrentReplicas int `json:"currentReplicas"`
				DesiredReplicas int `json:"desiredReplicas"`
				CurrentMetrics  []struct {
					Resource *struct {
						Current struct {
							AverageUtilization *int `json:"averageUtilization"`
						} `json:"current"`
					} `json:"resource"`
				} `json:"currentMetrics"`
			} `json:"status"`
		} `json:"items"`
	}{}
	path := fmt.Sprintf("/apis/autoscaling/v2/namespaces/%s/horizontalpodautoscalers", o.namespace)
	if err := o.get(ctx, path, &raw); err != nil {
		return nil, err
	}

	out := make([]HPAStatus, 0, len(raw.Items))
	for _, item := range raw.Items {
		min := 0
		if item.Spec.MinReplicas != nil {
			min = *item.Spec.MinReplicas
		}
		status := HPAStatus{
			Name:            item.Metadata.Name,
			CurrentReplicas: item.Status.CurrentReplicas,
			DesiredReplicas: item.Status.DesiredReplicas,
			MinReplicas:     min,
			MaxReplicas:     item.Spec.MaxReplicas,
		}
		for _, m := range item.Status.CurrentMetrics {
			if m.Resource != nil && m.Resource.Current.AverageUtilization != nil {
				status.CPUPercent = m.Resource.Current.AverageUtilization
			}
		}
		out = append(out, status)
	}
	return out, nil
}

// write performs a non-GET request against the Kubernetes API using the
// same auth/TLS setup as get() in observer.go.
func (o *Observer) write(ctx context.Context, method, path, contentType string, body []byte, target any) error {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, o.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("create Kubernetes request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+o.token)
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response, err := o.client.Do(request)
	if err != nil {
		return fmt.Errorf("call Kubernetes API: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		return fmt.Errorf("Kubernetes API %s: %s", response.Status, strings.TrimSpace(string(errBody)))
	}
	if target != nil {
		return fmt.Errorf("decoding write responses not implemented") // not needed by current callers
	}
	return nil
}
