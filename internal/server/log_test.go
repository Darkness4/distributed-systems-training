package server

import (
	"context"
	"crypto/tls"
	logv1 "distributed-systems/gen/log/v1"
	"distributed-systems/gen/log/v1/logv1connect"
	"distributed-systems/internal/auth"
	internalhttp "distributed-systems/internal/http"
	"distributed-systems/internal/log"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

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

	clog := log.NewLog(dir, log.Config{})
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

	auth := auth.New(aclModelFile, aclPolicyFile)

	r := http.NewServeMux()
	path, handler := NewLogAPIHandler(cfg, connect.WithInterceptors(auth.Interceptor()))
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
