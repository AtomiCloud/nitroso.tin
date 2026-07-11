package cmds

import (
	"os"

	"github.com/AtomiCloud/nitroso-tin/lib/count"
	"github.com/AtomiCloud/nitroso-tin/lib/encryptor"
	"github.com/AtomiCloud/nitroso-tin/lib/enricher"
	"github.com/AtomiCloud/nitroso-tin/lib/ktmb"
	"github.com/AtomiCloud/nitroso-tin/lib/otelredis"
	"github.com/AtomiCloud/nitroso-tin/lib/prober"
	"github.com/AtomiCloud/nitroso-tin/lib/session"
	"github.com/urfave/cli/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func (state *State) Spawner(c *cli.Context) error {
	ctx := c.Context
	kubeConfig, err := rest.InClusterConfig()
	if err != nil {
		return err
	}
	kube, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return err
	}
	podName, namespace := os.Getenv("POD_NAME"), os.Getenv("POD_NAMESPACE")
	if podName == "" || namespace == "" {
		return cli.Exit("POD_NAME and POD_NAMESPACE are required", 1)
	}
	pod, err := kube.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	container, err := prober.ContainerFromPod(pod)
	if err != nil {
		return err
	}

	mainRedis := otelredis.New(state.Config.Cache["main"])
	ktmbConfig := state.Config.Ktmb
	k := ktmb.New(ktmbConfig.ApiUrl, ktmbConfig.AppUrl, ktmbConfig.RequestSignature, state.Logger, ktmbConfig.Proxy,
		ktmb.WarmConfig{PoolSize: ktmbConfig.WarmPoolSize, IntervalMs: ktmbConfig.WarmIntervalMs, DnsRefreshMs: ktmbConfig.DnsRefreshMs})
	storeEncryptor := encryptor.NewSymEncryptor[enricher.FindStore](state.Config.Encryptor.Key, state.Logger)
	sessionEncryptor := encryptor.NewSymEncryptor[ktmb.LoginRes](state.Config.Encryptor.Key, state.Logger)
	sharedSession := session.New(&k, &mainRedis, state.Logger, ktmbConfig.LoginKey, sessionEncryptor)
	finder := enricher.New(k, &sharedSession, state.Logger)
	store := prober.NewStore(&mainRedis, &sharedSession, &finder, storeEncryptor, state.Config.Enricher,
		ktmbConfig.LoginKey, state.Ps+":prober:session-dead", state.Logger)
	counts := count.New(state.Config.Buffer, &mainRedis, state.Logger, state.Ps, state.Location)
	jobs := prober.NewJobCreator(kube, namespace, container, pod.Spec.Volumes,
		state.Config.App, state.Config.Prober.JobMinutes, state.Logger)
	spawner := prober.NewSpawner(counts, store, jobs, &mainRedis, state.Config.Prober, state.Ps, state.Logger)
	state.Logger.Info().Msg("Starting epoch prober spawner")
	return spawner.Start(ctx)
}
