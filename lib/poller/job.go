package poller

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/AtomiCloud/nitroso-tin/lib/pool"
	"github.com/AtomiCloud/nitroso-tin/system/config"
	"github.com/rs/xid"
	"github.com/rs/zerolog"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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
	podName      string
	podUID       types.UID
	pool         *pool.Pool
	shardSize    int
}

// HeliumJob is one polling target: a (date, direction) pair. The marshalled
// array becomes multi-watch's -d (targets) argument.
type HeliumJob struct {
	Date string `json:"date"`
	From string `json:"from"`
}

// HeliumSetting is one polling stream config. The marshalled array becomes
// multi-watch's -s (settings) argument; helium runs the cross product of
// targets × settings. JSON tags must stay aligned with helium's StreamSpec.
type HeliumSetting struct {
	Mode    string `json:"mode"`            // "web" | "mobile"
	Type    string `json:"type"`            // "stateless" | "held"
	Delay   int    `json:"delay"`           // inter-poll sleep, milliseconds
	Proxy   bool   `json:"proxy"`           // false = direct (no proxy)
	SpoofIp bool   `json:"spoofIp"`         // true = send a spoofed X-Real-IP
	Token   string `json:"token,omitempty"` // mobile only: KTMB userData session
}

func NewHeliumJobCreator(kubectl *kubernetes.Clientset, polleeConfig config.PolleeConfig, appConfig config.AppConfig, logger *zerolog.Logger, podName string, podUID types.UID, p *pool.Pool, shardSize int) *HeliumJobCreator {
	return &HeliumJobCreator{
		kubectl:      kubectl,
		namespace:    polleeConfig.Namespace,
		version:      polleeConfig.Version,
		image:        polleeConfig.Image,
		appConfig:    appConfig,
		configMapRef: polleeConfig.ConfigRef,
		secretRef:    polleeConfig.SecretRef,
		logger:       logger,
		podName:      podName,
		podUID:       podUID,
		pool:         p,
		shardSize:    shardSize,
	}
}

// shardTargets splits targets into chunks of at most size. size <= 0 (or a
// single chunk that already fits) returns one chunk with all targets.
func shardTargets(targets []HeliumJob, size int) [][]HeliumJob {
	if size <= 0 || len(targets) <= size {
		return [][]HeliumJob{targets}
	}
	var shards [][]HeliumJob
	for i := 0; i < len(targets); i += size {
		end := i + size
		if end > len(targets) {
			end = len(targets)
		}
		shards = append(shards, targets[i:end])
	}
	return shards
}

// CreateMultiJob shards the targets into pods of at most shardSize streams each
// (1 stream = 1 date-direction target) and creates one helium job per shard.
func (h HeliumJobCreator) CreateMultiJob(ctx context.Context, job []HeliumJob) error {
	if len(job) == 0 {
		h.logger.Info().Ctx(ctx).Msg("No targets to poll, skipping helium job creation")
		return nil
	}

	shards := shardTargets(job, h.shardSize)
	h.logger.Info().Ctx(ctx).
		Int("targets", len(job)).
		Int("shardSize", h.shardSize).
		Int("pods", len(shards)).
		Msg("Sharding helium targets into pods")

	for i, shard := range shards {
		if err := h.createMultiPod(ctx, shard); err != nil {
			h.logger.Error().Ctx(ctx).Err(err).Int("shard", i).Msg("Failed to create helium pod shard")
			return err
		}
	}
	return nil
}

func (h HeliumJobCreator) createMultiPod(ctx context.Context, job []HeliumJob) error {

	jobClient := h.kubectl.BatchV1().Jobs(h.namespace)

	// targets (-d): all (date, direction) pairs this pod should poll.
	data, err := json.Marshal(job)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to marshal helium job ")
		return err
	}

	// Resolve one random KTMB userData from the session pool, once per job.
	// It fans out across every target via the single mobile stream below.
	token, err := h.pool.Pick(ctx)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to pick userData from pool for helium mobile stream")
		return err
	}

	// settings (-s): a single mobile stream per target — IP-spoofed, held
	// (cached search token), polling at a 10ms inter-poll delay.
	settings := []HeliumSetting{
		{Mode: "mobile", Type: "held", Delay: 10, Proxy: false, SpoofIp: true, Token: token},
	}
	settingsData, err := json.Marshal(settings)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to marshal helium settings")
		return err
	}

	var backOffLimit int32 = 0

	random := xid.New().String()

	name := fmt.Sprintf("helium-pollee-%s", random)

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
		OwnerReferences: []metav1.OwnerReference{
			{
				APIVersion: "v1",
				Kind:       "Pod",
				Name:       h.podName,
				UID:        h.podUID,
			},
		},
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
					"multi-watch",
					"-d",
					string(data),
					"-s",
					string(settingsData),
					"-i",
					"120", // 2 minutes
				},
				Resources: v1.ResourceRequirements{
					Limits: v1.ResourceList{
						v1.ResourceCPU:    resource.MustParse("1000m"),
						v1.ResourceMemory: resource.MustParse("1Gi"),
					},
					Requests: v1.ResourceList{
						v1.ResourceCPU:    resource.MustParse("1000m"),
						v1.ResourceMemory: resource.MustParse("1Gi"),
					},
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

	_, err = jobClient.Create(ctx, jb, metav1.CreateOptions{})
	if err != nil {
		h.logger.Info().Msg("Failed to create job")
		return err
	}
	return nil
}

func (h HeliumJobCreator) CreateJob(ctx context.Context, job HeliumJob) error {

	jobClient := h.kubectl.BatchV1().Jobs(h.namespace)

	var backOffLimit int32 = 0

	random := xid.New().String()

	name := fmt.Sprintf("helium-pollee-%s-%s-%s",
		strings.ToLower(job.Date),
		strings.ToLower(job.From),
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
		OwnerReferences: []metav1.OwnerReference{
			{
				APIVersion: "v1",
				Kind:       "Pod",
				Name:       h.podName,
				UID:        h.podUID,
			},
		},
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
					job.Date,
					"-f",
					job.From,
					"-i",
					"180", // 3 minutes
				},
				Resources: v1.ResourceRequirements{
					Limits: v1.ResourceList{
						v1.ResourceCPU:    resource.MustParse("1000m"),
						v1.ResourceMemory: resource.MustParse("1Gi"),
					},
					Requests: v1.ResourceList{
						v1.ResourceCPU:    resource.MustParse("1000m"),
						v1.ResourceMemory: resource.MustParse("1Gi"),
					},
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
