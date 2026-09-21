package handler

import (
	"net/http"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	appmw "github.com/PhamVanPhuc2k2/manage/internal/delivery/http/middleware"
	domainchat "github.com/PhamVanPhuc2k2/manage/internal/domain/chat"
	ucchat "github.com/PhamVanPhuc2k2/manage/internal/usecase/chat"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
	"github.com/PhamVanPhuc2k2/manage/pkg/httpx"
)

type ChatHandler struct {
	uc *ucchat.Usecase
}

func NewChatHandler(uc *ucchat.Usecase) *ChatHandler {
	return &ChatHandler{uc: uc}
}

// --------------------------------------------------------------- DTO

type conversationDTO struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	MemberCount int    `json:"member_count"`
	UnreadCount int    `json:"unread_count"`

	IsPinned bool `json:"is_pinned"`
	IsMuted  bool `json:"is_muted"`
	IsAdmin  bool `json:"is_admin"`
	// Managed cho client biết không được hiện nút sửa thành viên.
	Managed bool `json:"managed"`

	PeerID     string `json:"peer_id,omitempty"`
	PeerStatus string `json:"peer_status,omitempty"`

	LastMessageAt *time.Time `json:"last_message_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

func toConversationDTO(c *domainchat.Conversation) conversationDTO {
	dto := conversationDTO{
		ID:            c.ID.String(),
		Kind:          string(c.Kind),
		Name:          c.DisplayName,
		MemberCount:   c.MemberCount,
		UnreadCount:   c.UnreadCount,
		IsPinned:      c.IsPinned,
		IsMuted:       c.IsMuted,
		IsAdmin:       c.IsAdmin,
		Managed:       c.Kind.Managed(),
		PeerStatus:    c.PeerStatus,
		LastMessageAt: c.LastMessageAt,
		CreatedAt:     c.CreatedAt,
	}
	if c.PeerID != nil {
		dto.PeerID = c.PeerID.String()
	}
	return dto
}

type chatMemberDTO struct {
	EmployeeID   string `json:"employee_id"`
	EmployeeName string `json:"employee_name"`
	EmployeeCode string `json:"employee_code,omitempty"`
	IsAdmin      bool   `json:"is_admin"`
	Status       string `json:"status"`
}

func toChatMemberDTO(m *domainchat.Member) chatMemberDTO {
	return chatMemberDTO{
		EmployeeID:   m.EmployeeID.String(),
		EmployeeName: m.EmployeeName,
		EmployeeCode: m.EmployeeCode,
		IsAdmin:      m.IsAdmin,
		Status:       m.Status,
	}
}

// Tin nhắn KHÔNG có DTO riêng ở tầng này.
//
// Hình dạng của nó là ucchat.MessageView, dùng chung cho cả REST lẫn WebSocket.
// Một DTO riêng ở đây sẽ chỉ áp dụng cho đường REST, và bản tin realtime — vốn
// không đi qua handler — sẽ về với tên trường khác. Xem ghi chú ở MessageView.

// ---------------------------------------------------------- hội thoại

func (h *ChatHandler) ListConversations(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	items, err := h.uc.List(r.Context(), appmw.ActorFrom(r.Context()),
		r.URL.Query().Get("q"))
	if err != nil {
		Error(w, err, requestID)
		return
	}

	out := make([]conversationDTO, 0, len(items))
	for _, c := range items {
		out = append(out, toConversationDTO(c))
	}
	httpx.OK(w, out)
}

func (h *ChatHandler) GetConversation(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	c, members, err := h.uc.Get(r.Context(), appmw.ActorFrom(r.Context()), id)
	if err != nil {
		Error(w, err, requestID)
		return
	}

	out := make([]chatMemberDTO, 0, len(members))
	for _, m := range members {
		out = append(out, toChatMemberDTO(m))
	}
	httpx.OK(w, map[string]any{
		"conversation": toConversationDTO(c),
		"members":      out,
	})
}

type createConversationRequest struct {
	Kind      string   `json:"kind"`
	PeerID    string   `json:"peer_id"`
	Name      string   `json:"name"`
	MemberIDs []string `json:"member_ids"`
}

// CreateConversation phục vụ cả hai kiểu tạo: 1-1 và nhóm.
//
// Một endpoint thay vì hai vì giao diện chỉ có một nút "cuộc trò chuyện mới",
// và nó chọn kiểu ngay trong biểu mẫu.
func (h *ChatHandler) CreateConversation(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())
	actor := appmw.ActorFrom(r.Context())

	var req createConversationRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}

	if req.Kind == string(domainchat.KindDirect) {
		peerID, err := uuid.Parse(req.PeerID)
		if err != nil {
			Error(w, apperror.Invalid("peer_id không hợp lệ", nil), requestID)
			return
		}

		c, err := h.uc.OpenDirect(r.Context(), actor, peerID)
		if err != nil {
			Error(w, err, requestID)
			return
		}
		httpx.Created(w, toConversationDTO(c))
		return
	}

	if req.Kind != "" && req.Kind != string(domainchat.KindGroup) {
		Error(w, apperror.Invalid(
			"chỉ tạo được hội thoại 1-1 hoặc nhóm; nhóm phòng ban và dự án do hệ thống dựng",
			nil), requestID)
		return
	}

	ids, err := parseUUIDList(req.MemberIDs)
	if err != nil {
		Error(w, err, requestID)
		return
	}

	c, err := h.uc.CreateGroup(r.Context(), actor, ucchat.CreateGroupInput{
		Name:      req.Name,
		MemberIDs: ids,
	})
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.Created(w, toConversationDTO(c))
}

type chatRenameRequest struct {
	Name string `json:"name"`
}

func (h *ChatHandler) Rename(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	var req chatRenameRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}

	c, err := h.uc.Rename(r.Context(), appmw.ActorFrom(r.Context()), id, req.Name)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, toConversationDTO(c))
}

type chatMembersRequest struct {
	MemberIDs []string `json:"member_ids"`
}

func (h *ChatHandler) AddMembers(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	var req chatMembersRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}

	ids, err := parseUUIDList(req.MemberIDs)
	if err != nil {
		Error(w, err, requestID)
		return
	}

	if err := h.uc.AddMembers(r.Context(), appmw.ActorFrom(r.Context()), id, ids); err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, map[string]any{"ok": true})
}

func (h *ChatHandler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}
	employeeID, err := parseUUIDParam(r, "employeeID")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	if err := h.uc.RemoveMember(r.Context(), appmw.ActorFrom(r.Context()), id, employeeID); err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, map[string]any{"ok": true})
}

type chatAdminRequest struct {
	IsAdmin *bool `json:"is_admin"`
}

func (h *ChatHandler) SetAdmin(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}
	employeeID, err := parseUUIDParam(r, "employeeID")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	var req chatAdminRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}
	if req.IsAdmin == nil {
		Error(w, apperror.Invalid("thiếu trường is_admin", nil), requestID)
		return
	}

	err = h.uc.SetAdmin(r.Context(), appmw.ActorFrom(r.Context()), id, employeeID, *req.IsAdmin)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, map[string]any{"ok": true})
}

func (h *ChatHandler) Leave(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	if err := h.uc.Leave(r.Context(), appmw.ActorFrom(r.Context()), id); err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, map[string]any{"ok": true})
}

type chatFlagRequest struct {
	Pinned *bool `json:"pinned"`
	Muted  *bool `json:"muted"`
}

// SetFlags gom ghim và tắt thông báo vào một endpoint.
//
// Cả hai đều là tuỳ chọn riêng của người xem trên cùng một hội thoại, và tách
// thành hai route chỉ tạo thêm đường mà không thêm ý nghĩa nào.
func (h *ChatHandler) SetFlags(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())
	actor := appmw.ActorFrom(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	var req chatFlagRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}

	if req.Pinned != nil {
		if err := h.uc.SetPinned(r.Context(), actor, id, *req.Pinned); err != nil {
			Error(w, err, requestID)
			return
		}
	}
	if req.Muted != nil {
		if err := h.uc.SetMuted(r.Context(), actor, id, *req.Muted); err != nil {
			Error(w, err, requestID)
			return
		}
	}
	httpx.OK(w, map[string]any{"ok": true})
}

// ----------------------------------------------------------- tin nhắn

func (h *ChatHandler) History(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())
	q := r.URL.Query()

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
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

	items, err := h.uc.History(r.Context(), appmw.ActorFrom(r.Context()),
		id, before, q.Get("q"), atoiDefault(q.Get("limit"), 0))
	if err != nil {
		Error(w, err, requestID)
		return
	}

	// Danh sách trả về theo thứ tự tăng dần, nên cursor trang trước là tin
	// ĐẦU tiên — đó là tin cũ nhất đang hiện.
	var next *time.Time
	if len(items) > 0 {
		next = &items[0].CreatedAt
	}

	httpx.OK(w, map[string]any{"items": items, "next_before": next})
}

type chatSendRequest struct {
	Content         string                  `json:"content"`
	Kind            string                  `json:"kind"`
	ReplyToID       string                  `json:"reply_to_id"`
	ClientMessageID string                  `json:"client_message_id"`
	Attachments     []chatAttachmentRequest `json:"attachments"`
}

type chatAttachmentRequest struct {
	StorageKey  string `json:"storage_key"`
	FileName    string `json:"file_name"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
	Width       *int   `json:"width"`
	Height      *int   `json:"height"`
}

func (h *ChatHandler) Send(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	var req chatSendRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}

	in := ucchat.SendInput{
		ConversationID:  id,
		Content:         req.Content,
		Kind:            domainchat.MessageKind(req.Kind),
		ClientMessageID: req.ClientMessageID,
	}
	if req.ReplyToID != "" {
		replyID, err := uuid.Parse(req.ReplyToID)
		if err != nil {
			Error(w, apperror.Invalid("reply_to_id không hợp lệ", nil), requestID)
			return
		}
		in.ReplyToID = &replyID
	}
	for _, a := range req.Attachments {
		in.Attachments = append(in.Attachments, ucchat.AttachmentInput{
			StorageKey:  a.StorageKey,
			FileName:    a.FileName,
			ContentType: a.ContentType,
			SizeBytes:   a.SizeBytes,
			Width:       a.Width,
			Height:      a.Height,
		})
	}

	m, err := h.uc.Send(r.Context(), appmw.ActorFrom(r.Context()), in)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.Created(w, m)
}

type chatEditRequest struct {
	Content string `json:"content"`
}

func (h *ChatHandler) EditMessage(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	messageID, err := parseUUIDParam(r, "messageID")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	var req chatEditRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}

	m, err := h.uc.Edit(r.Context(), appmw.ActorFrom(r.Context()), messageID, req.Content)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, m)
}

func (h *ChatHandler) DeleteMessage(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	messageID, err := parseUUIDParam(r, "messageID")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	if err := h.uc.Delete(r.Context(), appmw.ActorFrom(r.Context()), messageID); err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, map[string]any{"ok": true})
}

type chatReadRequest struct {
	MessageID string `json:"message_id"`
}

func (h *ChatHandler) MarkRead(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	var req chatReadRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}
	messageID, err := uuid.Parse(req.MessageID)
	if err != nil {
		Error(w, apperror.Invalid("message_id không hợp lệ", nil), requestID)
		return
	}

	unread, err := h.uc.MarkRead(r.Context(), appmw.ActorFrom(r.Context()), id, messageID)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, map[string]any{"unread": unread})
}

func (h *ChatHandler) Unread(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	n, err := h.uc.TotalUnread(r.Context(), appmw.ActorFrom(r.Context()))
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, map[string]any{"unread": n})
}

type chatUploadRequest struct {
	FileName    string `json:"file_name"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
}

// PresignUpload cấp URL để client tải tệp THẲNG lên R2.
func (h *ChatHandler) PresignUpload(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	var req chatUploadRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}

	key, url, err := h.uc.PresignUpload(r.Context(), appmw.ActorFrom(r.Context()),
		id, req.FileName, req.ContentType, req.SizeBytes)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, map[string]any{"storage_key": key, "upload_url": url})
}

func parseUUIDList(values []string) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, 0, len(values))
	for _, s := range values {
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, apperror.Invalid("id không hợp lệ: "+s, nil)
		}
		out = append(out, id)
	}
	return out, nil
}
