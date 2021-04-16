// The package is a wrapper around gorilla websockets,
// aimed at simplifying the creation and usage of a websocket client/server.
//
// Check the Client and Server structure to get started.
package ws

import (
	"time"

	"github.com/gorilla/websocket"
)

// WebSocket offers both read and write channels for sending and receiving
// messages as well as channel to notify and monitor for closure.
type WebSocket struct {
	Read  ReadOrDrop
	Write WriteOrDropped
}

// PendingWebSocket is a websocket connection that can be
// transformed into a WebSocket.
type PendingWebSocket struct {
	connection RawWebSocket
	id         string
	// TODO what to do with this?
	pingMessage chan []byte
}

// NewPending creates a PendingWebSocket from a raw websocket.
func NewPending(connection RawWebSocket, id string) PendingWebSocket {
	return PendingWebSocket{
		connection: connection,
		id:         id,
	}
}

// GetID returns the final component of the path the client connected to.
func (ws *PendingWebSocket) GetID() string {
	return ws.id
}

// PendingWebSocketResult holds exactly one of a WebSocket or an error.
type PendingWebSocketResult struct {
	Ws  PendingWebSocket
	Err error
}

func (ws *PendingWebSocket) streamReadFromRemote(
	timeout time.Duration,
	toLocal WriteOrDropped,
	webSocketClose chan<- error,
) {
	var retErr error
	defer func() {
		closeErr := ws.connection.Close()
		if retErr == nil {
			retErr = closeErr
		}

		select {
		case webSocketClose <- retErr:
		default:
		}

		// We are the sole writer of readMessages
		close(toLocal.Write)
	}()

	for {
		// This error will be returned by ReadMessage
		_ = ws.connection.SetReadDeadline(time.Now().Add(timeout))

		// This error is permanent
		_, message, err := ws.connection.ReadMessage()
		if err != nil {
			retErr = err

			return
		}

		select {
		case <-toLocal.Dropped:
			return
		case toLocal.Write <- message:
		}
	}
}

func (ws *PendingWebSocket) streamWriteToRemote(
	timeout time.Duration,
	toRemoteBytes <-chan []byte,
	toRemoteClose chan<- struct{},
	webSocketClose chan<- error,
) {
	var retErr error
	defer func() {
		closeErr := ws.connection.Close()
		if retErr == nil {
			retErr = closeErr
		}
		select {
		case webSocketClose <- retErr:
		default:
		}

		close(toRemoteClose)
	}()

	for data := range toRemoteBytes {
		_ = ws.connection.SetWriteDeadline(time.Now().Add(timeout))

		if err := ws.connection.WriteMessage(websocket.TextMessage, data); err != nil {
			retErr = err

			return
		}
	}
	// Closing connection
	if err := ws.connection.WriteMessage(
		websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
	); err != nil {
		retErr = err
	}
}

// Start drives the websocket connection and returns a struct
// that provides channels for interacting with it.
func (ws *PendingWebSocket) Start(readTimeout, writeTimeout time.Duration) WebSocket {
	webSocketClose := make(chan error, 1)

	fromRemoteBytes := make(chan []byte)
	doneWithFromRemote := make(chan struct{})
	go ws.streamReadFromRemote(
		readTimeout,
		WriteOrDropped{
			Write:   fromRemoteBytes,
			Dropped: doneWithFromRemote,
		},
		webSocketClose,
	)

	toRemoteBytes := make(chan []byte)
	doneWithToRemote := make(chan struct{})
	go ws.streamWriteToRemote(writeTimeout, toRemoteBytes, doneWithToRemote, webSocketClose)

	// This takes our websocket error and puts it at the end of our byte stream
	fromRemoteByteResults := make(chan ByteResult)
	go func() {
		defer close(fromRemoteByteResults)
		for bytes := range fromRemoteBytes {
			fromRemoteByteResults <- ByteResult{Bytes: bytes}
		}

		err := <-webSocketClose
		if err != nil {
			fromRemoteByteResults <- ByteResult{Err: err}
		}
	}()

	return WebSocket{
		Read: ReadOrDrop{
			Read: fromRemoteByteResults,
			Drop: doneWithFromRemote,
		},
		Write: WriteOrDropped{
			Write:   toRemoteBytes,
			Dropped: doneWithToRemote,
		},
	}
}
