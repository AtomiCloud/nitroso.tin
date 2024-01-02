package otelredis

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/AtomiCloud/nitroso-tin/system/config"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
)

type OtelRedis struct {
	*redis.Client
}

type OtelPubSub struct {
	*redis.PubSub
	Channel string
}

type OtelRedisMessage struct {
	Message interface{}            `json:"message"`
	Context propagation.MapCarrier `json:"context"`
}

func New(cfg config.CacheConfig) OtelRedis {

	ssl := ""
	if cfg.Ssl {
		ssl = "s"
	}

	ep := fmt.Sprintf("redis%s://default:%s@%s", ssl, cfg.Password, cfg.Endpoints[0])
	opt, _ := redis.ParseURL(ep)
	rdb := redis.NewClient(opt)
	return OtelRedis{rdb}
}

func (r OtelRedis) QueuePush(ctx context.Context, tracer trace.Tracer, queue string, any interface{}) (*redis.IntCmd, error) {
	opts := []trace.SpanStartOption{
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(semconv.MessagingSystemKey.String("queue")),
	}

	ctx, childSpan := tracer.Start(ctx, "redis queue add to "+queue, opts...)
	defer childSpan.End()

	mc := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, mc)

	message := OtelRedisMessage{
		Message: any,
		Context: mc,
	}

	messageJson, err := json.Marshal(message)
	if err != nil {
		return nil, err
	}

	childSpan.SetAttributes(attribute.String("message", string(messageJson)))

	return r.LPush(ctx, queue, string(messageJson)), nil
}

func (r OtelRedis) QueuePop(ctx context.Context, tracer trace.Tracer, queue string, handler func(ctx context.Context, message json.RawMessage) error) error {
	opts := []trace.SpanStartOption{
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(semconv.MessagingSystemKey.String("queue")),
	}

	propagator := otel.GetTextMapPropagator()

	result := r.BRPop(ctx, 0, queue)

	popped, err := result.Result()
	if err != nil {
		return err
	}

	val := popped[1]

	var msg OtelRedisMessage
	err = json.Unmarshal([]byte(val), &msg)
	if err != nil {
		return err
	}

	ctx = propagator.Extract(ctx, msg.Context)
	marshal, err := json.Marshal(msg.Message)
	if err != nil {
		return err
	}
	ctx, childSpan := tracer.Start(ctx, "redis read from queue: "+queue, opts...)
	defer childSpan.End()
	return handler(ctx, marshal)
}

func (r OtelRedis) StreamAdd(ctx context.Context, tracer trace.Tracer, stream string, any interface{}) (*redis.StringCmd, error) {
	opts := []trace.SpanStartOption{
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(semconv.MessagingSystemKey.String("stream")),
	}

	ctx, childSpan := tracer.Start(ctx, "redis stream add to "+stream, opts...)
	defer childSpan.End()

	mc := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, mc)

	messageJson, err := json.Marshal(any)
	if err != nil {
		return nil, err
	}
	contextJson, err := json.Marshal(mc)
	if err != nil {
		return nil, err
	}

	childSpan.SetAttributes(attribute.String("message", string(messageJson)))
	childSpan.SetAttributes(attribute.String("context", string(contextJson)))

	return r.XAdd(ctx, &redis.XAddArgs{
		Stream: stream,

		MaxLen: 50,
		Values: map[string]interface{}{
			"message": messageJson,
			"context": contextJson,
		},
	}), nil
}

func (r OtelRedis) StreamRead(ctx context.Context, tracer trace.Tracer, stream string, handler func(ctx context.Context, message json.RawMessage) error) error {
	opts := []trace.SpanStartOption{
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(semconv.MessagingSystemKey.String("stream")),
	}

	propagator := otel.GetTextMapPropagator()

	result := r.XRead(ctx, &redis.XReadArgs{
		Streams: []string{stream, "0"},
		Count:   0,
		Block:   0,
	})

	streams, err := result.Result()
	if err != nil {
		return err
	}
	values := streams[0].Messages[0].Values
	msgJson := fmt.Sprintf("%v", values["message"])
	mcJson := fmt.Sprintf("%v", values["context"])

	var msg json.RawMessage
	err = json.Unmarshal([]byte(msgJson), &msg)
	if err != nil {
		return err
	}
	var mc propagation.MapCarrier
	err = json.Unmarshal([]byte(mcJson), &mc)
	if err != nil {
		return err
	}

	ctx = propagator.Extract(ctx, mc)
	ctx, childSpan := tracer.Start(ctx, "redis read from stream: "+stream, opts...)
	defer childSpan.End()
	marshal, err := json.Marshal(msg)
	return handler(ctx, marshal)
}

func (r OtelRedis) StreamGroupRead(ctx context.Context, tracer trace.Tracer, stream string, groupId, consumerId string, handler func(ctx context.Context, message json.RawMessage) error) error {
	opts := []trace.SpanStartOption{
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(semconv.MessagingSystemKey.String("stream")),
	}

	propagator := otel.GetTextMapPropagator()

	result := r.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    groupId,
		Consumer: consumerId,
		Streams:  []string{stream, ">"},
		Count:    1,
		Block:    0,
		NoAck:    false,
	})

	streams, err := result.Result()
	if err != nil {
		return err
	}
	values := streams[0].Messages[0].Values
	msgJson := fmt.Sprintf("%v", values["message"])
	mcJson := fmt.Sprintf("%v", values["context"])

	var msg json.RawMessage
	err = json.Unmarshal([]byte(msgJson), &msg)
	if err != nil {
		return err
	}
	var mc propagation.MapCarrier
	err = json.Unmarshal([]byte(mcJson), &mc)
	if err != nil {
		return err
	}
	ctx = propagator.Extract(ctx, mc)
	ctx, childSpan := tracer.Start(ctx, "redis read from stream: "+stream, opts...)
	defer childSpan.End()
	childSpan.SetAttributes(attribute.String("redis.consumer.group", groupId), attribute.String("redis.consumer.id", consumerId))
	return handler(ctx, msg)
}

func (r OtelRedis) Pub(ctx context.Context, tracer trace.Tracer, logger zerolog.Logger, channel string, any interface{}) (*redis.IntCmd, error) {

	opts := []trace.SpanStartOption{
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(semconv.MessagingSystemKey.String("pubsub")),
	}

	ctx, childSpan := tracer.Start(ctx, "redis publish to "+channel, opts...)
	defer childSpan.End()

	logger.Info().Ctx(ctx).Msg("Publishing to " + channel)

	mc := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, mc)
	message := OtelRedisMessage{
		Message: any,
		Context: mc,
	}

	logger.Info().Ctx(ctx).Msg("Marshaling JSON...")
	marshal, err := json.Marshal(message)
	if err != nil {
		return nil, err
	}
	childSpan.SetAttributes(attribute.String("message", string(marshal)))
	return r.Publish(ctx, channel, string(marshal)), nil
}

func (r OtelRedis) Sub(ctx context.Context, channel string) OtelPubSub {
	ps := r.Subscribe(ctx, channel)
	return OtelPubSub{ps, channel}
}

func (ps OtelPubSub) Recv(ctx context.Context, tracer trace.Tracer, handler func(ctx context.Context, message json.RawMessage) error) error {

	opts := []trace.SpanStartOption{
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(semconv.MessagingSystemKey.String("pubsub")),
	}

	propagator := otel.GetTextMapPropagator()

	message, err := ps.ReceiveMessage(ctx)
	if err != nil {
		return err
	}
	var msg OtelRedisMessage
	err = json.Unmarshal([]byte(message.Payload), &msg)
	if err != nil {
		return err
	}
	ctx = propagator.Extract(ctx, msg.Context)

	marshal, err := json.Marshal(msg.Message)
	if err != nil {
		return err
	}

	ctx, childSpan := tracer.Start(ctx, "redis receiving message from "+ps.Channel, opts...)
	defer childSpan.End()
	return handler(ctx, marshal)
}
