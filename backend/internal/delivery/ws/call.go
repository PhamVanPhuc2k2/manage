package ws

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	domainrealtime "github.com/PhamVanPhuc2k2/manage/internal/domain/realtime"
)

// CallService là phần module gọi mà tầng WebSocket cần.
//
// Đúng hai thao tác, và cả hai đều KHÔNG làm được qua REST:
//
//   - RelaySignal: một lượt đục NAT sinh hàng chục ICE candidate trong vài
//     giây. Mỗi cái một request HTTP thì người dùng ngồi nhìn màn hình đen
//     lâu hơn hẳn, và đó là lúc họ bấm nút gọi lần thứ hai.
//   - HandleDisconnect: chỉ hub mới biết kết nối cuối cùng của một người
//     vừa đóng. Không có ai gửi request HTTP báo "tôi vừa đóng laptop".
type CallService interface {
	RelaySignal(ctx context.Context, actor *domainauth.Actor,
		eventType string, p domainrealtime.CallSignalPayload) error
	HandleDisconnect(ctx context.Context, employeeID uuid.UUID)
}

// handleCall xử lý hai loại bản tin signaling client gửi lên.
//
// Trả về false khi `type` không phải của gọi, để handle() đi tiếp.
func (c *Client) handleCall(ctx context.Context, e domainrealtime.Envelope) bool {
	switch e.Type {
	case domainrealtime.TypeCallSDP, domainrealtime.TypeCallICE:
	default:
		return false
	}

	if c.hub.call == nil {
		c.callError("CALL_UNAVAILABLE", "Chức năng gọi chưa sẵn sàng")
		return true
	}
	// Kiểm quyền ĐÚNG như REST. Thiếu bước này thì WebSocket thành cửa sau
	// đi vòng qua toàn bộ phân quyền của HTTP.
	if !c.actor.Can(domainauth.PermCallRead) {
		c.callError("FORBIDDEN", "Không có quyền dùng chức năng gọi")
		return true
	}

	var p domainrealtime.CallSignalPayload
	if err := json.Unmarshal(e.Payload, &p); err != nil || p.CallID == uuid.Nil {
		c.callError("BAD_MESSAGE", "Thiếu hoặc sai call_id")
		return true
	}

	// From do MÁY CHỦ đặt, không lấy từ payload — usecase ghi đè lại. Tin
	// client ở đây là cho phép bất kỳ ai mạo danh người khác trong lúc
	// thương lượng kết nối.
	if err := c.hub.call.RelaySignal(ctx, c.actor, e.Type, p); err != nil {
		c.callError("SIGNAL_FAILED", err.Error())
	}
	return true
}

func (c *Client) callError(code, message string) {
	c.sendEnvelope(domainrealtime.TypeError,
		domainrealtime.ErrorPayload{Code: code, Message: message})
}
