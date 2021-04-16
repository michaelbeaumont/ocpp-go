package ws

import "time"

// RawWebSocket encapsulates the gorilla/websocket Conn.
type RawWebSocket interface {
	Close() error
	SetReadDeadline(time.Time) error
	SetWriteDeadline(time.Time) error
	ReadMessage() (int, []byte, error)
	WriteMessage(int, []byte) error
}

// ByteResult holds exactly one of either Bytes or Err.
type ByteResult struct {
	Bytes []byte
	Err   error
}

// ReadOrDrop holds a channel along with a channel to signal being done.
type ReadOrDrop struct {
	Read <-chan ByteResult
	Drop chan<- struct{}
}

// WriteOrDropped holds a channel along with a signal whether it's not being
// used.
type WriteOrDropped struct {
	Write   chan<- []byte
	Dropped <-chan struct{}
}

// Server represents a websocket server that can be driven to produce a stream
// of websocket connections.
type Server interface {
	// Start drives the websocket server and returns both a channel of
	// connections and a channel for stopping the server.
	Start(port int, path string) (<-chan PendingWebSocketResult, chan<- struct{})
}
