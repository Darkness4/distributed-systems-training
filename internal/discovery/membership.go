package discovery

import (
	"log/slog"
	"net"
	"strconv"

	"github.com/hashicorp/serf/serf"
)

type Config struct {
	NodeName           string
	BindAddress        string
	Tags               map[string]string
	StartJoinAddresses []string
}

// Handler represents an object that handles membership events.
type Handler interface {
	Join(name, addr string) error
	Leave(name string) error
}

type Membership struct {
	Config
	handler Handler
	serf    *serf.Serf
	events  chan serf.Event
	logger  *slog.Logger
}

func New(handler Handler, config Config) (*Membership, error) {
	c := &Membership{
		Config:  config,
		handler: handler,
		logger:  slog.With("component", "membership"),
	}
	return c, c.setupSerf()
}

func (m *Membership) setupSerf() error {
	addr, err := net.ResolveTCPAddr("tcp", m.BindAddress)
	if err != nil {
		return err
	}
	config := serf.DefaultConfig()
	config.Init()
	config.MemberlistConfig.BindAddr = addr.IP.String()
	config.MemberlistConfig.BindPort = addr.Port
	m.events = make(chan serf.Event, 256)
	config.EventCh = m.events
	config.Tags = m.Tags
	config.NodeName = m.NodeName
	m.serf, err = serf.Create(config)
	if err != nil {
		return err
	}
	// TODO: check lifecycle
	go m.eventHandler()
	if m.StartJoinAddresses != nil {
		_, err = m.serf.Join(m.StartJoinAddresses, true)
		if err != nil {
			return err
		}
	}
	return nil
}

func (m *Membership) eventHandler() {
	for e := range m.events {
		switch e.EventType() {
		case serf.EventMemberJoin:
			for _, member := range e.(serf.MemberEvent).Members {
				if m.isLocal(member) {
					continue
				}
				m.handleJoin(member)
			}
		case serf.EventMemberLeave, serf.EventMemberFailed:
			for _, member := range e.(serf.MemberEvent).Members {
				if m.isLocal(member) {
					continue
				}
				m.handleLeave(member)
			}
		}
	}
}

func (m *Membership) isLocal(member serf.Member) bool {
	return member.Name == m.serf.LocalMember().Name
}

func (m *Membership) handleJoin(member serf.Member) {
	if err := m.handler.Join(member.Name, net.JoinHostPort(member.Addr.String(), strconv.FormatUint(uint64(member.Port), 10))); err != nil {
		m.logger.Error("failed to handle join", "error", err, "member", member)
	}
}

func (m *Membership) handleLeave(member serf.Member) {
	if err := m.handler.Leave(member.Name); err != nil {
		m.logger.Error("failed to handle leave", "error", err, "member", member)
	}
}

func (m *Membership) Members() []serf.Member {
	return m.serf.Members()
}

func (m *Membership) Leave() error {
	return m.serf.Leave()
}
