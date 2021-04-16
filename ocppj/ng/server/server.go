package server

import (
	"github.com/lorenzodonini/ocpp-go/ocpp"
	ocppj "github.com/lorenzodonini/ocpp-go/ocppj/ng"
	"github.com/lorenzodonini/ocpp-go/ocppj/ng/protocol"
	ws "github.com/lorenzodonini/ocpp-go/ws/ng"
)

// Server represents an OCPP-J server that can be Started.
type Server struct {
	protocol.Endpoint
	server ws.Server
}

// New creates a new OCPPJ server.
func New(server ws.Server, profiles ...*ocpp.Profile) *Server {
	endpoint := protocol.Endpoint{}

	for _, profile := range profiles {
		endpoint.AddProfile(profile)
	}

	if server == nil {
		panic("ocppj.NewServer: websocket server must not be null!")
	}

	return &Server{Endpoint: endpoint, server: server}
}

// NewPendingConnection can be used to convert a pending websocket connection
// into an OCPP-J one.
func (s *Server) NewPendingConnection(webSocket ws.PendingWebSocket) ocppj.PendingConnection {
	return ocppj.NewPendingConnection(
		&s.Endpoint,
		&webSocket,
	)
}

// Start drives the underlying Websocket server on a specified listenPort and listenPath
// and returns a channel for handling new connections as well as stopping the
// server. It can be used to start an OCPP-J protocol server but
// NewPendingConnection can be used instead inside of overlying protocol.
func (s *Server) Start(
	port int,
	path string,
) (<-chan ocppj.PendingConnectionResult, chan<- struct{}) {
	conns := make(chan ocppj.PendingConnectionResult)
	wss, stop := s.server.Start(port, path)

	go func() {
		defer close(conns)

		for ws := range wss {
			if ws.Err != nil {
				conns <- ocppj.PendingConnectionResult{
					Err: ws.Err,
				}

				continue
			}
			conns <- ocppj.PendingConnectionResult{
				Connection: s.NewPendingConnection(ws.Ws),
			}
		}
	}()

	return conns, stop
}
