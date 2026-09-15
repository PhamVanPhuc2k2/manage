// Package apperror định nghĩa loại lỗi của ứng dụng và cách dịch chúng
// sang HTTP status.
//
// Nhờ package này, tầng usecase chỉ cần trả lỗi đúng ngữ nghĩa
// (NotFound, Conflict, Forbidden...) mà không cần biết gì về HTTP.
package apperror

import (
	"errors"
	"fmt"
	"net/http"
)

type Kind string

const (
	KindInvalid       Kind = "INVALID_ARGUMENT"
	KindUnauthorized  Kind = "UNAUTHORIZED"
	KindForbidden     Kind = "FORBIDDEN"
	KindNotFound      Kind = "NOT_FOUND"
	KindConflict      Kind = "CONFLICT"
	KindUnprocessable Kind = "UNPROCESSABLE"
	KindRateLimited   Kind = "RATE_LIMITED"
	KindInternal      Kind = "INTERNAL"
)

type Error struct {
	Kind    Kind
	Message string
	Details any
	cause   error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Kind, e.Message, e.cause)
	}
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}

func (e *Error) Unwrap() error { return e.cause }

func New(kind Kind, message string) *Error {
	return &Error{Kind: kind, Message: message}
}

func Wrap(kind Kind, message string, cause error) *Error {
	return &Error{Kind: kind, Message: message, cause: cause}
}

// Các hàm tạo lỗi thường dùng, viết cho gọn ở chỗ gọi.

func NotFound(what string) *Error {
	return New(KindNotFound, fmt.Sprintf("không tìm thấy %s", what))
}

func Invalid(message string, details any) *Error {
	return &Error{Kind: KindInvalid, Message: message, Details: details}
}

func Conflict(message string) *Error {
	return New(KindConflict, message)
}

func Forbidden(message string) *Error {
	return New(KindForbidden, message)
}

func Internal(cause error) *Error {
	return Wrap(KindInternal, "lỗi nội bộ", cause)
}

// HTTPStatus dịch lỗi sang mã HTTP. Lỗi lạ mặc định là 500.
func HTTPStatus(err error) (int, *Error) {
	var appErr *Error
	if !errors.As(err, &appErr) {
		return http.StatusInternalServerError, Internal(err)
	}

	status := map[Kind]int{
		KindInvalid:       http.StatusBadRequest,
		KindUnauthorized:  http.StatusUnauthorized,
		KindForbidden:     http.StatusForbidden,
		KindNotFound:      http.StatusNotFound,
		KindConflict:      http.StatusConflict,
		KindUnprocessable: http.StatusUnprocessableEntity,
		KindRateLimited:   http.StatusTooManyRequests,
		KindInternal:      http.StatusInternalServerError,
	}[appErr.Kind]

	if status == 0 {
		status = http.StatusInternalServerError
	}
	return status, appErr
}
