package protocol

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"reflect"

	"gopkg.in/go-playground/validator.v9"

	"github.com/lorenzodonini/ocpp-go/ocpp"
)

// The validator, used for validating incoming/outgoing OCPP messages.
var Validate = validator.New()

// The internal verbose logger
var log ocpp.Logger = &ocpp.VoidLogger{}

func init() {
	_ = Validate.RegisterValidation("errorCode", IsErrorCodeValid)
}

// Sets a custom Logger implementation, allowing the ocpp-j package to log events.
// By default, a VoidLogger is used, so no logs will be sent to any output.
//
// The function panics, if a nil logger is passed.
func SetLogger(logger ocpp.Logger) {
	if logger == nil {
		panic("cannot set a nil logger")
	}
	log = logger
}

// MessageType identifies the type of message exchanged between two OCPP endpoints.
type MessageType int

const (
	CALL        MessageType = 2
	CALL_RESULT MessageType = 3
	CALL_ERROR  MessageType = 4
)

// An OCPP-J message.
type Message interface {
	// Returns the message type identifier of the message.
	GetMessageTypeId() MessageType
	// Returns the unique identifier of the message.
	GetUniqueId() string
	json.Marshaler
}

// Error wraps an OCPP error, containing an ErrorCode, a Description and
// potentially details.
type Error struct {
	Code        ocpp.ErrorCode
	Description string
	Details     interface{}
}

func (err *Error) Error() string {
	return fmt.Sprintf("OCPP-J error %v - %v", err.Code, err.Description)
}

var messageIdGenerator = func() string {
	return fmt.Sprintf("%v", rand.Uint32())
}

// SetMessageIdGenerator sets a lambda function for generating unique IDs for new messages.
// The function is invoked automatically when creating a new Call.
//
// Settings this overrides the default behavior, which is:
//	fmt.Sprintf("%v", rand.Uint32())
func SetMessageIdGenerator(generator func() string) {
	if generator != nil {
		messageIdGenerator = generator
	}
}

// -------------------- Call --------------------

// An OCPP-J Call message, containing an OCPP Request.
type Call struct {
	Message       `validate:"-"`
	MessageTypeId MessageType  `json:"messageTypeId" validate:"required,eq=2"`
	UniqueId      string       `json:"uniqueId" validate:"required,max=36"`
	Action        string       `json:"action" validate:"required,max=36"`
	Payload       ocpp.Request `json:"payload" validate:"required"`
}

func (call *Call) GetMessageTypeId() MessageType {
	return call.MessageTypeId
}

func (call *Call) GetUniqueId() string {
	return call.UniqueId
}

func (call *Call) MarshalJSON() ([]byte, error) {
	fields := make([]interface{}, 4)
	fields[0] = int(call.MessageTypeId)
	fields[1] = call.UniqueId
	fields[2] = call.Action
	fields[3] = call.Payload
	return json.Marshal(fields)
}

// -------------------- Call Result --------------------

// An OCPP-J CallResult message, containing an OCPP Response.
type CallResult struct {
	Message
	MessageTypeId MessageType   `json:"messageTypeId" validate:"required,eq=3"`
	UniqueId      string        `json:"uniqueId" validate:"required,max=36"`
	Payload       ocpp.Response `json:"payload" validate:"required"`
}

func (callResult *CallResult) GetMessageTypeId() MessageType {
	return callResult.MessageTypeId
}

func (callResult *CallResult) GetUniqueId() string {
	return callResult.UniqueId
}

func (callResult *CallResult) MarshalJSON() ([]byte, error) {
	fields := make([]interface{}, 3)
	fields[0] = int(callResult.MessageTypeId)
	fields[1] = callResult.UniqueId
	fields[2] = callResult.Payload
	return json.Marshal(fields)
}

// -------------------- Call Error --------------------

// An OCPP-J CallError message, containing an OCPP Error.
type CallError struct {
	Message
	MessageTypeId    MessageType    `json:"messageTypeId" validate:"required,eq=4"`
	UniqueId         string         `json:"uniqueId" validate:"required,max=36"`
	ErrorCode        ocpp.ErrorCode `json:"errorCode" validate:"errorCode"`
	ErrorDescription string         `json:"errorDescription" validate:"required"`
	ErrorDetails     interface{}    `json:"errorDetails" validate:"omitempty"`
}

func (callError *CallError) GetMessageTypeId() MessageType {
	return callError.MessageTypeId
}

func (callError *CallError) GetUniqueId() string {
	return callError.UniqueId
}

func (callError *CallError) MarshalJSON() ([]byte, error) {
	fields := make([]interface{}, 5)
	fields[0] = int(callError.MessageTypeId)
	fields[1] = callError.UniqueId
	fields[2] = callError.ErrorCode
	fields[3] = callError.ErrorDescription
	fields[4] = callError.ErrorDetails
	return ocppMessageToJson(fields)
}

const (
	NotImplemented                ocpp.ErrorCode = "NotImplemented"                // Requested Action is not known by receiver.
	NotSupported                  ocpp.ErrorCode = "NotSupported"                  // Requested Action is recognized but not supported by the receiver.
	InternalError                 ocpp.ErrorCode = "InternalError"                 // An internal error occurred and the receiver was not able to process the requested Action successfully.
	ProtocolError                 ocpp.ErrorCode = "ProtocolError"                 // Payload for Action is incomplete.
	SecurityError                 ocpp.ErrorCode = "SecurityError"                 // During the processing of Action a security issue occurred preventing receiver from completing the Action successfully.
	FormationViolation            ocpp.ErrorCode = "FormationViolation"            // Payload for Action is syntactically incorrect or not conform the PDU structure for Action.
	PropertyConstraintViolation   ocpp.ErrorCode = "PropertyConstraintViolation"   // Payload is syntactically correct but at least one field contains an invalid value.
	OccurrenceConstraintViolation ocpp.ErrorCode = "OccurrenceConstraintViolation" // Payload for Action is syntactically correct but at least one of the fields violates occurrence constraints.
	TypeConstraintViolation       ocpp.ErrorCode = "TypeConstraintViolation"       // Payload for Action is syntactically correct but at least one of the fields violates data type constraints (e.g. “somestring”: 12).
	GenericError                  ocpp.ErrorCode = "GenericError"                  // Any other error not covered by the previous ones.
)

func IsErrorCodeValid(fl validator.FieldLevel) bool {
	code := ocpp.ErrorCode(fl.Field().String())
	switch code {
	case NotImplemented, NotSupported, InternalError, ProtocolError, SecurityError, FormationViolation, PropertyConstraintViolation, OccurrenceConstraintViolation, TypeConstraintViolation, GenericError:
		return true
	}
	return false
}

// -------------------- Logic --------------------

// Unmarshals an OCPP-J json object from a byte array.
// Returns the array of elements contained in the message.
func ParseRawJsonMessage(dataJson []byte) ([]interface{}, error) {
	var arr []interface{}
	err := json.Unmarshal(dataJson, &arr)
	if err != nil {
		return nil, err
	}
	return arr, nil
}

func ocppMessageToJson(message interface{}) ([]byte, error) {
	jsonData, err := json.Marshal(message)
	if err != nil {
		return nil, err
	}
	jsonData[0] = '['
	jsonData[len(jsonData)-1] = ']'
	return jsonData, nil
}

func getValueLength(value interface{}) int {
	switch value.(type) {
	case int:
		return value.(int)
	case string:
		return len(value.(string))
	default:
		return 0
	}
}

func errorFromValidation(validationErrors validator.ValidationErrors, messageId string) *Error {
	for _, el := range validationErrors {
		switch el.ActualTag() {
		case "required":
			return &Error{Code: OccurrenceConstraintViolation, Description: fmt.Sprintf("Field %v required but not found", el.Namespace())}
		case "max":
			return &Error{Code: PropertyConstraintViolation, Description: fmt.Sprintf("Field %v must be maximum %v, but was %v", el.Namespace(), el.Param(), getValueLength(el.Value()))}
		case "min":
			return &Error{Code: PropertyConstraintViolation, Description: fmt.Sprintf("Field %v must be minimum %v, but was %v", el.Namespace(), el.Param(), getValueLength(el.Value()))}
		case "gte":
			return &Error{Code: PropertyConstraintViolation, Description: fmt.Sprintf("Field %v must be >= %v, but was %v", el.Namespace(), el.Param(), getValueLength(el.Value()))}
		case "gt":
			return &Error{Code: PropertyConstraintViolation, Description: fmt.Sprintf("Field %v must be > %v, but was %v", el.Namespace(), el.Param(), getValueLength(el.Value()))}
		case "lte":
			return &Error{Code: PropertyConstraintViolation, Description: fmt.Sprintf("Field %v must be <= %v, but was %v", el.Namespace(), el.Param(), getValueLength(el.Value()))}
		case "lt":
			return &Error{Code: PropertyConstraintViolation, Description: fmt.Sprintf("Field %v must be < %v, but was %v", el.Namespace(), el.Param(), getValueLength(el.Value()))}
		}
	}
	return &Error{Code: GenericError, Description: fmt.Sprintf("%v", validationErrors.Error())}
}

func parseRawJsonRequest(raw interface{}, requestType reflect.Type) (ocpp.Request, error) {
	bytes, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	request := reflect.New(requestType).Interface()
	err = json.Unmarshal(bytes, &request)
	if err != nil {
		return nil, err
	}
	result := request.(ocpp.Request)
	return result, nil
}

func parseRawJsonConfirmation(raw interface{}, confirmationType reflect.Type) (ocpp.Response, error) {
	bytes, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	confirmation := reflect.New(confirmationType).Interface()
	err = json.Unmarshal(bytes, &confirmation)
	if err != nil {
		return nil, err
	}
	result := confirmation.(ocpp.Response)
	return result, nil
}

func ParseMessageType(arr []interface{}) (MessageType, string, *Error) {
	if len(arr) < 3 {
		return MessageType(0), "", &Error{Code: FormationViolation, Description: "Invalid message. Expected array length >= 3"}
	}
	rawTypeId, ok := arr[0].(float64)
	if !ok {
		return MessageType(0), "", &Error{Code: FormationViolation, Description: fmt.Sprintf("Invalid element %v at 0, expected message type (int)", arr[0])}
	}
	uniqueID, ok := arr[1].(string)
	if !ok {
		return MessageType(0), "", &Error{Code: FormationViolation, Description: fmt.Sprintf("Invalid element %v at 1, expected unique ID (string)", arr[1]), Details: uniqueID}
	}
	return MessageType(rawTypeId), uniqueID, nil
}

// ParseOcppRequest parses a raw JSON array into a Request.
func (endpoint *Endpoint) ParseOcppRequest(arr []interface{}) (ocpp.Request, *Error) {
	if len(arr) != 4 {
		return nil, &Error{Code: FormationViolation, Description: "Invalid Call message. Expected array length 4"}
	}
	uniqueID, ok := arr[1].(string)
	if !ok {
		return nil, &Error{Code: FormationViolation, Description: fmt.Sprintf("Invalid element %v at 1, expected unique ID (string)", arr[1])}
	}
	action, ok := arr[2].(string)
	if !ok {
		return nil, &Error{Code: FormationViolation, Description: fmt.Sprintf("Invalid element %v at 2, expected action (string)", arr[2])}
	}
	profile := GetProfileForFeature(endpoint.Profiles, action)
	request, err := profile.ParseRequest(action, arr[3], parseRawJsonRequest)
	if err != nil {
		return nil, &Error{Code: FormationViolation, Description: err.Error()}
	}
	err = Validate.Struct(request)
	if err != nil {
		return nil, errorFromValidation(err.(validator.ValidationErrors), uniqueID)
	}
	return request, nil
}

// ParseOcppResponse parses a raw JSON array into a Response.
func (endpoint *Endpoint) ParseOcppResponse(featureName string, arr []interface{}) (ocpp.Response, *Error) {
	uniqueID, ok := arr[1].(string)
	if !ok {
		return nil, &Error{Code: FormationViolation, Description: fmt.Sprintf("Invalid element %v at 1, expected unique ID (string)", arr[1])}
	}
	profile := GetProfileForFeature(endpoint.Profiles, featureName)
	confirmation, err := profile.ParseResponse(featureName, arr[2], parseRawJsonConfirmation)
	if err != nil {
		return nil, &Error{Code: FormationViolation, Description: err.Error()}
	}

	err = Validate.Struct(confirmation)
	if err != nil {
		return nil, errorFromValidation(err.(validator.ValidationErrors), uniqueID)
	}
	return confirmation, nil
}

// ParseOcppError parses a raw JSON array into an Error.
func (endpoint *Endpoint) ParseOcppError(arr []interface{}) (*Error, *Error) {
	if len(arr) < 4 {
		return nil, &Error{Code: FormationViolation, Description: "Invalid Call Error message. Expected array length >= 4"}
	}
	var details interface{}
	if len(arr) > 4 {
		details = arr[4]
	}
	rawErrorCode, _ := arr[2].(string)
	// TODO validate
	errorCode := ocpp.ErrorCode(rawErrorCode)
	description, _ := arr[3].(string)
	return &Error{Code: errorCode, Description: description, Details: details}, nil
}
