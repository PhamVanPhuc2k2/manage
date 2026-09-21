// Package ws là tầng delivery WebSocket của api. Đối xứng với tầng http:
// nó dịch giữa giao thức và usecase, tuyệt đối không chứa logic nghiệp vụ.
package ws

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	domainrealtime "github.com/PhamVanPhuc2k2/manage/internal/domain/realtime"
)

// Hub giữ mọi kết nối WebSocket của MỘT instance api.
//
// Vì sao dùng RWMutex + map thay vì một goroutine điều phối duy nhất (mẫu
// phổ biến của gorilla): mẫu goroutine đơn khiến mọi thao tác gửi phải xếp
// hàng qua một kênh, và một client có kênh gửi đầy sẽ làm nghẽn cả hub.
// Với map có khoá, việc gửi tới hai người khác nhau chạy song song, và mỗi
// client tự chịu trách nhiệm cho kênh gửi của mình.
type Hub struct {
	log        zerolog.Logger
	instanceID string

	mu sync.RWMutex
	// Khoá theo EMPLOYEE ID chứ không phải user id, vì mọi module nghiệp vụ
	// đều làm việc với nhân viên. Giá trị là TẬP kết nối: một người có thể
	// mở máy tính, điện thoại và thêm vài tab cùng lúc.
	clients map[uuid.UUID]map[*Client]struct{}

	presence  domainrealtime.PresenceStore
	broadcast domainrealtime.Broadcaster

	// chat có thể nil (test, hoặc bản chạy chưa bật chat). Khi nil, các bản
	// tin chat gửi lên bị từ chối tử tế thay vì làm panic cả tiến trình.
	chat ChatService
}

// SetChat cắm module chat vào hub.
//
// Tách khỏi NewHub vì thứ tự khởi tạo: usecase chat cần một Pusher, mà Pusher
// lại bọc chính Hub này. Truyền qua hàm dựng sẽ tạo ra vòng tròn không gỡ
// được ở composition root.
func (h *Hub) SetChat(c ChatService) { h.chat = c }

func NewHub(
	log zerolog.Logger,
	instanceID string,
	presence domainrealtime.PresenceStore,
	broadcast domainrealtime.Broadcaster,
) *Hub {
	return &Hub{
		log:        log.With().Str("component", "ws-hub").Logger(),
		instanceID: instanceID,
		clients:    make(map[uuid.UUID]map[*Client]struct{}),
		presence:   presence,
		broadcast:  broadcast,
	}
}

func (h *Hub) register(c *Client) {
	h.mu.Lock()
	if h.clients[c.employeeID] == nil {
		h.clients[c.employeeID] = make(map[*Client]struct{})
	}
	h.clients[c.employeeID][c] = struct{}{}
	n := len(h.clients[c.employeeID])
	h.mu.Unlock()

	h.log.Debug().
		Str("employee_id", c.employeeID.String()).
		Str("conn_id", c.connID.String()).
		Int("connections", n).
		Msg("client kết nối")
}

func (h *Hub) unregister(c *Client) {
	h.mu.Lock()
	conns := h.clients[c.employeeID]
	if conns != nil {
		delete(conns, c)
		// Dọn luôn map con khi rỗng. Không dọn thì map cha phình dần theo
		// số người từng đăng nhập và không bao giờ co lại.
		if len(conns) == 0 {
			delete(h.clients, c.employeeID)
		}
	}
	h.mu.Unlock()

	// Đóng kênh gửi ở ĐÂY, sau khi đã gỡ khỏi map.
	//
	// Thứ tự này quan trọng: gỡ trước thì không còn ai tìm thấy client để
	// gửi vào kênh nữa, nên không thể có chuyện "gửi vào kênh đã đóng" —
	// lỗi đó gây panic và làm sập cả tiến trình.
	c.closeOnce.Do(func() { close(c.send) })
}

// Deliver gửi bản tin tới những người nhận đang nối vào INSTANCE NÀY.
//
// Không tự đi tìm ở instance khác: việc đó do Broadcaster lo. Hàm này là
// chặng cuối, và nó cố ý không biết gì về các instance còn lại.
func (h *Hub) Deliver(msg domainrealtime.Message) {
	data, err := encode(msg.Envelope)
	if err != nil {
		h.log.Error().Err(err).Str("type", msg.Envelope.Type).
			Msg("không mã hoá được bản tin")
		return
	}

	h.mu.RLock()
	targets := make([]*Client, 0, 8)
	if msg.Broadcast {
		for _, conns := range h.clients {
			for c := range conns {
				targets = append(targets, c)
			}
		}
	} else {
		for _, id := range msg.Recipients {
			for c := range h.clients[id] {
				targets = append(targets, c)
			}
		}
	}
	h.mu.RUnlock()

	// Gửi SAU KHI đã nhả khoá đọc.
	//
	// Gửi trong lúc còn giữ khoá sẽ chặn mọi kết nối mới và mọi lần ngắt
	// kết nối cho tới khi client chậm nhất nhận xong.
	for _, c := range targets {
		c.trySend(data)
	}
}

// Publish phát bản tin ra TOÀN HỆ THỐNG, qua mọi instance.
//
// Đây là hàm mà các module nghiệp vụ nên gọi. Gọi Deliver trực tiếp chỉ
// đúng khi chắc chắn người nhận nối vào chính instance này — điều không ai
// biết trước được.
func (h *Hub) Publish(ctx context.Context, msg domainrealtime.Message) {
	if h.broadcast == nil {
		h.Deliver(msg) // chạy một mình, không có hàng đợi
		return
	}
	if err := h.broadcast.Broadcast(ctx, msg); err != nil {
		h.log.Error().Err(err).Str("type", msg.Envelope.Type).
			Msg("không phát được bản tin, gửi tạm trong instance này")
		// Suy giảm êm: người nối vào instance khác sẽ không nhận được, nhưng
		// im lặng hoàn toàn còn tệ hơn.
		h.Deliver(msg)
	}
}

// OnlineEmployees trả về danh sách nhân viên đang nối vào instance NÀY.
// Dùng cho chẩn đoán; câu hỏi "ai đang online" toàn hệ thống hỏi PresenceStore.
func (h *Hub) OnlineEmployees() []uuid.UUID {
	h.mu.RLock()
	defer h.mu.RUnlock()

	out := make([]uuid.UUID, 0, len(h.clients))
	for id := range h.clients {
		out = append(out, id)
	}
	return out
}

// ConnectionCount là số kết nối đang mở trên instance này.
// Phase 6 sẽ đẩy con số này ra /metrics.
func (h *Hub) ConnectionCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	n := 0
	for _, conns := range h.clients {
		n += len(conns)
	}
	return n
}

// CloseAll đóng mọi kết nối. Gọi lúc tắt êm để client biết đường nối lại
// thay vì treo chờ tới khi hết hạn TCP.
func (h *Hub) CloseAll() {
	h.mu.Lock()
	all := make([]*Client, 0, len(h.clients))
	for _, conns := range h.clients {
		for c := range conns {
			all = append(all, c)
		}
	}
	h.clients = make(map[uuid.UUID]map[*Client]struct{})
	h.mu.Unlock()

	for _, c := range all {
		c.closeOnce.Do(func() { close(c.send) })
	}
	h.log.Info().Int("connections", len(all)).Msg("đã đóng mọi kết nối WebSocket")
}

// touchPresence ghi nhận kết nối còn sống vào Redis.
func (h *Hub) touchPresence(ctx context.Context, c *Client, isActive bool) {
	if h.presence == nil {
		return
	}
	now := time.Now()
	conn := &domainrealtime.Connection{
		ConnID:      c.connID,
		InstanceID:  h.instanceID,
		IsActive:    isActive,
		ConnectedAt: c.connectedAt,
		LastSeenAt:  now,
		DeviceInfo:  c.deviceInfo,
	}
	if err := h.presence.Touch(ctx, c.employeeID, conn); err != nil {
		h.log.Warn().Err(err).
			Str("employee_id", c.employeeID.String()).
			Msg("không ghi được presence")
	}
}

func (h *Hub) removePresence(ctx context.Context, c *Client) {
	if h.presence == nil {
		return
	}
	if err := h.presence.Remove(ctx, c.employeeID, c.connID); err != nil {
		h.log.Warn().Err(err).
			Str("employee_id", c.employeeID.String()).
			Msg("không gỡ được presence")
	}
}
