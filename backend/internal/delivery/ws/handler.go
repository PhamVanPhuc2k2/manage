package ws

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	domainrealtime "github.com/PhamVanPhuc2k2/manage/internal/domain/realtime"
	"github.com/PhamVanPhuc2k2/manage/pkg/jwt"
)

// Handler nhận request HTTP và nâng cấp thành WebSocket.
type Handler struct {
	hub      *Hub
	jwt      *jwt.Manager
	sessions domainauth.SessionStore
	upgrader websocket.Upgrader
}

func NewHandler(
	hub *Hub,
	jwtMgr *jwt.Manager,
	sessions domainauth.SessionStore,
	allowedOrigins []string,
) *Handler {
	return &Handler{
		hub:      hub,
		jwt:      jwtMgr,
		sessions: sessions,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			// HandshakeTimeout chặn client mở kết nối rồi bỏ đó giữa chừng.
			HandshakeTimeout: 10 * time.Second,

			// CheckOrigin PHẢI tự viết.
			//
			// Mặc định của gorilla là "chỉ cho phép cùng origin", nhưng trong
			// dự án này frontend và api nằm sau cùng một nginx nên origin có
			// thể khác cổng ở môi trường dev. Quan trọng hơn: WebSocket KHÔNG
			// được bảo vệ bởi CORS như request thường — trình duyệt vẫn cho
			// trang bất kỳ mở kết nối tới đây. Đây chính là lỗ hổng
			// Cross-Site WebSocket Hijacking, và CheckOrigin là hàng rào duy
			// nhất chặn nó ở tầng trình duyệt.
			CheckOrigin: func(r *http.Request) bool {
				origin := r.Header.Get("Origin")
				// Không có Origin: không phải trình duyệt (curl, app di động).
				// Chúng vẫn phải qua bước kiểm tra token bên dưới.
				if origin == "" {
					return true
				}
				return slices.Contains(allowedOrigins, origin)
			},
		},
	}
}

// ServeHTTP xử lý GET /ws.
//
// Xác thực bằng token trên QUERY STRING, không phải header và không phải
// cookie:
//
//   - Trình duyệt KHÔNG cho đặt header tuỳ ý khi mở WebSocket, nên
//     `Authorization: Bearer ...` là bất khả thi từ JavaScript.
//   - Cookie thì trình duyệt tự gửi kèm, và đó chính là điều khiến
//     Cross-Site WebSocket Hijacking khai thác được: một trang độc hại mở
//     kết nối, cookie tự bay theo, và kẻ tấn công đọc được luồng dữ liệu.
//
// Token trên query string có nhược điểm riêng — nó lọt vào access log. Bù
// lại, access token chỉ sống 15 phút và phiên có thể thu hồi tức thì.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("token")
	if raw == "" {
		http.Error(w, "thiếu token", http.StatusUnauthorized)
		return
	}

	claims, err := h.jwt.Verify(raw)
	if err != nil {
		http.Error(w, "token không hợp lệ hoặc đã hết hạn", http.StatusUnauthorized)
		return
	}

	// Kiểm tra phiên còn sống, đúng như middleware RequireAuth của REST.
	//
	// Bỏ bước này thì đăng xuất không cắt được kết nối WebSocket: nó đã mở
	// rồi và sẽ sống tiếp hàng giờ, vì token chỉ được kiểm đúng một lần lúc
	// bắt tay.
	session, err := h.sessions.Get(r.Context(), claims.SessionID)
	if err != nil || session == nil {
		http.Error(w, "phiên đăng nhập đã kết thúc", http.StatusUnauthorized)
		return
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade đã tự ghi phản hồi lỗi, chỉ cần ghi log.
		h.hub.log.Debug().Err(err).Msg("không nâng cấp được lên WebSocket")
		return
	}

	client := &Client{
		hub:         h.hub,
		conn:        conn,
		send:        make(chan []byte, sendBuffer),
		connID:      uuid.New(),
		userID:      claims.UserID,
		employeeID:  claims.EmployeeID,
		deviceInfo:  deviceInfo(r.UserAgent()),
		connectedAt: time.Now(),
		windowStart: time.Now(),
	}

	h.hub.register(client)

	// Context tách khỏi request: request kết thúc ngay sau khi nâng cấp,
	// nhưng kết nối còn sống hàng giờ. Dùng ctx của request sẽ khiến mọi
	// thao tác Redis phía sau bị huỷ ngay lập tức.
	connCtx, cancel := context.WithCancel(context.WithoutCancel(r.Context()))

	h.hub.touchPresence(connCtx, client, true)

	client.sendEnvelope(domainrealtime.TypeWelcome, map[string]any{
		"conn_id":            client.connID.String(),
		"employee_id":        client.employeeID.String(),
		"heartbeat_interval": int(domainrealtime.HeartbeatInterval.Seconds()),
		"instance_id":        h.hub.instanceID,
	})

	go client.writePump()
	go func() {
		defer cancel()
		client.readPump(connCtx)
	}()
}

// deviceInfo rút gọn User-Agent thành mô tả ngắn để hiển thị.
//
// Không phân tích chi tiết: mục đích chỉ là giúp người dùng nhận ra thiết bị
// nào của mình trong danh sách, không phải thống kê trình duyệt.
func deviceInfo(ua string) string {
	ua = strings.TrimSpace(ua)
	if ua == "" {
		return "không rõ"
	}
	if len(ua) > 120 {
		ua = ua[:120]
	}
	return ua
}
