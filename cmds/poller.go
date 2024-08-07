package cmds

import (
	"github.com/AtomiCloud/nitroso-tin/lib/count"
	"github.com/AtomiCloud/nitroso-tin/lib/otelredis"
	"github.com/AtomiCloud/nitroso-tin/lib/poller"
	"github.com/rs/xid"
	"github.com/urfave/cli/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"os"
)

func (state *State) Poller(c *cli.Context) error {

	ctx := c.Context

	kubectlCfg, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}
	// creates the clientset
	clientset, err := kubernetes.NewForConfig(kubectlCfg)
	if err != nil {
		panic(err.Error())
	}

	// get pod name and namespace
	podName := os.Getenv("POD_NAME")
	podNamespace := os.Getenv("POD_NAMESPACE")

	// panic if pod name or namespace is not set
	if podName == "" || podNamespace == "" {
		panic("POD_NAME or POD_NAMESPACE environment variable is not set")
	}

	// retrieve pod UID
	currentPod, err := clientset.CoreV1().Pods(podNamespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		panic(err.Error())
	}

	channel := make(chan string)
	uniqueID := xid.New().String()

	mainRedis := otelredis.New(state.Config.Cache["main"])
	streamRedis := otelredis.New(state.Config.Cache["stream"])

	countReader := count.New(state.Config.Buffer, &mainRedis, state.Logger, state.Ps, state.Location)

	job := poller.NewHeliumJobCreator(clientset, state.Config.Poller.Pollee, state.Config.App, state.Logger, podName, currentPod.UID)
	trigger := poller.NewTrigger(channel, state.Logger, &streamRedis, state.Config.Stream, state.Config.Poller, state.OtelConfigurator, state.Psm)

	p := poller.NewPoller(channel, job, trigger, state.Logger, state.Psm, state.Ps, countReader)

	err = p.Start(ctx, uniqueID)

	if err != nil {
		state.Logger.Error().Ctx(ctx).Err(err).Msg("Failed to start poller")
		return err
	}
	return nil
}
