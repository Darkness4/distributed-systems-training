package otel

import (
	"context"
	"errors"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace"
)

// SetupOTelSDK bootstraps the OpenTelemetry pipeline.
// If it does not return an error, make sure to call shutdown for proper cleanup.
//
//nolint:ireturn
func SetupOTelSDK(
	ctx context.Context,
	metricExporter metric.Exporter,
	traceExporter trace.SpanExporter,
) (
	tracerProvider *trace.TracerProvider,
	meterProvider *metric.MeterProvider,
	propagator propagation.TextMapPropagator,
	shutdown func(context.Context) error,
	err error,
) {
	var shutdownFuncs []func(context.Context) error

	// shutdown calls cleanup functions registered via shutdownFuncs.
	// The errors from the calls are joined.
	// Each registered cleanup will be invoked once.
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

	// Set up propagator.
	propagator = newPropagator()
	otel.SetTextMapPropagator(propagator)

	// Set up trace provider.
	tracerProvider, err = newTraceProvider(traceExporter)
	if err != nil {
		handleErr(err)
		return
	}
	shutdownFuncs = append(shutdownFuncs, tracerProvider.Shutdown)
	otel.SetTracerProvider(tracerProvider)

	// Set up meter provider.
	meterProvider, err = newMeterProvider(metricExporter)
	if err != nil {
		handleErr(err)
		return
	}
	shutdownFuncs = append(shutdownFuncs, meterProvider.Shutdown)
	otel.SetMeterProvider(meterProvider)

	return
}

//nolint:ireturn
func newPropagator() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
}

func newTraceProvider(traceExporter trace.SpanExporter) (*trace.TracerProvider, error) {
	opts := []trace.TracerProviderOption{
		trace.WithSampler(trace.AlwaysSample()),
	}
	if traceExporter != nil {
		opts = append(opts,
			trace.WithBatcher(traceExporter,
				// Default is 5s. Set to 1s for demonstrative purposes.
				trace.WithBatchTimeout(time.Second),
			),
		)
	}

	traceProvider := trace.NewTracerProvider(
		opts...,
	)
	return traceProvider, nil
}

func newMeterProvider(metricExporter metric.Exporter) (*metric.MeterProvider, error) {
	opts := []metric.Option{}
	if metricExporter != nil {
		opts = append(opts,
			metric.WithReader(metric.NewPeriodicReader(metricExporter,
				// Default is 1m. Set to 3s for demonstrative purposes.
				metric.WithInterval(time.Second))),
		)
	}
	meterProvider := metric.NewMeterProvider(opts...)
	return meterProvider, nil
}
