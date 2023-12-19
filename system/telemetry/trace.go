package telemetry

import (
	"context"
	"errors"
	"github.com/AtomiCloud/nitroso-tin/system/config"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	"strings"
	"time"
)

type TraceConfigurator struct {
	Cfg config.TraceConfig
}

func (t TraceConfigurator) Configure(ctx context.Context, res *resource.Resource) (*trace.TracerProvider, error) {
	return t.newProvider(ctx, res)
}

func (t TraceConfigurator) newProcessor(e trace.SpanExporter) (trace.TracerProviderOption, error) {

	c := t.Cfg.Processor

	switch strings.ToLower(c.ProcessorType) {
	case "sync":
		return trace.WithSyncer(e), nil
	case "batch":
		var opt []trace.BatchSpanProcessorOption
		if c.BatchProcessorConfig != nil {
			if c.BatchProcessorConfig.BatchTimeout != nil {
				opt = append(opt, trace.WithBatchTimeout(time.Duration(*c.BatchProcessorConfig.BatchTimeout)*time.Millisecond))
			}
			if c.BatchProcessorConfig.ExportTimeout != nil {
				opt = append(opt, trace.WithExportTimeout(time.Duration(*c.BatchProcessorConfig.ExportTimeout)*time.Millisecond))
			}
			if c.BatchProcessorConfig.Blocking != nil && *c.BatchProcessorConfig.Blocking {
				opt = append(opt, trace.WithBlocking())
			}
			if c.BatchProcessorConfig.BatchSize != nil {
				opt = append(opt, trace.WithMaxExportBatchSize(*c.BatchProcessorConfig.BatchSize))
			}
			if c.BatchProcessorConfig.QueueSize != nil {
				opt = append(opt, trace.WithMaxQueueSize(*c.BatchProcessorConfig.QueueSize))
			}
		}
		return trace.WithBatcher(e, opt...), nil
	default:
		return nil, errors.New("invalid trace processor type")
	}
}

func (t TraceConfigurator) newExporter(ctx context.Context) (trace.SpanExporter, error) {

	c := t.Cfg.Exporter

	switch strings.ToLower(c.ExporterType) {
	case "otlp":
		if c.Otlp == nil {
			return nil, errors.New("missing trace exporter configuration for OTLP")
		}
		switch strings.ToLower(c.Otlp.Protocol) {
		case "grpc":
			opt := []otlptracegrpc.Option{
				otlptracegrpc.WithEndpoint(c.Otlp.Endpoint),
			}
			if c.Otlp.Insecure != nil && *c.Otlp.Insecure {
				opt = append(opt, otlptracegrpc.WithInsecure())
			}
			if c.Otlp.Headers != nil {
				opt = append(opt, otlptracegrpc.WithHeaders(*c.Otlp.Headers))
			}
			if c.Otlp.Compression != nil {
				opt = append(opt, otlptracegrpc.WithCompressor(*c.Otlp.Compression))
			}
			if c.Otlp.Timeout != nil {
				opt = append(opt, otlptracegrpc.WithTimeout(time.Duration(*c.Otlp.Timeout)*time.Millisecond))
			}
			return otlptracegrpc.New(ctx, opt...)
		case "http":
			opt := []otlptracehttp.Option{
				otlptracehttp.WithEndpoint(c.Otlp.Endpoint),
			}
			if c.Otlp.Insecure != nil && *c.Otlp.Insecure {
				opt = append(opt, otlptracehttp.WithInsecure())
			}
			if c.Otlp.Headers != nil {
				opt = append(opt, otlptracehttp.WithHeaders(*c.Otlp.Headers))
			}
			if c.Otlp.Compression != nil {
				switch strings.ToLower(*c.Otlp.Compression) {
				case "gzip":
					opt = append(opt, otlptracehttp.WithCompression(otlptracehttp.GzipCompression))
				case "none":
					opt = append(opt, otlptracehttp.WithCompression(otlptracehttp.NoCompression))
				default:
					return nil, errors.New("invalid trace exporter compression type")
				}
			}
			if c.Otlp.Timeout != nil {
				opt = append(opt, otlptracehttp.WithTimeout(time.Duration(*c.Otlp.Timeout)*time.Millisecond))
			}
			return otlptracehttp.New(ctx, opt...)
		default:
			return nil, errors.New("invalid trace exporter protocol type")
		}

	case "console":
		var opt []stdouttrace.Option
		if c.Console != nil {
			if c.Console.PrettyPrint != nil && *c.Console.PrettyPrint {
				opt = append(opt, stdouttrace.WithPrettyPrint())
			}
			if c.Console.Timestamp != nil && !*c.Console.Timestamp {
				opt = append(opt, stdouttrace.WithoutTimestamps())
			}
		}
		return stdouttrace.New(opt...)
	default:
		return nil, errors.New("invalid trace exporter type")
	}
}

func (t TraceConfigurator) newProvider(ctx context.Context, res *resource.Resource) (*trace.TracerProvider, error) {

	traceExporter, err := t.newExporter(ctx)
	if err != nil {
		return nil, err
	}
	spanProcessor, err := t.newProcessor(traceExporter)
	if err != nil {
		return nil, err
	}
	traceProvider := trace.NewTracerProvider(spanProcessor, trace.WithResource(res))

	return traceProvider, nil
}
