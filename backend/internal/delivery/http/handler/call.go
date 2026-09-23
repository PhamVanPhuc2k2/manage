package handler

import (
	"context"
	"net/http"
	"strconv"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	appmw "github.com/PhamVanPhuc2k2/manage/internal/delivery/http/middleware"
	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	domaincall "github.com/PhamVanPhuc2k2/manage/internal/domain/call"
	uccall "github.com/PhamVanPhuc2k2/manage/internal/usecase/call"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
	"github.com/PhamVanPhuc2k2/manage/pkg/httpx"
)

type CallHandler struct {
	uc *uccall.Usecase

	// publicURL là địa chỉ WebSocket của SFU mà TRÌNH DUYỆT dùng, không
	// phải địa chỉ backend dùng. Trong Docker hai cái này khác hẳn nhau:
	// backend gọi http://livekit:7880, còn trình duyệt không phân giải
	// được cái tên đó. Trả kèm token để client không phải tự đoán.
	publicURL string
}

func NewCallHandler(uc *uccall.Usecase, publicURL string) *CallHandler {
	return &CallHandler{uc: uc, publicURL: publicURL}
}

// --------------------------------------------------------------- DTO

type callDTO struct {
	ID             string `json:"id"`
	ConversationID string `json:"conversation_id"`
	InitiatorID    string `json:"initiator_id,omitempty"`
	InitiatorName  string `json:"initiator_name,omitempty"`

	Kind   string `json:"kind"`
	Status string `json:"status"`

	RoomName string `json:"room_name"`

	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`

	EndReason string `json:"end_reason,omitempty"`

	// Duration tính sẵn ở máy chủ. Client tự trừ hai mốc thời gian sẽ ra
	// số khác khi đồng hồ máy nó lệch, và lịch sử cuộc gọi là thứ người
	// dùng đem ra đối chiếu với nhau.
	DurationSeconds int `json:"duration_seconds"`

	RelayRatio *float64 `json:"relay_ratio,omitempty"`

	Participants []callParticipantDTO `json:"participants,omitempty"`
}

type callParticipantDTO struct {
	EmployeeID   string `json:"employee_id"`
	EmployeeName string `json:"employee_name,omitempty"`
	AvatarKey    string `json:"avatar_key,omitempty"`

	JoinedAt *time.Time `json:"joined_at,omitempty"`
	LeftAt   *time.Time `json:"left_at,omitempty"`

	InRoom bool `json:"in_room"`

	HadAudio  bool `json:"had_audio"`
	HadVideo  bool `json:"had_video"`
	HadScreen bool `json:"had_screen"`
}

func toCallDTO(c *domaincall.Call) callDTO {
	dto := callDTO{
		ID:              c.ID.String(),
		ConversationID:  c.ConversationID.String(),
		InitiatorName:   c.InitiatorName,
		Kind:            string(c.Kind),
		Status:          string(c.Status),
		RoomName:        c.RoomName,
		StartedAt:       c.StartedAt,
		EndedAt:         c.EndedAt,
		EndReason:       c.EndReason,
		DurationSeconds: int(c.Duration().Seconds()),
		RelayRatio:      c.RelayRatio,
	}
	if c.InitiatorID != nil {
		dto.InitiatorID = c.InitiatorID.String()
	}
	for _, p := range c.Participants {
		dto.Participants = append(dto.Participants, callParticipantDTO{
			EmployeeID:   p.EmployeeID.String(),
			EmployeeName: p.EmployeeName,
			AvatarKey:    p.AvatarKey,
			JoinedAt:     p.JoinedAt,
			LeftAt:       p.LeftAt,
			InRoom:       p.InRoom(),
			HadAudio:     p.HadAudio,
			HadVideo:     p.HadVideo,
			HadScreen:    p.HadScreen,
		})
	}
	return dto
}

// ---------------------------------------------------------- bắt đầu

type startCallRequest struct {
	ConversationID string `json:"conversation_id"`
	Kind           string `json:"kind"`
}

// Start mở một cuộc gọi.
//
// Trả luôn cả token và địa chỉ SFU trong một lượt: mỗi lượt gọi thêm là
// thêm một khoảng lặng giữa lúc bấm nút và lúc người dùng thấy hình mình,
// và khoảng lặng đó là thứ làm người ta bấm nút lần thứ hai.
func (h *CallHandler) Start(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	var req startCallRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}

	conversationID, err := uuid.Parse(req.ConversationID)
	if err != nil {
		Error(w, apperror.Invalid("conversation_id không hợp lệ", nil), requestID)
		return
	}

	kind := domaincall.Kind(req.Kind)
	if !kind.Valid() {
		Error(w, apperror.Invalid("kind phải là audio hoặc video", nil), requestID)
		return
	}

	res, err := h.uc.Start(r.Context(), appmw.ActorFrom(r.Context()),
		conversationID, kind)
	if err != nil {
		Error(w, err, requestID)
		return
	}

	httpx.Created(w, h.joinPayload(res.Call, res.Token, map[string]any{
		"ringing": res.Ringing,
		"busy":    res.Busy,
	}))
}

// ------------------------------------------------------- vòng đời

func (h *CallHandler) Accept(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	res, err := h.uc.Accept(r.Context(), appmw.ActorFrom(r.Context()), id)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, h.joinPayload(res.Call, res.Token, nil))
}

// Reject, Leave và End dùng chung một khuôn vì cả ba đều là "gọi usecase
// rồi trả 200 rỗng". Gộp lại để ba đường không trôi xa nhau theo thời gian.
func (h *CallHandler) Reject(w http.ResponseWriter, r *http.Request) {
	h.act(w, r, h.uc.Reject)
}

func (h *CallHandler) Leave(w http.ResponseWriter, r *http.Request) {
	h.act(w, r, h.uc.Leave)
}

func (h *CallHandler) End(w http.ResponseWriter, r *http.Request) {
	h.act(w, r, h.uc.End)
}

func (h *CallHandler) act(
	w http.ResponseWriter,
	r *http.Request,
	fn func(context.Context, *domainauth.Actor, uuid.UUID) error,
) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}
	if err := fn(r.Context(), appmw.ActorFrom(r.Context()), id); err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, map[string]any{"ok": true})
}

// ------------------------------------------------------------- đọc

func (h *CallHandler) Get(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	c, err := h.uc.Get(r.Context(), appmw.ActorFrom(r.Context()), id)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, toCallDTO(c))
}

// Live trả 200 với data rỗng khi hội thoại không có cuộc gọi nào.
//
// KHÔNG trả 404: "đang không có cuộc gọi" là câu trả lời đúng, không phải
// lỗi. Giao diện gọi endpoint này mỗi lần mở hội thoại, và một 404 ở đó sẽ
// nhuộm đỏ console cùng mọi bảng giám sát lỗi.
func (h *CallHandler) Live(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	conversationID, err := parseUUIDParam(r, "conversationID")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	c, err := h.uc.Live(r.Context(), appmw.ActorFrom(r.Context()), conversationID)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	if c == nil {
		httpx.OK(w, nil)
		return
	}
	httpx.OK(w, toCallDTO(c))
}

func (h *CallHandler) History(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	conversationID, err := parseUUIDParam(r, "conversationID")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	list, err := h.uc.History(r.Context(), appmw.ActorFrom(r.Context()),
		conversationID, limit)
	if err != nil {
		Error(w, err, requestID)
		return
	}

	out := make([]callDTO, 0, len(list))
	for _, c := range list {
		out = append(out, toCallDTO(c))
	}
	httpx.OK(w, out)
}

// -------------------------------------------------------- hạ tầng

// ICEServers cấp danh sách STUN/TURN cho đường P2P của gọi 1-1.
//
// Đường qua SFU KHÔNG dùng danh sách này: LiveKit tự cấp credential TURN
// của nó qua chính đường signaling, nên backend không có việc gì ở đó.
func (h *CallHandler) ICEServers(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	servers, err := h.uc.ICEServers(r.Context(), appmw.ActorFrom(r.Context()))
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, map[string]any{"ice_servers": servers})
}

// Token cấp lại token vào phòng khi token cũ sắp hết hạn.
//
// Cuộc họp dài hơn TTL của token vẫn phải chạy tiếp; không có đường này
// thì người dùng bị rớt giữa buổi mà không hiểu vì sao.
func (h *CallHandler) Token(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	token, err := h.uc.Token(r.Context(), appmw.ActorFrom(r.Context()), id)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, map[string]any{
		"token":     token,
		"media_url": h.publicURL,
	})
}

// joinPayload gom thứ client cần để vào phòng ngay.
func (h *CallHandler) joinPayload(
	c *domaincall.Call,
	token string,
	extra map[string]any,
) map[string]any {
	out := map[string]any{
		"call":      toCallDTO(c),
		"token":     token,
		"media_url": h.publicURL,
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}
