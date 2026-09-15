// Package httpx chuẩn hoá định dạng response JSON cho toàn bộ REST API.
// Mọi endpoint đều trả về cùng một hình dạng, frontend chỉ cần viết một
// bộ xử lý lỗi duy nhất.
package httpx

import (
	"encoding/json"
	"net/http"
)

type Envelope struct {
	Data  any        `json:"data,omitempty"`
	Meta  any        `json:"meta,omitempty"`
	Error *ErrorBody `json:"error,omitempty"`
}

type ErrorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Details   any    `json:"details,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

func JSON(w http.ResponseWriter, status int, payload Envelope) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func OK(w http.ResponseWriter, data any) {
	JSON(w, http.StatusOK, Envelope{Data: data})
}

func Created(w http.ResponseWriter, data any) {
	JSON(w, http.StatusCreated, Envelope{Data: data})
}

func Paginated(w http.ResponseWriter, data any, meta any) {
	JSON(w, http.StatusOK, Envelope{Data: data, Meta: meta})
}
