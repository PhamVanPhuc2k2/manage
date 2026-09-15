package handler

import (
	"net/http"

	chimw "github.com/go-chi/chi/v5/middleware"

	appmw "github.com/PhamVanPhuc2k2/manage/internal/delivery/http/middleware"
	"github.com/PhamVanPhuc2k2/manage/pkg/httpx"
)

type avatarUploadRequest struct {
	ContentType string `json:"content_type"`
}

// RequestAvatarUpload cấp URL để client tải ảnh thẳng lên Cloudflare R2.
func (h *EmployeeHandler) RequestAvatarUpload(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	var req avatarUploadRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}

	ticket, err := h.uc.RequestAvatarUpload(
		r.Context(), appmw.ActorFrom(r.Context()), id, req.ContentType)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, ticket)
}

type avatarConfirmRequest struct {
	Key string `json:"key"`
}

// ConfirmAvatar xác nhận tệp đã tải lên hợp lệ rồi gắn vào hồ sơ.
//
// Bước này bắt buộc: presigned URL chỉ cho phép ghi, nó không kiểm tra nội
// dung tệp. Xem ghi chú trong usecase.
func (h *EmployeeHandler) ConfirmAvatar(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	var req avatarConfirmRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}

	url, err := h.uc.ConfirmAvatar(r.Context(), appmw.ActorFrom(r.Context()), id, req.Key)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, map[string]string{"avatar_url": url})
}

func (h *EmployeeHandler) RemoveAvatar(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	if err := h.uc.RemoveAvatar(r.Context(), appmw.ActorFrom(r.Context()), id); err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, map[string]string{"status": "đã gỡ ảnh đại diện"})
}
