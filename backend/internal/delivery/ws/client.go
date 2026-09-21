package ws

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	domainrealtime "github.com/PhamVanPhuc2k2/manage/internal/domain/realtime"
)

const (
	// maxMessageBytes chặn bản tin quá lớn.
	//
	// 32KB đủ rộng cho mọi bản tin điều khiển và một tin nhắn chat dài.
	// Không đặt ngưỡng thì một client độc hại gửi vài bản tin trăm MB là
	// làm hết RAM của instance.
	maxMessageBytes = 32 * 1024

	// writeWait: hạn ghi một frame. Quá hạn nghĩa là client đã chết hoặc
	// mạng nghẽn tới mức không cứu được — đóng còn hơn giữ goroutine treo.
	writeWait = 10 * time.Second

	// pongWait phải LỚN HƠN pingPeriod, nếu không server sẽ tự cắt kết nối
	// khoẻ mạnh trước khi kịp nhận pong cho ping vừa gửi.
	pongWait   = 60 * time.Second
	pingPeriod = (pongWait * 9) / 10

	// sendBuffer: số bản tin xếp hàng cho một client trước khi bị coi là
	// quá chậm. Client chậm sẽ bị đóng kết nối thay vì kéo cả hub chậm theo.
	sendBuffer = 64

	// Giới hạn tốc độ gửi: tối đa rateLimitBurst bản tin trong rateLimitWindow.
	// Chống spam, và chặn cả client lỗi gửi vòng lặp vô hạn do bug.
	rateLimitWindow = 10 * time.Second
	rateLimitBurst  = 60
)

// Client là một kết nối WebSocket.
//
// Mỗi client có ĐÚNG hai goroutine: readPump và writePump. Quy ước của
// gorilla/websocket là chỉ một goroutine được ghi tại một thời điểm — mọi
// lệnh ghi vì vậy đều phải đi qua kênh `send`, không ai được gọi thẳng
// conn.WriteMessage từ ngoài.
type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte

	connID      uuid.UUID
	userID      uuid.UUID
	employeeID  uuid.UUID
	deviceInfo  string
	connectedAt time.Time

	// closeOnce bảo đảm kênh send chỉ đóng một lần. Đóng hai lần là panic,
	// và có hai đường dẫn tới việc đóng: client tự ngắt, và hub gọi CloseAll.
	closeOnce sync.Once

	// Bộ đếm giới hạn tốc độ. Chỉ readPump chạm vào nên không cần khoá.
	windowStart time.Time
	windowCount int
}

func encode(e domainrealtime.Envelope) ([]byte, error) {
	return json.Marshal(e)
}

// trySend đưa bản tin vào hàng đợi của client.
//
// KHÔNG chặn: nếu hàng đợi đầy thì client này đang quá chậm (mạng kém, hoặc
// tab bị trình duyệt treo). Chặn ở đây sẽ kéo chậm mọi người nhận khác của
// cùng một bản tin, nên thà bỏ kết nối chậm đi và để client tự nối lại.
func (c *Client) trySend(data []byte) {
	defer func() {
		// Kênh có thể vừa bị đóng bởi unregister ngay giữa hai dòng lệnh.
		// Hồi phục im lặng: kết nối đang đóng thì bản tin này không còn
		// người nhận, đó không phải lỗi.
		_ = recover()
	}()

	select {
	case c.send <- data:
	default:
		c.hub.log.Warn().
			Str("employee_id", c.employeeID.String()).
			Str("conn_id", c.connID.String()).
			Msg("hàng đợi gửi đầy, đóng kết nối chậm")
		c.closeOnce.Do(func() { close(c.send) })
	}
}

// sendEnvelope đóng gói và gửi một bản tin cho riêng client này.
func (c *Client) sendEnvelope(typ string, payload any) {
	e, err := domainrealtime.NewEnvelope(typ, payload, "")
	if err != nil {
		return
	}
	data, err := encode(e)
	if err != nil {
		return
	}
	c.trySend(data)
}

// allow kiểm tra giới hạn tốc độ theo cửa sổ trượt thô.
//
// Cửa sổ cố định chứ không phải token bucket chính xác: ở đây chỉ cần chặn
// spam rõ ràng, và một thuật toán đơn giản không có trạng thái ẩn thì dễ
// suy luận hơn nhiều khi đi tìm lỗi.
func (c *Client) allow(now time.Time) bool {
	if now.Sub(c.windowStart) > rateLimitWindow {
		c.windowStart = now
		c.windowCount = 0
	}
	c.windowCount++
	return c.windowCount <= rateLimitBurst
}

// readPump đọc bản tin từ client cho tới khi kết nối đóng.
//
// Chạy trong goroutine riêng. Khi nó thoát, kết nối coi như kết thúc: nó
// chịu trách nhiệm gỡ đăng ký và dọn presence.
func (c *Client) readPump(ctx context.Context) {
	defer func() {
		c.hub.unregister(c)
		c.hub.removePresence(context.WithoutCancel(ctx), c)
		_ = c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageBytes)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))

	// Mỗi pong nhận được là một bằng chứng client còn sống — đẩy hạn đọc ra.
	// Thiếu handler này, kết nối khoẻ mạnh vẫn bị cắt sau đúng pongWait.
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				c.hub.log.Debug().Err(err).
					Str("conn_id", c.connID.String()).
					Msg("kết nối đóng bất thường")
			}
			return
		}

		now := time.Now()
		if !c.allow(now) {
			c.sendEnvelope(domainrealtime.TypeError, domainrealtime.ErrorPayload{
				Code:    "RATE_LIMITED",
				Message: "Gửi quá nhanh, tạm dừng một lát",
			})
			continue
		}

		var e domainrealtime.Envelope
		if err := json.Unmarshal(raw, &e); err != nil {
			c.sendEnvelope(domainrealtime.TypeError, domainrealtime.ErrorPayload{
				Code:    "BAD_MESSAGE",
				Message: "Bản tin không đúng định dạng",
			})
			continue
		}

		c.handle(ctx, e)
	}
}

// handle định tuyến bản tin theo `type`.
//
// Danh sách loại bản tin client được phép gửi cố ý rất hẹp. Mọi thứ khác bị
// từ chối: WebSocket không phải một cổng API thứ hai, nghiệp vụ đi qua REST
// nơi đã có sẵn phân quyền và nhật ký.
func (c *Client) handle(ctx context.Context, e domainrealtime.Envelope) {
	switch e.Type {
	case domainrealtime.TypePing:
		c.sendEnvelope(domainrealtime.TypePong, nil)

	case domainrealtime.TypeHeartbeat:
		// Thiếu payload thì coi như đang hoạt động: client cũ chưa biết gửi
		// cờ vẫn phải được tính là online.
		p := domainrealtime.HeartbeatPayload{IsActive: true}
		if len(e.Payload) > 0 {
			_ = json.Unmarshal(e.Payload, &p)
		}

		// Chỉ ghi presence vào Redis. Việc dựng dữ liệu chấm công do một
		// job nền quét Redis mỗi phút đảm nhiệm — xem usecase/attendance.
		//
		// Ghi thẳng vào database ở đây sẽ là một lệnh INSERT cho mỗi nhân
		// viên mỗi 30 giây; với 200 người là 400 lượt ghi mỗi phút để thu
		// được đúng lượng thông tin mà một lần quét gom lại được.
		c.hub.touchPresence(ctx, c, p.IsActive)

	default:
		c.sendEnvelope(domainrealtime.TypeError, domainrealtime.ErrorPayload{
			Code:    "UNKNOWN_TYPE",
			Message: "Loại bản tin không được hỗ trợ: " + e.Type,
		})
	}
}

// writePump là goroutine DUY NHẤT được ghi vào kết nối này.
//
// Nó cũng gửi ping định kỳ. Ping do SERVER chủ động gửi chứ không chờ client:
// nhiều proxy (kể cả nginx) đóng kết nối im lặng sau một khoảng không có dữ
// liệu, và bên bị đóng không hề biết cho tới lần ghi tiếp theo.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()

	for {
		select {
		case data, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// Kênh đã đóng: hub muốn kết thúc kết nối này.
				_ = c.conn.WriteMessage(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}

		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
