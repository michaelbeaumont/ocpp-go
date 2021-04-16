package ocppj

import (
	"fmt"
	"time"

	protocol "github.com/lorenzodonini/ocpp-go/ocppj/ng/protocol"
	ws "github.com/lorenzodonini/ocpp-go/ws/ng"
)

// PendingConnection represents a websocket connection that can be transformed
// into an OCPP-J Connection.
type PendingConnection struct {
	endpoint *protocol.Endpoint
	ws       *ws.PendingWebSocket
}

// NewPendingConnection returns a new pending connection from a pending
// WebSocket.
func NewPendingConnection(endpoint *protocol.Endpoint, ws *ws.PendingWebSocket) PendingConnection {
	return PendingConnection{
		endpoint,
		ws,
	}
}

// PendingConnectionResult is either a PendingConnection or an underlying
// transport error.
type PendingConnectionResult struct {
	Connection PendingConnection
	Err        error
}

// Connection offers both read and write channels for sending and receiving
// messages as well as a channel to monitor for Write closure.
type Connection struct {
	endpoint *protocol.Endpoint
	clientID string
	read     ReadOrDrop
	write    WriteOrDropped
}

// ClientID returns the ID for this OCPP-J connection, i.e. the final path
// component.
func (conn *Connection) ClientID() string {
	return conn.clientID
}

// Read produces requests from the remote. Remotes SHOULD NOT send more than one
// requests before receiving a response but if they do, this will produce them.
// However, they must be responded to in the order they were received, otherwise
// progress will not be made.
func (conn *Connection) Read() ReadOrDrop {
	return conn.read
}

// Write consumes local OCPPJ messages. Producers can be notified if the
// channel is no longer being processed by checking Close().
// Producers can also close the channel to notify the consumer that no new
// requests will be sent.
func (conn *Connection) Write() WriteOrDropped {
	return conn.write
}

// outstandingLocalRequest holds what we need to track local requests waiting
// for a response (or error).
type outstandingLocalRequest struct {
	result      chan<- ResponseResult
	id          string
	featureName string
}

// outstandingRemoteRequest holds what we need to track requests initiated by
// the remote.
type outstandingRemoteRequest struct {
	result <-chan ResponseResult
	id     string
}

// Start drives the OCPPJ connection and must only be called once.
func (pending *PendingConnection) Start(
	bufferSize int,
	readTimeout,
	writeTimeout time.Duration,
) Connection {
	remoteRequests := make(chan RequestResultPair, bufferSize)
	remoteRequestsDone := make(chan struct{})
	localRequests := make(chan RequestResultPair, bufferSize)
	localRequestsDone := make(chan struct{})

	webSocket := pending.ws.Start(readTimeout, writeTimeout)

	writeWebSocket := newWait(
		webSocket.Write,
	)

	// State of last received request
	// We only allow one outstanding local request
	outstandingLocalRequests := make(chan outstandingLocalRequest, 1)
	outstandingLocalRequests <- outstandingLocalRequest{}

	waitingOnRemoteResponse := make(chan struct{}, 1)

	writeWebSocket.Go()
	go func() {
		defer writeWebSocket.Done()
		handleLocalRequests(
			writeWebSocket.Channel(),
			ReadOrDrop{
				Read: localRequests,
				Drop: localRequestsDone,
			},
			waitingOnRemoteResponse,
			outstandingLocalRequests,
		)
	}()

	writeWebSocket.Go()
	go func() {
		defer writeWebSocket.Done()
		handleRemoteMessages(
			pending.endpoint,
			webSocket.Read,
			writeWebSocket,
			WriteOrDropped{
				Write:   remoteRequests,
				Dropped: remoteRequestsDone,
			},
			waitingOnRemoteResponse,
			outstandingLocalRequests,
		)
	}()

	writeWebSocket.GoCloseWhenDone()

	return Connection{
		clientID: pending.ws.GetID(),
		read: ReadOrDrop{
			Read: remoteRequests,
			Drop: remoteRequestsDone,
		},
		write: WriteOrDropped{
			Write:   localRequests,
			Dropped: localRequestsDone,
		},
	}

}

// handleLocalRequests drives the sending of local requests
// to the Write channel.
func handleLocalRequests(
	writeWebSocket ws.WriteOrDropped,
	localRequests ReadOrDrop,
	waitingOnRemoteResponse chan struct{},
	outstandingLocalRequests chan outstandingLocalRequest,
) {
	defer func() {
		close(waitingOnRemoteResponse)
		close(localRequests.Drop)
	}()

	// Each pair contains a oneshot channel.
	for pair := range localRequests.Read {
		featureName := pair.req.GetFeatureName()

		uniqueID, jsonMessage, err := protocol.SendRequest(pair.req)
		if err != nil {
			panic(fmt.Sprintf("internal inconsistency with validated request to send to remote: %s", err))
		}

		// Start waiting for remote response
		waitingOnRemoteResponse <- struct{}{}

		<-outstandingLocalRequests

		outstandingLocalRequests <- outstandingLocalRequest{
			result:      pair.resp,
			id:          uniqueID,
			featureName: featureName,
		}

		select {
		case <-writeWebSocket.Dropped:
			<-waitingOnRemoteResponse
			return
		case writeWebSocket.Write <- jsonMessage:
		}

		time.AfterFunc(60*time.Second, func() {
			// Drop request if it's the request we're expecting
			// if not well it's been answered!
			req := <-outstandingLocalRequests

			if req.id == uniqueID {
				req.result <- NewResponseError(fmt.Errorf("timeout"))
				close(req.result)

				outstandingLocalRequests <- outstandingLocalRequest{}

				<-waitingOnRemoteResponse
			} else {
				outstandingLocalRequests <- req
			}
		})
	}
}

// handleLocalResponse drives the sending of local responses
// for received requests.
// Every response is sent from local on its own channel, provided when the
// request if received.
// The outstanding requests are buffered in order to allow multiple outstanding
// requests to exist but this goroutine only makes progress if the responses
// are sent in the order they were received.
func handleLocalResponse(
	write ws.WriteOrDropped,
	outstandingRemoteRequest outstandingRemoteRequest,
) {
	// Wait on the response from local
	// TODO timeout?
	result := <-outstandingRemoteRequest.result

	var jsonMessage []byte
	var err error
	// The user can only set err or resp, guaranteed by the public
	// constructors for ResponseResult
	if result.err != nil {
		jsonMessage, err = protocol.SendError(outstandingRemoteRequest.id, result.err.Code, result.err.Description, result.err.Details)
		if err != nil {
			panic(fmt.Sprintf("internal inconsistency creating errors to send to remote: %s", err))
		}
	} else {
		jsonMessage, err = protocol.SendResponse(outstandingRemoteRequest.id, result.resp)
		if err != nil {
			panic(fmt.Sprintf("internal inconsistency with validated response to send to remote: %s", err))
		}
	}

	select {
	case <-write.Dropped:
		return
	case write.Write <- jsonMessage:
	}
}

// handleRemoteMessages drives reading the websocket connection.
func handleRemoteMessages(
	endpoint *protocol.Endpoint,
	readWebSocket ws.ReadOrDrop,
	writeWait wait,
	remoteRequests WriteOrDropped,
	waitingOnRemoteResponse chan struct{},
	outstandingLocalRequests chan outstandingLocalRequest,
) {
	defer func() {
		// We are the only sender for these channels
		close(remoteRequests.Write)
		close(readWebSocket.Drop)
	}()

	handleError := func(messageID string, err *protocol.Error) bool {
		jsonErrorMessage, sendErr := protocol.SendError(messageID, err.Code, err.Description, nil)
		if sendErr != nil {
			panic(fmt.Sprintf("internal inconsistency creating errors to send to remote: %s", sendErr))
		}

		var done bool

		select {
		case <-remoteRequests.Dropped:
			done = true
		case remoteRequests.Write <- RequestResultPair{err: err}:
		}

		write := writeWait.Channel()

		select {
		case <-write.Dropped:
			done = true
		case write.Write <- jsonErrorMessage:
		}

		return done
	}

	for messageResult := range readWebSocket.Read {
		if messageResult.Err != nil {
			remoteRequests.Write <- RequestResultPair{err: messageResult.Err}
			continue
		}

		message := messageResult.Bytes

		parsedJSON, parseErr := protocol.ParseRawJsonMessage(message)
		if parseErr != nil {
			// We don't even have a messageID to give here
			if ret := handleError("", &protocol.Error{Code: protocol.ProtocolError, Description: "cannot parse as message"}); ret {
				return
			}
			continue
		}

		messageType, uniqueID, err := protocol.ParseMessageType(parsedJSON)
		if err != nil {
			if ret := handleError(uniqueID, err); ret {
				return
			}
			continue
		}

		switch messageType {
		case protocol.CALL:
			message, err := endpoint.ParseOcppRequest(parsedJSON)
			if err != nil {
				if ret := handleError(uniqueID, err); ret {
					return
				}
				continue
			}

			respChan := make(chan ResponseResult, 1)

			outstandingRemoteRequest := outstandingRemoteRequest{
				result: respChan,
				id:     uniqueID,
			}

			writeWait.Go()
			go func() {
				defer writeWait.Done()
				handleLocalResponse(writeWait.Channel(), outstandingRemoteRequest)
			}()

			select {
			case <-remoteRequests.Dropped:
				return
			case remoteRequests.Write <- RequestResultPair{req: message, resp: respChan}:
			default:
				if ret := handleError(uniqueID, &protocol.Error{Code: protocol.GenericError, Description: "too many concurrent calls"}); ret {
					return
				}
			}

		case protocol.CALL_RESULT, protocol.CALL_ERROR:
			fmt.Println(messageType, uniqueID)
			outstandingReq := <-outstandingLocalRequests

			if outstandingReq.id == "" || outstandingReq.id != uniqueID {
				// This means either:
				//  * we've got an internal inconsistency
				//  * or the the peer sent a response but
				//    there are no local, sent requests waiting for a response.
				//    May be due to a timeout.
				outstandingLocalRequests <- outstandingReq
				continue
			}

			// Allow local to send messages again
			outstandingLocalRequests <- outstandingLocalRequest{}
			<-waitingOnRemoteResponse

			switch messageType {
			case protocol.CALL_RESULT:
				response, err := endpoint.ParseOcppResponse(outstandingReq.featureName, parsedJSON)
				if err != nil {
					if ret := handleError(uniqueID, err); ret {
						return
					}
					continue
				}

				// We don't care if this channel is ever read
				outstandingReq.result <- ResponseResult{resp: response}
				close(outstandingReq.result)
			case protocol.CALL_ERROR:
				ocppError, err := endpoint.ParseOcppError(parsedJSON)
				if err != nil {
					if ret := handleError(uniqueID, err); ret {
						return
					}
					continue
				}

				// We don't care if this channel is ever read
				outstandingReq.result <- ResponseResult{err: ocppError}
				close(outstandingReq.result)
			default:
				panic("unreachable")
			}

		}
	}
}
