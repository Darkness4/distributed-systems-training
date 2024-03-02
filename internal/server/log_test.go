package server

import (
	"context"
	logv1 "distributed-systems/gen/log/v1"
	"distributed-systems/gen/log/v1/logv1connect"
	internalhttp "distributed-systems/internal/http"
	"distributed-systems/internal/log"
	"fmt"
	"net"
	"net/http"
	"os"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

func TestServer(t *testing.T) {
	for scenario, fn := range map[string]func(
		t *testing.T,
		client logv1connect.LogAPIClient,
	){
		"produce/consume a message to/from the log succeeeds": testProduceConsume,
		"produce/consume stream succeeds":                     testProduceConsumeStream,
		"consume past log boundary fails":                     testConsumePastBoundary,
	} {
		t.Run(scenario, func(t *testing.T) {
			fmt.Println("Running test: ", scenario)
			client, teardown := setupTest(t)
			defer teardown()
			fn(t, client)
		})
	}
}

func setupTest(t *testing.T) (
	client logv1connect.LogAPIClient,
	teardown func(),
) {
	t.Helper()

	l, err := net.Listen("tcp", ":0")
	require.NoError(t, err)

	dir, err := os.MkdirTemp("", "server-test")
	require.NoError(t, err)

	clog := log.NewLog(dir, log.Config{})
	cfg := &Config{
		CommitLog: clog,
	}

	r := http.NewServeMux()
	path, handler := NewLogAPIHandler(cfg)
	r.Handle(path, handler)
	require.NoError(t, err)
	srv := http.Server{
		Handler: h2c.NewHandler(r, &http2.Server{}),
	}
	go func() {
		if err := srv.Serve(l); err != http.ErrServerClosed {
			panic(err)
		}
	}()

	client = logv1connect.NewLogAPIClient(
		internalhttp.DefaultClient,
		"http://"+l.Addr().String(),
		connect.WithGRPC(),
	)

	return client, func() {
		_ = srv.Shutdown(context.Background())
		l.Close()
		clog.Remove()
	}
}

func testProduceConsume(t *testing.T, client logv1connect.LogAPIClient) {
	ctx := context.Background()

	want := &logv1.Record{
		Value: []byte("hello world"),
	}

	produce, err := client.Produce(
		ctx,
		&connect.Request[logv1.ProduceRequest]{
			Msg: &logv1.ProduceRequest{
				Record: want,
			},
		},
	)
	require.NoError(t, err)

	consume, err := client.Consume(ctx, &connect.Request[logv1.ConsumeRequest]{
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
	client logv1connect.LogAPIClient,
) {
	ctx := context.Background()

	produce, err := client.Produce(ctx, &connect.Request[logv1.ProduceRequest]{
		Msg: &logv1.ProduceRequest{
			Record: &logv1.Record{
				Value: []byte("hello world"),
			},
		},
	})
	require.NoError(t, err)

	consume, err := client.Consume(ctx, &connect.Request[logv1.ConsumeRequest]{
		Msg: &logv1.ConsumeRequest{
			Offset: produce.Msg.Offset + 1,
		},
	})
	if consume != nil {
		t.Fatal("consume not nil")
	}
	got := connect.CodeOf(err)
	want := connect.CodeOf(log.WrapToConnectError(log.ErrOffsetOutOfRange{}))
	if got != want {
		t.Fatalf("got err: %v, want: %v", got, want)
	}
}

func testProduceConsumeStream(
	t *testing.T,
	client logv1connect.LogAPIClient,
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
		fmt.Println("ProduceStream")
		stream := client.ProduceStream(ctx)

		for offset, record := range records {
			err := stream.Send(&logv1.ProduceStreamRequest{
				Record: record,
			})
			require.NoError(t, err)
			res, err := stream.Receive()
			require.NoError(t, err)
			if res.Offset != uint64(offset) {
				t.Fatalf(
					"got offset: %d, want: %d",
					res.Offset,
					offset,
				)
			}
		}
	}

	{
		fmt.Println("ConsumeStream")
		stream, err := client.ConsumeStream(
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
