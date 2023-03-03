package kit

import (
	"context"
	"fmt"

	"github.com/libp2p/go-libp2p/core/host"

	"github.com/filecoin-project/mir/pkg/events"
	"github.com/filecoin-project/mir/pkg/logging"
	"github.com/filecoin-project/mir/pkg/net"
	"github.com/filecoin-project/mir/pkg/net/libp2p"
	"github.com/filecoin-project/mir/pkg/pb/eventpb"
	"github.com/filecoin-project/mir/pkg/pb/messagepb"
	t "github.com/filecoin-project/mir/pkg/types"
)

var _ net.Transport = &MockedTransport{}

func NewTransport(params libp2p.Params, ownID t.NodeID, h host.Host, logger logging.Logger) *MockedTransport {
	tr := libp2p.NewTransport(params, ownID, h, logger)

	return &MockedTransport{
		transport:      tr,
		logger:         logger,
		h:              h,
		transportChan:  tr.EventsOut(),
		controlledChan: make(chan *events.EventList),
		stop:           make(chan struct{}),
	}
}

type MockedTransport struct {
	stop           chan struct{}
	h              host.Host
	transport      *libp2p.Transport
	logger         logging.Logger
	transportChan  <-chan *events.EventList
	controlledChan chan *events.EventList
	disconnected   bool
}

func (m *MockedTransport) Start() error {
	return m.transport.Start()
}

func (m *MockedTransport) Disable() {
	m.h.RemoveStreamHandler("/mir/0.0.1")
	conns := m.h.Network().Conns()
	for _, c := range conns {
		_ = c.Close() // nolint
	}
	m.disconnected = true
}

// Enable enables the transport after calling Disable.
func (m *MockedTransport) Enable() {
	m.disconnected = false
	err := m.Start()
	if err != nil {
		panic(err)
	}
}

func (m *MockedTransport) Stop() {
	m.transport.Stop()
	close(m.stop)
}

func (m *MockedTransport) Send(dest t.NodeID, msg *messagepb.Message) error {
	if m.disconnected {
		return nil // fmt.Errorf("no connection")
	}
	return m.transport.Send(dest, msg)
}

func (m *MockedTransport) Connect(nodes map[t.NodeID]t.NodeAddress) {
	m.transport.Connect(nodes)
}

func (m *MockedTransport) WaitFor(n int) {
	m.transport.WaitFor(n)
}

// CloseOldConnections closes connections to the nodes that don't needed.
func (m *MockedTransport) CloseOldConnections(newNodes map[t.NodeID]t.NodeAddress) {
	m.transport.CloseOldConnections(newNodes)
}

func (m *MockedTransport) ImplementsModule() {}

func (m *MockedTransport) ApplyEvents(ctx context.Context, eventList *events.EventList) error {
	iter := eventList.Iterator()
	for event := iter.Next(); event != nil; event = iter.Next() {

		switch e := event.Type.(type) {
		case *eventpb.Event_Init:
			// no actions on init
		case *eventpb.Event_SendMessage:
			for _, destID := range e.SendMessage.Destinations {
				if err := m.Send(t.NodeID(destID), e.SendMessage.Msg); err != nil {
					m.logger.Log(logging.LevelWarn, "Failed to send a message", "dest", destID, "err", err)
				}
			}
		default:
			return fmt.Errorf("unexpected event: %T", event.Type)
		}
	}
	return nil
}

func (m *MockedTransport) EventsOut() <-chan *events.EventList {
	go func() {
		for {
			select {
			case <-m.stop:
				return
			case msg := <-m.transportChan:
				if !m.disconnected {
					m.controlledChan <- msg
				}
			}
		}

	}()

	return m.controlledChan
}
