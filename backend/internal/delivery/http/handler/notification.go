package handler

import (
	"net/http"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	appmw "github.com/PhamVanPhuc2k2/manage/internal/delivery/http/middleware"
	domainnotif "github.com/PhamVanPhuc2k2/manage/internal/domain/notification"
	ucnotif "github.com/PhamVanPhuc2k2/manage/internal/usecase/notification"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
	"github.com/PhamVanPhuc2k2/manage/pkg/httpx"
)

type NotificationHandler struct {
	uc *ucnotif.Usecase
}

func NewNotificationHandler(uc *ucnotif.Usecase) *NotificationHandler {
	return &NotificationHandler{uc: uc}
}

// List trả về thông báo của CHÍNH người đang đăng nhập.
//
// Không có tham số employee_id, kể cả cho quản trị viên: thông báo là hộp thư
// cá nhân, và một endpoint "xem thông báo của người khác" là thứ không có nhu
// cầu nghiệp vụ nào biện minh được.
func (h *NotificationHandler) List(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())
	q := r.URL.Query()
	actor := appmw.ActorFrom(r.Context())

	var typ *domainnotif.Type
	if v := q.Get("type"); v != "" {
		t := domainnotif.Type(v)
		if !t.Valid() {
			Error(w, apperror.Invalid("type không hợp lệ", nil), requestID)
			return
		}
		typ = &t
	}

	var before *time.Time
	if v := q.Get("before"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			Error(w, apperror.Invalid("before phải có dạng RFC3339", nil), requestID)
			return
		}
		before = &t
	}

	items, err := h.uc.List(r.Context(), actor.EmployeeID,
		q.Get("unread") == "true", typ, before, atoiDefault(q.Get("limit"), 0))
	if err != nil {
		Error(w, err, requestID)
		return
	}

	// next_before là cursor cho trang sau: thời điểm của bản ghi CUỐI trang
	// này. Trả sẵn để client không phải tự bóc, và để cách phân trang còn đổi
	// được mà không phải sửa client.
	var next *time.Time
	if len(items) > 0 {
		next = &items[len(items)-1].CreatedAt
	}

	httpx.OK(w, map[string]any{"items": items, "next_before": next})
}

func (h *NotificationHandler) Summary(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	s, err := h.uc.Summary(r.Context(), appmw.ActorFrom(r.Context()).EmployeeID)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, s)
}

type markReadRequest struct {
	IDs []string `json:"ids"`
}

func (h *NotificationHandler) MarkRead(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	var req markReadRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}

	ids := make([]uuid.UUID, 0, len(req.IDs))
	for _, s := range req.IDs {
		id, err := uuid.Parse(s)
		if err != nil {
			Error(w, apperror.Invalid("id không hợp lệ: "+s, nil), requestID)
			return
		}
		ids = append(ids, id)
	}

	s, err := h.uc.MarkRead(r.Context(), appmw.ActorFrom(r.Context()).EmployeeID, ids)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, s)
}

func (h *NotificationHandler) MarkAllRead(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	s, err := h.uc.MarkAllRead(r.Context(), appmw.ActorFrom(r.Context()).EmployeeID)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, s)
}

func (h *NotificationHandler) Preferences(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	items, err := h.uc.Preferences(r.Context(), appmw.ActorFrom(r.Context()).EmployeeID)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, items)
}

type prefRequest struct {
	Type    string `json:"type"`
	Enabled *bool  `json:"enabled"`
}

func (h *NotificationHandler) SetPreference(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	var req prefRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}
	// Con trỏ chứ không phải bool: thiếu trường "enabled" và gửi false là hai
	// ý khác nhau, và giá trị zero của bool không phân biệt được chúng.
	if req.Enabled == nil {
		Error(w, apperror.Invalid("thiếu trường enabled", nil), requestID)
		return
	}

	err := h.uc.SetPreference(r.Context(), appmw.ActorFrom(r.Context()).EmployeeID,
		domainnotif.Type(req.Type), *req.Enabled)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, map[string]any{"ok": true})
}
