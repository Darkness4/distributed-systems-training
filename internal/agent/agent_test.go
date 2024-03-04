package agent_test

import (
	"context"
	"crypto/tls"
	logv1 "distributed-systems/gen/log/v1"
	"distributed-systems/gen/log/v1/logv1connect"
	"distributed-systems/internal/agent"
	internalhttp "distributed-systems/internal/http"
	"distributed-systems/internal/log"
	"distributed-systems/internal/net"
	"distributed-systems/internal/server"
	"fmt"
	"os"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
)

const (
	peerClientCert = "../../test/certs/root-client/tls.test.crt"
	peerClientKey  = "../../test/certs/root-client/tls.test.key"
	caCert         = "../../test/certs/ca/tls.test.crt"
	serverCert     = "../../test/certs/server/tls.test.crt"
	serverKey      = "../../test/certs/server/tls.test.key"
	serverName     = "localhost"
	aclPolicyFile  = "../../test/acl/policy.csv"
	aclModelFile   = "../../test/acl/model.conf"
)

func TestAgent(t *testing.T) {
	var serverTLSConfig tls.Config
	err := internalhttp.SetupServerTLSConfig(
		serverCert,
		serverKey,
		caCert,
		serverName,
		&serverTLSConfig,
	)
	require.NoError(t, err)
	peerTLSConfig, err := internalhttp.SetupClientTLSConfig(
		peerClientCert,
		peerClientKey,
		caCert,
		serverName,
	)
	require.NoError(t, err)

	var agents []*agent.Agent
	for i := 0; i < 3; i++ {
		port, err := net.GetAvailablePort()
		require.NoError(t, err)
		bindAddr := fmt.Sprintf("%s:%d", "localhost", port)
		rpcPort, err := net.GetAvailablePort()
		require.NoError(t, err)

		dataDir, err := os.MkdirTemp("", "agent-test-log")
		require.NoError(t, err)

		var startJoinAddrs []string
		if i != 0 {
			startJoinAddrs = append(
				startJoinAddrs,
				agents[0].Config.BindAddress,
			)
		}

		agent, err := agent.New(agent.Config{
			NodeName:           fmt.Sprintf("%d", i),
			StartJoinAddresses: startJoinAddrs,
			BindAddress:        bindAddr,
			RPCPort:            rpcPort,
			DataDir:            dataDir,
			ACLModelFile:       aclModelFile,
			ACLPolicyFile:      aclPolicyFile,
			ServerTLSConfig:    &serverTLSConfig,
			PeerTLSConfig:      peerTLSConfig,
			Bootstrap:          i == 0,
		})
		require.NoError(t, err)

		agents = append(agents, agent)
	}
	defer func() {
		for _, agent := range agents {
			err := agent.Shutdown()
			require.NoError(t, err)
			require.NoError(t,
				os.RemoveAll(agent.Config.DataDir),
			)
		}
	}()
	time.Sleep(3 * time.Second)

	leaderClient := client(t, agents[0], peerTLSConfig)
	produceResponse, err := leaderClient.Produce(
		context.Background(),
		&connect.Request[logv1.ProduceRequest]{
			Msg: &logv1.ProduceRequest{
				Record: &logv1.Record{
					Value: []byte("foo"),
				},
			},
		},
	)
	require.NoError(t, err)
	consumeResponse, err := leaderClient.Consume(
		context.Background(),
		&connect.Request[logv1.ConsumeRequest]{
			Msg: &logv1.ConsumeRequest{
				Offset: produceResponse.Msg.Offset,
			},
		},
	)
	require.NoError(t, err)
	require.Equal(t, consumeResponse.Msg.Record.Value, []byte("foo"))

	// wait until replication has finished
	time.Sleep(3 * time.Second)

	followerClient := client(t, agents[1], peerTLSConfig)
	consumeResponse, err = followerClient.Consume(
		context.Background(),
		&connect.Request[logv1.ConsumeRequest]{
			Msg: &logv1.ConsumeRequest{
				Offset: produceResponse.Msg.Offset,
			},
		},
	)
	require.NoError(t, err)
	require.Equal(t, consumeResponse.Msg.Record.Value, []byte("foo"))

	consumeResponse, err = leaderClient.Consume(
		context.Background(),
		&connect.Request[logv1.ConsumeRequest]{
			Msg: &logv1.ConsumeRequest{
				Offset: produceResponse.Msg.Offset + 1,
			},
		},
	)
	require.Nil(t, consumeResponse)
	require.Error(t, err)
	got := connect.CodeOf(err)
	want := connect.CodeOf(server.WrapToConnectError(log.ErrOffsetOutOfRange{}))
	require.Equal(t, want, got)
}

func client(
	t *testing.T,
	agent *agent.Agent,
	tlsConfig *tls.Config,
) logv1connect.LogAPIClient {
	http := internalhttp.NewH2Client(internalhttp.WithTLSConfig(tlsConfig))
	rpcAddr, err := agent.Config.RPCAddress()
	require.NoError(t, err)
	if tlsConfig != nil {
		rpcAddr = "https://" + rpcAddr
	} else {
		rpcAddr = "http://" + rpcAddr
	}
	client := logv1connect.NewLogAPIClient(http, rpcAddr, connect.WithGRPC())
	return client
}
