package distributed_test

import (
	logv1 "distributed-systems/gen/log/v1"
	"distributed-systems/internal/log"
	"distributed-systems/internal/log/distributed"
	internalnet "distributed-systems/internal/net"
	"fmt"
	"net"
	"os"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/raft"
	"github.com/stretchr/testify/require"
)

func TestMultipleNodes(t *testing.T) {
	var logs []*distributed.Log
	nodeCount := 3

	for i := 0; i < nodeCount; i++ {
		port, err := internalnet.GetAvailablePort()
		require.NoError(t, err)
		dataDir, err := os.MkdirTemp("", "distributed-log-test")
		require.NoError(t, err)
		defer func(dir string) {
			_ = os.RemoveAll(dir)
		}(dataDir)
		ln, err := net.Listen(
			"tcp",
			net.JoinHostPort("127.0.0.1", strconv.Itoa(port)),
		)
		require.NoError(t, err)

		config := log.Config{
			Raft: log.Raft{
				StreamLayer: distributed.NewStreamLayer(ln, nil, nil),
				Config: raft.Config{
					LocalID:            raft.ServerID(fmt.Sprintf("%d", i)),
					HeartbeatTimeout:   50 * time.Millisecond,
					ElectionTimeout:    50 * time.Millisecond,
					LeaderLeaseTimeout: 50 * time.Millisecond,
					CommitTimeout:      5 * time.Millisecond,
				},
			},
		}

		if i == 0 {
			config.Raft.Bootstrap = true
		}

		l, err := distributed.NewLog(dataDir, config)
		require.NoError(t, err)

		if i != 0 {
			err = logs[0].Join(
				fmt.Sprintf("%d", i), ln.Addr().String(),
			)
			require.NoError(t, err)
		} else {
			err = l.WaitForLeader(5 * time.Second)
			require.NoError(t, err)
		}

		logs = append(logs, l)
	}

	records := []*logv1.Record{
		{Value: []byte("first")},
		{Value: []byte("second")},
	}
	for _, record := range records {
		off, err := logs[0].Append(record)
		require.NoError(t, err)

		require.Eventually(t, func() bool {
			for j := 0; j < nodeCount; j++ {
				got, err := logs[j].Read(off)
				if err != nil {
					return false
				}
				record.Offset = off
				if !reflect.DeepEqual(got.Value, record.Value) {
					return false
				}
			}
			return true
		}, 500*time.Millisecond, 50*time.Millisecond)
	}

	err := logs[0].Leave("1")
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	off, err := logs[0].Append(&logv1.Record{
		Value: []byte("third"),
	})
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	record, err := logs[1].Read(off)
	require.IsType(t, log.ErrOffsetOutOfRange{}, err)
	require.Nil(t, record)

	record, err = logs[2].Read(off)
	require.NoError(t, err)
	require.Equal(t, []byte("third"), record.Value)
	require.Equal(t, off, record.Offset)
}
