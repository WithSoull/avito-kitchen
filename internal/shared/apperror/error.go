package apperror

import "fmt"

type Kind string

const (
	KindBadRequest      Kind = "bad_request"
	KindUnauthorized    Kind = "unauthorized"
	KindForbidden       Kind = "forbidden"
	KindNotFound        Kind = "not_found"
	KindConflict        Kind = "conflict"
	KindValidation      Kind = "validation"
	KindTooLarge        Kind = "too_large"
	KindTooManyRequests Kind = "too_many_requests"
	KindInternal        Kind = "internal"
	KindUnavailable     Kind = "unavailable"
)

type Error struct {
	Kind   Kind
	Code   string
	Detail string
	Cause  error
}

func New(kind Kind, code, detail string) *Error {
	return &Error{Kind: kind, Code: code, Detail: detail}
}

func Wrap(kind Kind, code, detail string, cause error) *Error {
	return &Error{Kind: kind, Code: code, Detail: detail, Cause: cause}
}

func (e *Error) Error() string {
	if e.Cause == nil {
		return fmt.Sprintf("%s: %s", e.Code, e.Detail)
	}
	return fmt.Sprintf("%s: %s: %v", e.Code, e.Detail, e.Cause)
}

func (e *Error) Unwrap() error { return e.Cause }
