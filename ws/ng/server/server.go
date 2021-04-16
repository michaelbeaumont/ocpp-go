// The package is a wrapper around gorilla websockets,
// aimed at simplifying the creation and usage of a websocket client/server.
//
// Check the Client and Server structure to get started.
package ws

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"

	ws "github.com/lorenzodonini/ocpp-go/ws/ng"
)

const (
	// Time allowed to write a message to the peer.
	defaultWriteWait = 10 * time.Second
	// Time allowed to read the next pong message from the peer.
	defaultPongWait = 60 * time.Second
	// Time allowed to wait for a ping on the server, before closing a connection due to inactivity.
	defaultPingWait = defaultPongWait
)

// TimeoutConfig contains optional configuration parameters for a websocket server.
// Setting the parameter allows to define custom timeout intervals for websocket network operations.
//
// To set a custom configuration, refer to the server's SetTimeoutConfig method.
// If no configuration is passed, a default configuration is generated via the NewServerTimeoutConfig function.
type TimeoutConfig struct {
	WriteWait time.Duration
	PingWait  time.Duration
}

// NewServerTimeoutConfig creates the default timeout settings for the WebSocket
// server.
func NewTimeoutConfig() TimeoutConfig {
	return TimeoutConfig{WriteWait: defaultWriteWait, PingWait: defaultPingWait}
}

// Builder is used to build a Server from options.
type Builder struct {
	opts []func(*srv)
}

// Build uses the options to create a Server.
func (b *Builder) Build() ws.Server {
	s := &srv{
		upgrader: websocket.Upgrader{Subprotocols: []string{}},
	}

	for _, opt := range b.opts {
		opt(s)
	}

	return s
}

// WithListenAddress sets the address to listen on for connections.
func (b *Builder) WithListenAddress(addr string) *Builder {
	b.opts = append(
		b.opts,
		func(s *srv) { s.listenAddr = addr },
	)
	return b
}

// WithBasicAuthHandler sets the function to be used to handle HTTP Basic Auth.
func (b *Builder) WithBasicAuthHandler(handler func(username string, password string) bool) *Builder {
	b.opts = append(
		b.opts,
		func(s *srv) { s.basicAuthHandler = handler },
	)
	return b
}

// WithCheckOriginHandler sets the handler to check the origin of the request.
func (b *Builder) WithCheckOriginHandler(handler func(r *http.Request) bool) *Builder {
	b.opts = append(
		b.opts,
		func(s *srv) { s.upgrader.CheckOrigin = handler },
	)
	return b
}

// WithSupportedSubprotocol adds a protocol to be allowed in the upgrade step.
func (b *Builder) WithSupportedSubprotocol(subProto string) *Builder {
	b.opts = append(
		b.opts,
		func(s *srv) {
			// Don't add duplicates
			for _, sub := range s.upgrader.Subprotocols {
				if sub == subProto {
					return
				}
			}

			s.upgrader.Subprotocols = append(s.upgrader.Subprotocols, subProto)
		},
	)
	return b
}

// WithTLS sets up the server to use TLS.
func (b *Builder) WithTLS(certificatePath string, certificateKey string, tlsConfig *tls.Config) *Builder {
	b.opts = append(
		b.opts,
		func(s *srv) {
			s.tlsCertificatePath = certificatePath
			s.tlsCertificateKey = certificateKey
			s.httpServer = &http.Server{
				TLSConfig: tlsConfig,
			}
		},
	)
	return b
}

// srv is a WebSocket server that can be driven as a stream of incoming
// WebSocket connections.
type srv struct {
	listenAddr         string
	httpServer         *http.Server
	basicAuthHandler   func(username string, password string) bool
	tlsCertificatePath string
	tlsCertificateKey  string
	upgrader           websocket.Upgrader
	errC               chan error
	readTimeout        time.Duration
	writeTimeout       time.Duration
}

// upgradeToWebSocket takes an HTTP request and tries to upgrad the connection to a
// WebSocket.
func (server *srv) upgradeToWebSocket(w http.ResponseWriter, r *http.Request) ws.PendingWebSocketResult {
	vars := mux.Vars(r)

	id, ok := vars["id"]
	if !ok {
		if id, ok = vars["ws"]; !ok {
			return ws.PendingWebSocketResult{
				Err: fmt.Errorf("internal error: couldn't get id or ws var from URL"),
			}
		}
	}

	// Check if requested subprotocol is supported
	clientSubprotocols := websocket.Subprotocols(r)
	supported := false
	if len(server.upgrader.Subprotocols) == 0 {
		// All subProtocols are accepted
		supported = true
	}
	for _, supportedProto := range server.upgrader.Subprotocols {
		for _, requestedProto := range clientSubprotocols {
			if requestedProto == supportedProto {
				supported = true
				break
			}
		}
	}
	if !supported {
		http.Error(w, "unsupported subprotocol", http.StatusBadRequest)
		return ws.PendingWebSocketResult{Err: fmt.Errorf("unsupported subprotocol: %v", clientSubprotocols)}
	}
	// Handle client authentication
	if server.basicAuthHandler != nil {
		username, password, ok := r.BasicAuth()
		if !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return ws.PendingWebSocketResult{Err: fmt.Errorf("basic auth failed: credentials not found")}
		}
		ok = server.basicAuthHandler(username, password)
		if !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return ws.PendingWebSocketResult{Err: fmt.Errorf("basic auth failed: credentials invalid")}
		}
	}
	// Upgrade websocket
	conn, err := server.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return ws.PendingWebSocketResult{Err: fmt.Errorf("upgrade failed: %w", err)}
	}

	pending := ws.NewPending(
		conn,
		id,
	)

	// TODO do on connection!
	conn.SetPingHandler(func(message string) error {
		err := conn.SetReadDeadline(time.Now().Add(server.readTimeout))
		if err != nil {
			return err
		}
		err = conn.WriteControl(websocket.PongMessage, []byte(message), time.Now().Add(server.writeTimeout))
		if err == websocket.ErrCloseSent {
			return nil
		} else if e, ok := err.(net.Error); ok && e.Temporary() {
			return nil
		}
		return err
	})

	return ws.PendingWebSocketResult{Ws: pending}
}

// Start drives the server and returns a channel of websockets.
func (server *srv) Start(port int, listenPath string) (<-chan ws.PendingWebSocketResult, chan<- struct{}) {
	wss := make(chan ws.PendingWebSocketResult)
	stop := make(chan struct{})

	router := mux.NewRouter()
	router.HandleFunc(listenPath, func(w http.ResponseWriter, r *http.Request) {
		wss <- server.upgradeToWebSocket(w, r)
	})

	if server.httpServer == nil {
		server.httpServer = &http.Server{}
	}

	shutdownServer := func() {
		<-stop
		if err := server.httpServer.Shutdown(context.TODO()); err != nil {
			wss <- ws.PendingWebSocketResult{Err: err}
		}
	}
	go shutdownServer()

	startServer := func() {
		defer close(wss)

		addr := fmt.Sprintf("%s:%v", server.listenAddr, port)
		server.httpServer.Addr = addr
		server.httpServer.Handler = router

		if server.tlsCertificatePath != "" && server.tlsCertificateKey != "" {
			if err := server.httpServer.ListenAndServeTLS(
				server.tlsCertificatePath, server.tlsCertificateKey,
			); err != http.ErrServerClosed {
				wss <- ws.PendingWebSocketResult{
					Err: err,
				}
			}
		} else {
			if err := server.httpServer.ListenAndServe(); err != http.ErrServerClosed {
				wss <- ws.PendingWebSocketResult{
					Err: err,
				}
			}
		}
	}
	go startServer()

	return wss, stop
}
