package server

import (
	"context"
	"crypto/tls"
	logv1 "distributed-systems/gen/log/v1"
	"distributed-systems/gen/log/v1/logv1connect"
	"distributed-systems/internal/auth"
	internalhttp "distributed-systems/internal/http"
	"distributed-systems/internal/log"
	"distributed-systems/internal/otel"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"

	"connectrpc.com/connect"
	"connectrpc.com/otelconnect"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

var debug = flag.Bool("debug", false, "Enable observability for debugging")

func TestMain(m *testing.M) {
	flag.Parse()
	if *debug {
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}
	os.Exit(m.Run())
}

const (
	rootClientCert   = "certs/root-client/tls.test.crt"
	rootClientKey    = "certs/root-client/tls.test.key"
	nobodyClientCert = "certs/nobody-client/tls.test.crt"
	nobodyClientKey  = "certs/nobody-client/tls.test.key"
	caCert           = "certs/ca/tls.test.crt"
	serverCert       = "certs/server/tls.test.crt"
	serverKey        = "certs/server/tls.test.key"
	serverName       = "localhost"
	aclPolicyFile    = "acl/policy.csv"
	aclModelFile     = "acl/model.conf"
)

func TestServer(t *testing.T) {
	for scenario, fn := range map[string]func(
		t *testing.T,
		rootClient, nobodyClient logv1connect.LogAPIClient,
	){
		"produce/consume a message to/from the log succeeeds": testProduceConsume,
		"produce/consume stream succeeds":                     testProduceConsumeStream,
		"consume past log boundary fails":                     testConsumePastBoundary,
		"unauthorized fails":                                  testUnauthorized,
	} {
		t.Run(scenario, func(t *testing.T) {
			fmt.Println("Running test: ", scenario)
			rootClient, nobodyClient, teardown := setupTest(t)
			defer teardown()
			fn(t, rootClient, nobodyClient)
		})
	}
}

// nolint: ireturn
func setupTest(t *testing.T) (
	rootClient, nobodyClient logv1connect.LogAPIClient,
	teardown func(),
) {
	t.Helper()

	dir, err := os.MkdirTemp("", "server-test")
	require.NoError(t, err)

	clog, err := log.NewLog(dir, log.Config{})
	require.NoError(t, err)
	cfg := &Config{
		CommitLog: clog,
	}

	tlsConfig := &tls.Config{}
	if err := internalhttp.SetupServerTLSConfig(serverCert, serverKey, caCert, serverName, tlsConfig); err != nil {
		slog.Error("error setting up tls", "error", err)
		tlsConfig = nil
	}

	l, err := tls.Listen("tcp", "localhost:0", tlsConfig)
	require.NoError(t, err)

	// OTEL
	var metricExporter metric.Exporter
	var traceExporter trace.SpanExporter
	if *debug {
		metricsLogFile, err := os.Create(fmt.Sprintf("metrics-%d.log", time.Now().UnixMilli()))
		require.NoError(t, err)
		metricExporter, err = stdoutmetric.New(stdoutmetric.WithWriter(metricsLogFile))
		require.NoError(t, err)

		tracesLogFile, err := os.Create(fmt.Sprintf("trace-%d.log", time.Now().UnixMilli()))
		require.NoError(t, err)
		traceExporter, err = stdouttrace.New(stdouttrace.WithWriter(tracesLogFile))
		require.NoError(t, err)
	}

	traceProvider, meterProvider, prop, otelShutdown, err := otel.SetupOTelSDK(
		context.Background(),
		metricExporter,
		traceExporter,
	)
	require.NoError(t, err)
	otel, err := otelconnect.NewInterceptor(
		otelconnect.WithTracerProvider(traceProvider),
		otelconnect.WithMeterProvider(meterProvider),
		otelconnect.WithPropagator(prop),
	)
	require.NoError(t, err)

	// Authorizer
	auth, err := auth.New(aclModelFile, aclPolicyFile)
	require.NoError(t, err)

	r := http.NewServeMux()
	path, handler := NewLogAPIHandler(
		cfg,
		connect.WithInterceptors(
			auth.Interceptor(),
			LoggingInterceptor(slog.With("component", "server")),
			otel,
		),
	)
	r.Handle(path, handler)
	require.NoError(t, err)

	srv := &http.Server{
		Handler: internalhttp.AuthMiddleware(h2c.NewHandler(r, &http2.Server{})),
	}
	go func() {
		if err := srv.Serve(l); err != http.ErrServerClosed {
			panic(err)
		}
	}()

	rootHTTP := internalhttp.NewTLSClient(rootClientCert, rootClientKey, caCert)
	rootClient = logv1connect.NewLogAPIClient(
		rootHTTP,
		"https://"+l.Addr().String(),
		connect.WithGRPC(),
	)

	nobodyHTTP := internalhttp.NewTLSClient(nobodyClientCert, nobodyClientKey, caCert)
	nobodyClient = logv1connect.NewLogAPIClient(
		nobodyHTTP,
		"https://"+l.Addr().String(),
		connect.WithGRPC(),
	)

	return rootClient, nobodyClient, func() {
		_ = srv.Shutdown(context.Background())
		_ = l.Close()
		_ = clog.Remove()
		_ = otelShutdown(context.Background())
		if metricExporter != nil && traceExporter != nil {
			time.Sleep(1500 * time.Millisecond)
			_ = metricExporter.Shutdown(context.Background())
			_ = traceExporter.Shutdown(context.Background())
		}
	}
}

func testProduceConsume(t *testing.T, rootClient, _ logv1connect.LogAPIClient) {
	ctx := context.Background()

	want := &logv1.Record{
		Value: []byte("hello world"),
	}

	produce, err := rootClient.Produce(
		ctx,
		&connect.Request[logv1.ProduceRequest]{
			Msg: &logv1.ProduceRequest{
				Record: want,
			},
		},
	)
	require.NoError(t, err)

	consume, err := rootClient.Consume(ctx, &connect.Request[logv1.ConsumeRequest]{
		Msg: &logv1.ConsumeRequest{
			Offset: produce.Msg.Offset,
		},
	})
	require.NoError(t, err)
	require.Equal(t, want.Value, consume.Msg.Record.Value)
	require.Equal(t, want.Offset, consume.Msg.Record.Offset)
}

func testConsumePastBoundary(
	t *testing.T,
	rootClient, _ logv1connect.LogAPIClient,
) {
	ctx := context.Background()

	produce, err := rootClient.Produce(ctx, &connect.Request[logv1.ProduceRequest]{
		Msg: &logv1.ProduceRequest{
			Record: &logv1.Record{
				Value: []byte("hello world"),
			},
		},
	})
	require.NoError(t, err)

	consume, err := rootClient.Consume(ctx, &connect.Request[logv1.ConsumeRequest]{
		Msg: &logv1.ConsumeRequest{
			Offset: produce.Msg.Offset + 1,
		},
	})
	require.Nil(t, consume)
	got := connect.CodeOf(err)
	want := connect.CodeOf(WrapToConnectError(&log.ErrOffsetOutOfRange{}))
	require.Equal(t, want, got)
}

func testProduceConsumeStream(
	t *testing.T,
	rootClient, _ logv1connect.LogAPIClient,
) {
	ctx := context.Background()

	records := []*logv1.Record{{
		Value:  []byte("first message"),
		Offset: 0,
	}, {
		Value:  []byte("second message"),
		Offset: 1,
	}}

	{
		stream := rootClient.ProduceStream(ctx)

		for offset, record := range records {
			err := stream.Send(&logv1.ProduceStreamRequest{
				Record: record,
			})
			require.NoError(t, err)
			res, err := stream.Receive()
			require.NoError(t, err)
			require.Equal(t, res.Offset, uint64(offset))
		}
	}

	{
		stream, err := rootClient.ConsumeStream(
			ctx,
			&connect.Request[logv1.ConsumeStreamRequest]{
				Msg: &logv1.ConsumeStreamRequest{
					Offset: 0,
				},
			},
		)
		require.NoError(t, err)

		for i, record := range records {
			_ = stream.Receive()
			require.NoError(t, stream.Err())
			require.Equal(t, stream.Msg().Record, &logv1.Record{
				Value:  record.Value,
				Offset: uint64(i),
			})
		}
	}
}

func testUnauthorized(
	t *testing.T,
	_, nobodyClient logv1connect.LogAPIClient,
) {
	ctx := context.Background()
	produce, err := nobodyClient.Produce(ctx,
		&connect.Request[logv1.ProduceRequest]{
			Msg: &logv1.ProduceRequest{
				Record: &logv1.Record{
					Value: []byte("hello world"),
				},
			},
		},
	)
	require.Nil(t, produce)
	got, want := connect.CodeOf(err), connect.CodePermissionDenied
	require.Equal(t, want, got)
	consume, err := nobodyClient.Consume(ctx, &connect.Request[logv1.ConsumeRequest]{
		Msg: &logv1.ConsumeRequest{
			Offset: 0,
		},
	})
	require.Nil(t, consume)
	got, want = connect.CodeOf(err), connect.CodePermissionDenied
	require.Equal(t, want, got)
}
