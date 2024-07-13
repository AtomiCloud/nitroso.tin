package poller

import (
	"context"
	"fmt"
	"github.com/AtomiCloud/nitroso-tin/system/config"
	"github.com/rs/xid"
	"github.com/rs/zerolog"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"strings"
)

type HeliumJobCreator struct {
	kubectl      *kubernetes.Clientset
	namespace    string
	version      string
	image        string
	appConfig    config.AppConfig
	configMapRef string
	secretRef    string
	logger       *zerolog.Logger
}

func NewHeliumJobCreator(kubectl *kubernetes.Clientset, polleeConfig config.PolleeConfig, appConfig config.AppConfig, logger *zerolog.Logger) *HeliumJobCreator {
	return &HeliumJobCreator{
		kubectl:      kubectl,
		namespace:    polleeConfig.Namespace,
		version:      polleeConfig.Version,
		image:        polleeConfig.Image,
		appConfig:    appConfig,
		configMapRef: polleeConfig.ConfigRef,
		secretRef:    polleeConfig.SecretRef,
		logger:       logger,
	}
}

func (h HeliumJobCreator) CreateJob(ctx context.Context, date, direction string) error {

	jobClient := h.kubectl.BatchV1().Jobs(h.namespace)

	var backOffLimit int32 = 0

	random := xid.New().String()

	name := fmt.Sprintf("helium-pollee-%s-%s-%s",
		strings.ToLower(date),
		strings.ToLower(direction),
		random)

	configMapKey := fmt.Sprintf("%s.config.yaml", h.appConfig.Landscape)

	labels := map[string]string{
		"atomi.cloud/landscape": h.appConfig.Landscape,
		"atomi.cloud/platform":  h.appConfig.Platform,
		"atomi.cloud/service":   "helium",
		"atomi.cloud/module":    "pollee",
		"atomi.cloud/layer":     "2",
	}

	meta := metav1.ObjectMeta{
		Name:      name,
		Namespace: h.namespace,
	}

	t := true
	f := false
	var onek int64 = 1000

	spec := v1.PodSpec{
		SecurityContext: &v1.PodSecurityContext{
			RunAsUser:    &onek,
			RunAsGroup:   &onek,
			RunAsNonRoot: &t,
			FSGroup:      &onek,
		},
		Containers: []v1.Container{
			{
				Name:  "helium-pollee",
				Image: fmt.Sprintf("%s:%s", h.image, h.version),
				Command: []string{
					"bun",
					"run",
					"index.js",
					"watch",
					"-d",
					date,
					"-f",
					direction,
					"-i",
					"180", // 3 minutes
				},
				SecurityContext: &v1.SecurityContext{
					AllowPrivilegeEscalation: &f,
					ReadOnlyRootFilesystem:   &t,
					RunAsNonRoot:             &t,
					RunAsGroup:               &onek,
					RunAsUser:                &onek,
					Capabilities: &v1.Capabilities{
						Drop: []v1.Capability{
							"ALL",
						},
					},
				},
				Env: []v1.EnvVar{
					{
						Name:  "LANDSCAPE",
						Value: h.appConfig.Landscape,
					},
				},
				EnvFrom: []v1.EnvFromSource{
					{
						SecretRef: &v1.SecretEnvSource{
							LocalObjectReference: v1.LocalObjectReference{
								Name: h.secretRef,
							},
						},
					},
				},
				VolumeMounts: []v1.VolumeMount{
					{
						Name:      "config-volume",
						MountPath: fmt.Sprintf("%s/%s", "/app/config/app", configMapKey),
						SubPath:   configMapKey,
					},
				},
			},
		},
		Volumes: []v1.Volume{
			{
				Name: "config-volume",
				VolumeSource: v1.VolumeSource{
					ConfigMap: &v1.ConfigMapVolumeSource{
						LocalObjectReference: v1.LocalObjectReference{
							Name: h.configMapRef,
						},
						Items: []v1.KeyToPath{
							{
								Key:  configMapKey,
								Path: configMapKey,
							},
						},
					},
				},
			},
		},
		RestartPolicy: v1.RestartPolicyNever,
	}

	ttl := int32(180)
	jb := &batchv1.Job{
		ObjectMeta: meta,
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: &ttl,
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: labels,
					Labels:      labels,
				},
				Spec: spec,
			},
			BackoffLimit: &backOffLimit,
		},
	}

	_, err := jobClient.Create(ctx, jb, metav1.CreateOptions{})
	if err != nil {
		h.logger.Info().Msg("Failed to create job")
		return err
	}
	return nil
}
