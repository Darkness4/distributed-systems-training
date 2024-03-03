package discovery_test

import (
	"distributed-systems/internal/discovery"
	"distributed-systems/internal/net"
	"fmt"
	"testing"
	"time"

	"github.com/hashicorp/serf/serf"
	"github.com/stretchr/testify/require"
)

func TestMembership(t *testing.T) {
	m, handler := setupMembership(t, nil)
	m, _ = setupMembership(t, m)
	m, _ = setupMembership(t, m)

	require.Eventually(t, func() bool {
		return len(handler.joins) == 2 && len(m[0].Members()) == 3 && len(handler.leaves) == 0
	}, 3*time.Second, 250*time.Millisecond)

	err := m[2].Leave()
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return len(handler.joins) == 2 && len(m[0].Members()) == 3 && len(handler.leaves) == 1 &&
			m[0].Members()[2].Status == serf.StatusLeft
	}, 3*time.Second, 250*time.Millisecond)

	require.Equal(t, fmt.Sprintf("%d", 2), <-handler.leaves)
}

func setupMembership(
	t *testing.T,
	members []*discovery.Membership,
) ([]*discovery.Membership, *handler) {
	id := len(members)
	port, err := net.GetAvailablePort()
	require.NoError(t, err)
	addr := fmt.Sprintf("%s:%d", "127.0.0.1", port)
	c := discovery.Config{
		NodeName:    fmt.Sprintf("%d", id),
		BindAddress: addr,
		Tags:        map[string]string{},
	}
	h := &handler{}
	if len(members) == 0 {
		h.joins = make(chan map[string]string, 3)
		h.leaves = make(chan string, 3)
	} else {
		c.StartJoinAddresses = []string{
			members[0].BindAddress,
		}
	}
	m, err := discovery.New(h, c)
	require.NoError(t, err)
	members = append(members, m)
	return members, h
}

type handler struct {
	joins  chan map[string]string
	leaves chan string
}

func (h *handler) Join(id, addr string) error {
	if h.joins != nil {
		h.joins <- map[string]string{
			"id":   id,
			"addr": addr,
		}
	}
	return nil
}

func (h *handler) Leave(id string) error {
	if h.leaves != nil {
		h.leaves <- id
	}
	return nil
}
