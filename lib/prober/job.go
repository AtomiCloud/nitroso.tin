package prober

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/AtomiCloud/nitroso-tin/system/config"
	"github.com/rs/zerolog"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type JobCreator struct {
	kube       kubernetes.Interface
	namespace  string
	container  corev1.Container
	volumes    []corev1.Volume
	app        config.AppConfig
	jobMinutes int
	logger     *zerolog.Logger
}

func NewJobCreator(kube kubernetes.Interface, namespace string, container corev1.Container,
	volumes []corev1.Volume, app config.AppConfig, jobMinutes int,
	logger *zerolog.Logger) *JobCreator {
	return &JobCreator{kube: kube, namespace: namespace, container: container,
		volumes: volumes, app: app, jobMinutes: jobMinutes, logger: logger}
}

func JobName(epoch int64, shard, fanout int) string {
	return fmt.Sprintf("tin-prober-%d-%d-%d", epoch, shard, fanout)
}

func (c *JobCreator) Create(ctx context.Context, epoch int64, shard, fanout int, targets []Target) error {
	data, err := json.Marshal(targets)
	if err != nil {
		return fmt.Errorf("marshal prober targets: %w", err)
	}
	name := JobName(epoch, shard, fanout)
	container := c.container.DeepCopy()
	container.Name = "prober"
	container.Command = []string{"/app/nitroso-tin", "prober"}
	container.Args = []string{"--data", string(data), "--interval", fmt.Sprint(c.jobMinutes * 60), "--epoch", fmt.Sprint(epoch), "--job", name}
	container.LivenessProbe = nil
	container.ReadinessProbe = nil
	container.StartupProbe = nil
	container.Lifecycle = nil
	container.Ports = nil
	container.Resources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("250m"), corev1.ResourceMemory: resource.MustParse("128Mi")},
		Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("250m"), corev1.ResourceMemory: resource.MustParse("128Mi")},
	}
	container.Env = append(container.Env, corev1.EnvVar{Name: "ATOMI_APP__MODULE", Value: "prober"})

	labels := map[string]string{
		"atomi.cloud/landscape":     c.app.Landscape,
		"atomi.cloud/platform":      c.app.Platform,
		"atomi.cloud/service":       c.app.Service,
		"atomi.cloud/module":        "prober",
		"atomi.cloud/layer":         "2",
		"nitroso.atomi.cloud/epoch": fmt.Sprint(epoch),
	}
	backoff := int32(0)
	ttl := int32(300)
	activeDeadline := int64(c.jobMinutes*60 + 125)
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: c.namespace, Labels: labels},
		Spec: batchv1.JobSpec{
			BackoffLimit: &backoff, TTLSecondsAfterFinished: &ttl, ActiveDeadlineSeconds: &activeDeadline,
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: labels}, Spec: corev1.PodSpec{
				RestartPolicy:   corev1.RestartPolicyNever,
				SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: boolPtr(true)},
				Containers:      []corev1.Container{*container}, Volumes: c.volumes,
			}},
		},
	}
	_, err = c.kube.BatchV1().Jobs(c.namespace).Create(ctx, job, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		c.logger.Info().Str("job", name).Msg("Prober Job already exists")
		return nil
	}
	if err != nil {
		return fmt.Errorf("create prober Job %s: %w", name, err)
	}
	return nil
}

func ContainerFromPod(pod *corev1.Pod) (corev1.Container, error) {
	if len(pod.Spec.Containers) == 0 {
		return corev1.Container{}, errors.New("spawner pod has no containers")
	}
	return *pod.Spec.Containers[0].DeepCopy(), nil
}

func boolPtr(v bool) *bool { return &v }
