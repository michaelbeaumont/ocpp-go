package ocppj

import (
	"fmt"

	"github.com/lorenzodonini/ocpp-go/ocpp"
	"github.com/lorenzodonini/ocpp-go/ocppj"
	protocol "github.com/lorenzodonini/ocpp-go/ocppj/ng/protocol"
)

// ResponseResult can only be accessed using the methods provided and it's
// guaranteed that exactly one of Response or Err will be non-nil.
type ResponseResult struct {
	err     *protocol.Error
	resp    ocpp.Response
	respErr chan *protocol.Error
}

// Response returns the OCPP response, if any.
func (result *ResponseResult) Response() ocpp.Response {
	return result.resp
}

// Err returns the error that occurred, if any.
func (result *ResponseResult) Err() *protocol.Error {
	return result.err
}

// RespErr TODO
func (result *ResponseResult) RespErr() <-chan *protocol.Error {
	return result.respErr
}

// NewResponseResult validates the response and returns a ResponseResult if
// successful.
func NewResponseResult(resp ocpp.Response) (ResponseResult, error) {
	if err := protocol.Validate.Struct(resp); err != nil {
		return ResponseResult{}, fmt.Errorf("invalidate response: %w", err)
	}

	return ResponseResult{
		resp: resp,
	}, nil
}

// NewResponseError return a ResponseResult containing an error. It corresponds
// to an InternalError OCPP-J CALL_ERROR.
func NewResponseError(err error) ResponseResult {
	return NewResponseInternalError(err.Error(), nil)
}

// NewResponseNotImplementedError returns a ResponseResult containing an OCPP error.
func NewResponseNotImplementedError(description string, details interface{}) ResponseResult {
	return ResponseResult{
		err: &protocol.Error{
			Code:        ocppj.NotImplemented,
			Description: description,
			Details:     details,
		},
	}
}

// NewResponseNotSupportedError returns a ResponseResult containing an OCPP error.
func NewResponseNotSupportedError(description string, details interface{}) ResponseResult {
	return ResponseResult{
		err: &protocol.Error{
			Code:        ocppj.NotSupported,
			Description: description,
			Details:     details,
		},
	}
}

// NewResponseSecurityError returns a ResponseResult containing an OCPP error.
func NewResponseSecurityError(description string, details interface{}) ResponseResult {
	return ResponseResult{
		err: &protocol.Error{
			Code:        ocppj.SecurityError,
			Description: description,
			Details:     details,
		},
	}
}

// NewResponseInternalError returns a ResponseResult containing an OCPP error.
func NewResponseInternalError(description string, details interface{}) ResponseResult {
	return ResponseResult{
		err: &protocol.Error{
			Code:        ocppj.InternalError,
			Description: description,
			Details:     details,
		},
	}
}

// RequestResultPair contains one of a request or error as well as a channel over which
// responses can be sent.
type RequestResultPair struct {
	req  ocpp.Request
	err  error
	resp chan<- ResponseResult
}

// NewRequestPair returns a new RequestPair and a channel for getting the
// result.
func NewRequestPair(req ocpp.Request) (RequestResultPair, <-chan ResponseResult, error) {
	if err := protocol.Validate.Struct(req); err != nil {
		return RequestResultPair{}, nil, fmt.Errorf("invalidate request: %w", err)
	}

	resp := make(chan ResponseResult, 1)

	return RequestResultPair{
		req:  req,
		resp: resp,
	}, resp, nil
}

// Request returns the request that was received, if any.
func (pair *RequestResultPair) Request() ocpp.Request {
	return pair.req
}

// Err returns a remote error that occurred while handling requests, if any.
func (pair *RequestResultPair) Err() error {
	return pair.err
}

// Response should be used to return the response to Req, if it's non-nil.
func (pair *RequestResultPair) Response() chan<- ResponseResult {
	return pair.resp
}
