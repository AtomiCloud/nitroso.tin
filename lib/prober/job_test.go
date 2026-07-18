package prober

import (
	"context"
	"strings"
	"testing"

	"github.com/AtomiCloud/nitroso-tin/system/config"
	"github.com/rs/zerolog"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestJobCreatorBuildsDeterministicCacheOnlyJob(t *testing.T) {
	kube := fake.NewSimpleClientset()
	logger := zerolog.Nop()
	container := corev1.Container{
		Name: "spawner", Image: "ghcr.io/atomicloud/nitroso.tin/nitroso-tin:v1",
		EnvFrom:      []corev1.EnvFromSource{{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "tin"}}}},
		VolumeMounts: []corev1.VolumeMount{{Name: "config", MountPath: "/app/config"}},
	}
	creator := NewJobCreator(kube, "nitroso", container,
		[]corev1.Volume{{Name: "config"}}, config.AppConfig{Landscape: "pichu", Platform: "nitroso", Service: "tin"},
		2, "1", "1Gi", &logger)
	targets := []Target{{Direction: "JToW", Date: "01-01-2027", Time: "08:30:00", Needed: 2}}

	if err := creator.Create(context.Background(), 123, 4, 1, targets); err != nil {
		t.Fatal(err)
	}
	// A second spawner replica sees AlreadyExists and treats it as success.
	if err := creator.Create(context.Background(), 123, 4, 1, targets); err != nil {
		t.Fatalf("idempotent create failed: %v", err)
	}

	job, err := kube.BatchV1().Jobs("nitroso").Get(context.Background(), "tin-prober-123-4-1", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if *job.Spec.BackoffLimit != 0 || *job.Spec.TTLSecondsAfterFinished != 300 {
		t.Fatalf("unsafe lifecycle config: %#v", job.Spec)
	}
	if job.Spec.ActiveDeadlineSeconds == nil || *job.Spec.ActiveDeadlineSeconds != 245 {
		t.Fatalf("active deadline = %v, want 245 seconds", job.Spec.ActiveDeadlineSeconds)
	}
	if len(job.OwnerReferences) != 0 {
		t.Fatalf("Jobs must survive spawner Pod replacement: %#v", job.OwnerReferences)
	}
	got := job.Spec.Template.Spec.Containers[0]
	if got.Image != container.Image || strings.Join(got.Command, " ") != "/app/nitroso-tin prober" {
		t.Fatalf("unexpected prober container: %#v", got)
	}
	args := strings.Join(got.Args, " ")
	if !strings.Contains(args, `"needed":2`) || strings.Contains(strings.ToLower(args), "userdata") {
		t.Fatalf("args must contain demand only, never session data: %s", args)
	}
	if len(got.EnvFrom) != 1 || got.EnvFrom[0].SecretRef.Name != "tin" {
		t.Fatalf("spawned Job did not inherit runtime secret: %#v", got.EnvFrom)
	}
	assertContainerResources(t, got, "1", "1Gi")
}

func TestJobCreatorUsesDefaultResourcesWhenUnset(t *testing.T) {
	kube := fake.NewSimpleClientset()
	logger := zerolog.Nop()
	creator := NewJobCreator(kube, "nitroso", corev1.Container{}, nil, config.AppConfig{},
		1, "", "", &logger)

	if err := creator.Create(context.Background(), 456, 0, 0, nil); err != nil {
		t.Fatal(err)
	}
	job, err := kube.BatchV1().Jobs("nitroso").Get(context.Background(), "tin-prober-456-0-0", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assertContainerResources(t, job.Spec.Template.Spec.Containers[0], "250m", "128Mi")
}

func TestContainerFromPodRejectsEmptyPod(t *testing.T) {
	if _, err := ContainerFromPod(&corev1.Pod{}); err == nil {
		t.Fatal("expected empty pod to fail")
	}
}

func assertContainerResources(t *testing.T, container corev1.Container, cpu, memory string) {
	t.Helper()
	wantCPU := resource.MustParse(cpu)
	wantMemory := resource.MustParse(memory)
	for name, resources := range map[string]corev1.ResourceList{
		"requests": container.Resources.Requests,
		"limits":   container.Resources.Limits,
	} {
		gotCPU, ok := resources[corev1.ResourceCPU]
		if !ok || gotCPU.Cmp(wantCPU) != 0 {
			t.Errorf("%s CPU = %v, want %s", name, gotCPU.String(), cpu)
		}
		gotMemory, ok := resources[corev1.ResourceMemory]
		if !ok || gotMemory.Cmp(wantMemory) != 0 {
			t.Errorf("%s memory = %v, want %s", name, gotMemory.String(), memory)
		}
	}
}
