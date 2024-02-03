package telemetry

import (
	"context"
	"errors"
	"github.com/AtomiCloud/nitroso-tin/system/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.22.0"
)

type OtelConfigurator struct {
	App    config.AppConfig
	Otel   config.OtelConfig
	Trace  TraceConfigurator
	Metric MetricConfigurator
}

func (o OtelConfigurator) Configure(ctx context.Context) (shutdown func(context.Context) error, err error) {
	var shutdownFuncs []func(context.Context) error

	shutdown = func(ctx context.Context) error {
		var err error
		for _, fn := range shutdownFuncs {
			err = errors.Join(err, fn(ctx))
		}
		shutdownFuncs = nil
		return err
	}

	// handleErr calls shutdown for cleanup and makes sure that all errors are returned.
	handleErr := func(inErr error) {
		err = errors.Join(inErr, shutdown(ctx))
	}

	res, e := o.newResource()
	if e != nil {
		handleErr(e)
		return
	}

	// Set up propagator.
	prop := o.newPropagator()
	otel.SetTextMapPropagator(prop)

	if o.Otel.Trace.Enable {
		// Set up trace provider.
		tracerProvider, er := o.Trace.Configure(ctx, res)
		if er != nil {
			handleErr(er)
			return
		}
		shutdownFuncs = append(shutdownFuncs, tracerProvider.Shutdown)
		otel.SetTracerProvider(tracerProvider)

	}

	if o.Otel.Metric.Enable {
		// Set up meter provider.
		meterProvider, er := o.Metric.Configure(ctx, res)
		if er != nil {
			handleErr(er)
			return
		}
		shutdownFuncs = append(shutdownFuncs, meterProvider.Shutdown)
		otel.SetMeterProvider(meterProvider)
	}

	return
}

func (o OtelConfigurator) newResource() (*resource.Resource, error) {

	lpsm := resource.NewSchemaless([]attribute.KeyValue{
		attribute.String("atomicloud.landscape", o.App.Landscape),
		attribute.String("atomicloud.platform", o.App.Platform),
		attribute.String("atomicloud.service", o.App.Service),
		attribute.String("atomicloud.module", o.App.Module),
		attribute.String("atomicloud.version", o.App.Version),
	}...)

	def := resource.Default()
	n := resource.NewSchemaless(
		semconv.ServiceName(o.App.Platform+"."+o.App.Service+"."+o.App.Module),
		semconv.ServiceVersion(o.App.Version),
	)
	m, err := resource.Merge(def, n)
	if err != nil {
		return nil, err
	}
	return resource.Merge(m, lpsm)
}

func (o OtelConfigurator) newPropagator() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
}
