package protocol

import "github.com/lorenzodonini/ocpp-go/ocpp"

type Endpoint struct {
	Profiles []*ocpp.Profile
}

// Adds support for a new profile on the endpoint.
func (endpoint *Endpoint) AddProfile(profile *ocpp.Profile) {
	endpoint.Profiles = append(endpoint.Profiles, profile)
}

// Retrieves a profile for a given feature.
// Returns a false flag in case no profile supporting the specified feature was found.
func GetProfileForFeature(profiles []*ocpp.Profile, featureName string) *ocpp.Profile {
	for _, p := range profiles {
		if p.SupportsFeature(featureName) {
			return p
		}
	}
	panic("missing profile??")
}

// Creates a Call message, given an OCPP request. A unique ID for the message is automatically generated.
// Returns an error in case the request's feature is not supported on this endpoint.
//
// The created call is not automatically scheduled for transmission and is not added to the list of pending requests.
func CreateCall(request ocpp.Request) (*Call, error) {
	action := request.GetFeatureName()
	// TODO: handle collisions?
	uniqueId := messageIdGenerator()
	call := Call{
		MessageTypeId: CALL,
		UniqueId:      uniqueId,
		Action:        action,
		Payload:       request,
	}
	err := Validate.Struct(call)
	if err != nil {
		return nil, err
	}
	return &call, nil
}

// Creates a CallResult message, given an OCPP response and the message's unique ID.
//
// Returns an error in case the response's feature is not supported on this endpoint.
func CreateCallResult(confirmation ocpp.Response, uniqueId string) (*CallResult, error) {
	callResult := CallResult{
		MessageTypeId: CALL_RESULT,
		UniqueId:      uniqueId,
		Payload:       confirmation,
	}
	err := Validate.Struct(callResult)
	if err != nil {
		return nil, err
	}
	return &callResult, nil
}

// Creates a CallError message, given the message's unique ID and the error.
func CreateCallError(uniqueId string, code ocpp.ErrorCode, description string, details interface{}) (*CallError, error) {
	callError := CallError{
		MessageTypeId:    CALL_ERROR,
		UniqueId:         uniqueId,
		ErrorCode:        code,
		ErrorDescription: description,
		ErrorDetails:     details,
	}
	err := Validate.Struct(callError)
	if err != nil {
		return nil, err
	}
	return &callError, nil
}

// SendRequest serializes a request returning the ID and the bytes.
func SendRequest(request ocpp.Request) (string, []byte, error) {
	call, err := CreateCall(request.(ocpp.Request))
	if err != nil {
		return "", []byte{}, err
	}
	jsonMessage, err := call.MarshalJSON()
	if err != nil {
		return "", []byte{}, err
	}
	return call.UniqueId, jsonMessage, nil
}

// SendResponse serializes a response for a requestID.
func SendResponse(requestID string, response ocpp.Response) ([]byte, error) {
	callResult, err := CreateCallResult(response, requestID)
	if err != nil {
		return []byte{}, err
	}
	return callResult.MarshalJSON()
}

// SendError serializes an error for a requestID.
func SendError(requestID string, errorCode ocpp.ErrorCode, description string, details interface{}) ([]byte, error) {
	callError, err := CreateCallError(requestID, errorCode, description, details)
	if err != nil {
		return []byte{}, err
	}
	jsonMessage, err := callError.MarshalJSON()
	if err != nil {
		return []byte{}, err
	}
	return []byte(jsonMessage), nil
}
