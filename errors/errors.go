package errors

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

var (
	ErrElasticsearchNetworkDown  = errors.New("elasticsearch or network down")
	ErrInvalidToken              = errors.New("invalid token")
	ErrTokenNotFound             = errors.New("token not found")
	ErrInvalidTokenType          = errors.New("invalid token type")
	ErrTokenRevoked              = errors.New("token has been revoked")
	ErrTokenBanned               = errors.New("token has been banned")
	ErrCannotVerifyToken         = errors.New("cannot verify token")
	ErrForbidden                 = errors.New("forbidden")
	ErrDecryptBase64             = errors.New("base64 encoding issue")
	ErrInvalidUsernameOrPassword = errors.New("username or password is invalid")
	ErrPermissionDenied          = errors.New("permission denied")
	ErrPermissionNotFound        = errors.New("permission not found")
	ErrParsePermissionError      = errors.New("parse permission error")
	ErrInvalidBankCountry        = errors.New("invalid bank country")
	ErrIpNotFound                = errors.New("ip address not found")
)

type (
	ValidateErrors struct {
		FieldName string   `json:"field_name"`
		Message   []string `json:"message"`
	}

	Error struct {
		Id                   string                     `json:"id,omitempty"`
		Code                 int32                      `json:"code,omitempty"`
		Message              string                     `json:"message,omitempty"`
		Status               string                     `json:"status,omitempty"`
		ValidateErrors       map[string]*ValidateErrors `json:"validate_errors,omitempty"`
		MfaCode              string                     `json:"mfa_code,omitempty"`
		MobileNumberVerified bool                       `json:"mobile_number_verified,omitempty"`
		MobileNumber         string                     `json:"mobile_number,omitempty"`
		Additional           any                        `json:"additional,omitempty"`
		Data                 any                        `json:"data,omitempty"`
	}
)

func (e *Error) Error() string {
	b, _ := json.Marshal(e)
	return string(b)
}

// New generates a custom error.
func New(id, message string, code int32) error {
	return &Error{
		Id:      id,
		Code:    code,
		Message: message,
		Status:  http.StatusText(int(code)),
	}
}

func NewValidateErrors(v map[string]*ValidateErrors) error {
	return &Error{
		Id:             "422",
		Code:           422,
		Message:        "Validation Error",
		Status:         http.StatusText(int(422)),
		ValidateErrors: v,
	}
}

func NewMfaError(errCode string, mfaCode string, mobileNumberVerified bool, mobileNumber string) error {
	return &Error{
		Id:                   "428",
		Code:                 428,
		Message:              errCode,
		Status:               http.StatusText(int(428)),
		MfaCode:              mfaCode,
		MobileNumberVerified: mobileNumberVerified,
		MobileNumber:         mobileNumber,
	}
}

// Parse tries to parse a JSON string into an error. If that
// fails, it will set the given string as the error message.
func Parse(err string) *Error {
	e := new(Error)
	errr := json.Unmarshal([]byte(err), e)
	if errr != nil {
		e.Message = err
	}
	return e
}

// BadRequest generates a 400 error.
func BadRequest(id, format string, a ...interface{}) error {
	return &Error{
		Id:      id,
		Code:    400,
		Message: fmt.Sprintf(format, a...),
		Status:  http.StatusText(400),
	}
}

// Unauthorized generates a 401 error.
func Unauthorized(id, format string, a ...interface{}) error {
	return &Error{
		Id:      id,
		Code:    401,
		Message: fmt.Sprintf(format, a...),
		Status:  http.StatusText(401),
	}
}

// Forbidden generates a 403 error.
func Forbidden(id, format string, a ...interface{}) error {
	return &Error{
		Id:      id,
		Code:    403,
		Message: fmt.Sprintf(format, a...),
		Status:  http.StatusText(403),
	}
}

// NotFound generates a 404 error.
func NotFound(id, format string, a ...interface{}) error {
	return &Error{
		Id:      id,
		Code:    404,
		Message: fmt.Sprintf(format, a...),
		Status:  http.StatusText(404),
	}
}

// MethodNotAllowed generates a 405 error.
func MethodNotAllowed(id, format string, a ...interface{}) error {
	return &Error{
		Id:      id,
		Code:    405,
		Message: fmt.Sprintf(format, a...),
		Status:  http.StatusText(405),
	}
}

// Timeout generates a 408 error.
func Timeout(id, format string, a ...interface{}) error {
	return &Error{
		Id:      id,
		Code:    408,
		Message: fmt.Sprintf(format, a...),
		Status:  http.StatusText(408),
	}
}

// Conflict generates a 409 error.
func Conflict(id, format string, a ...interface{}) error {
	return &Error{
		Id:      id,
		Code:    409,
		Message: fmt.Sprintf(format, a...),
		Status:  http.StatusText(409),
	}
}

// InternalServerError generates a 500 error.
func InternalServerError(id, format string, a ...interface{}) error {
	return &Error{
		Id:      id,
		Code:    500,
		Message: fmt.Sprintf(format, a...),
		Status:  http.StatusText(500),
	}
}

// TooManyRequests generates a 429 error.
func TooManyRequests(id, format string, a ...interface{}) error {
	return &Error{
		Id:      id,
		Code:    429,
		Message: fmt.Sprintf(format, a...),
		Status:  http.StatusText(429),
	}
}

// TooManyRequestsTTL generates a 429 error.
func TooManyRequestsTTL(id string, ttl any, format string, a ...interface{}) error {
	return &Error{
		Id:      id,
		Code:    429,
		Message: fmt.Sprintf(format, a...),
		Status:  http.StatusText(429),
		Additional: map[string]interface{}{
			"expired_in": ttl,
		},
	}
}

// Equal tries to compare errors
func Equal(err1 error, err2 error) bool {
	verr1, ok1 := err1.(*Error)
	verr2, ok2 := err2.(*Error)

	if ok1 != ok2 {
		return false
	}

	if !ok1 {
		return err1 == err2
	}

	if verr1.Code != verr2.Code {
		return false
	}

	return true
}

// FromError try to convert go error to *Error
func FromError(err error) *Error {
	if verr, ok := err.(*Error); ok && verr != nil {
		return verr
	}

	return Parse(err.Error())
}
