package telemetry

import (
	"context"
	"errors"
	"github.com/AtomiCloud/nitroso-tin/system/config"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"strings"
	"time"
)

type MetricConfigurator struct {
	Cfg config.MetricConfig
}

func (m MetricConfigurator) Configure(ctx context.Context, res *resource.Resource) (*metric.MeterProvider, error) {
	return m.newMeterProvider(ctx, res)
}

func (m MetricConfigurator) newReader(e metric.Exporter) (metric.Reader, error) {

	c := m.Cfg.Reader
	var opt []metric.PeriodicReaderOption
	if c.Interval != nil {
		opt = append(opt, metric.WithInterval(time.Duration(*c.Interval)*time.Millisecond))
	}
	if c.Timeout != nil {
		opt = append(opt, metric.WithTimeout(time.Duration(*c.Timeout)*time.Millisecond))
	}
	return metric.NewPeriodicReader(e, opt...), nil
}

func (m MetricConfigurator) newExporter(ctx context.Context) (metric.Exporter, error) {

	c := m.Cfg.Exporter

	switch strings.ToLower(c.ExporterType) {
	case "otlp":
		if c.Otlp == nil {
			return nil, errors.New("missing metric exporter configuration for OTLP")
		}
		switch strings.ToLower(c.Otlp.Protocol) {
		case "grpc":

			opt := []otlpmetricgrpc.Option{
				otlpmetricgrpc.WithEndpoint(c.Otlp.Endpoint),
			}
			if c.Otlp.Insecure != nil && *c.Otlp.Insecure {
				opt = append(opt, otlpmetricgrpc.WithInsecure())
			}
			if c.Otlp.Headers != nil {
				opt = append(opt, otlpmetricgrpc.WithHeaders(*c.Otlp.Headers))
			}
			if c.Otlp.Compression != nil {
				opt = append(opt, otlpmetricgrpc.WithCompressor(strings.ToLower(*c.Otlp.Compression)))
			}
			if c.Otlp.Timeout != nil {
				opt = append(opt, otlpmetricgrpc.WithTimeout(time.Duration(*c.Otlp.Timeout)*time.Millisecond))
			}
			return otlpmetricgrpc.New(ctx, opt...)
		case "http":
			opt := []otlpmetrichttp.Option{
				otlpmetrichttp.WithEndpoint(c.Otlp.Endpoint),
			}

			if c.Otlp.Insecure != nil && *c.Otlp.Insecure {
				opt = append(opt, otlpmetrichttp.WithInsecure())
			}
			if c.Otlp.Headers != nil {
				opt = append(opt, otlpmetrichttp.WithHeaders(*c.Otlp.Headers))
			}
			if c.Otlp.Compression != nil {
				switch strings.ToLower(*c.Otlp.Compression) {
				case "gzip":
					opt = append(opt, otlpmetrichttp.WithCompression(otlpmetrichttp.GzipCompression))
				case "none":
					opt = append(opt, otlpmetrichttp.WithCompression(otlpmetrichttp.NoCompression))
				default:
					return nil, errors.New("invalid metric exporter compression type")
				}
			}
			if c.Otlp.Timeout != nil {
				opt = append(opt, otlpmetrichttp.WithTimeout(time.Duration(*c.Otlp.Timeout)*time.Millisecond))
			}
			return otlpmetrichttp.New(ctx, opt...)
		default:
			return nil, errors.New("invalid metric exporter protocol type")
		}
	case "console":
		var opt []stdoutmetric.Option
		if c.Console != nil {
			if c.Console.PrettyPrint != nil && *c.Console.PrettyPrint {
				opt = append(opt, stdoutmetric.WithPrettyPrint())
			}
			if c.Console.Timestamp != nil && !*c.Console.Timestamp {
				opt = append(opt, stdoutmetric.WithoutTimestamps())
			}
		}
		return stdoutmetric.New(opt...)
	default:
		return nil, errors.New("invalid metrics exporter type")
	}

}

func (m MetricConfigurator) newMeterProvider(ctx context.Context, res *resource.Resource) (*metric.MeterProvider, error) {

	exporter, err := m.newExporter(ctx)
	if err != nil {
		return nil, err
	}
	reader, err := m.newReader(exporter)
	if err != nil {
		return nil, err
	}
	meterProvider := metric.NewMeterProvider(metric.WithResource(res), metric.WithReader(reader))

	return meterProvider, nil
}
