package call

import (
	"context"

	"github.com/google/uuid"

	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	domaincall "github.com/PhamVanPhuc2k2/manage/internal/domain/call"
	domainrealtime "github.com/PhamVanPhuc2k2/manage/internal/domain/realtime"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
)

// =========================================================================
// ĐỌC
// =========================================================================

// Get đọc một cuộc gọi kèm danh sách người tham gia.
func (u *Usecase) Get(
	ctx context.Context,
	actor *domainauth.Actor,
	callID uuid.UUID,
) (*domaincall.Call, error) {
	c, err := u.load(ctx, actor, callID)
	if err != nil {
		return nil, err
	}
	c.Participants, err = u.participants.ListForCall(ctx, c.ID)
	if err != nil {
		return nil, apperror.Internal(err)
	}
	return c, nil
}

// Live trả về cuộc gọi đang diễn ra của một hội thoại, hoặc nil.
//
// Trả nil thay vì 404 khi không có: "hội thoại này đang không có cuộc gọi"
// là một câu trả lời hợp lệ, không phải lỗi. Giao diện gọi hàm này mỗi lần
// mở hội thoại để biết có nên hiện nút "Tham gia" không.
func (u *Usecase) Live(
	ctx context.Context,
	actor *domainauth.Actor,
	conversationID uuid.UUID,
) (*domaincall.Call, error) {
	if actor == nil {
		return nil, apperror.New(apperror.KindUnauthorized, "Chưa xác thực")
	}
	if err := u.requireMember(ctx, conversationID, actor.EmployeeID); err != nil {
		return nil, err
	}

	c, err := u.calls.LiveInConversation(ctx, conversationID)
	if err != nil {
		return nil, nil //nolint:nilerr // không có cuộc gọi là câu trả lời hợp lệ
	}
	c.Participants, _ = u.participants.ListForCall(ctx, c.ID)
	return c, nil
}

// History trả về lịch sử cuộc gọi của một hội thoại, mới nhất trước.
func (u *Usecase) History(
	ctx context.Context,
	actor *domainauth.Actor,
	conversationID uuid.UUID,
	limit int,
) ([]*domaincall.Call, error) {
	if actor == nil {
		return nil, apperror.New(apperror.KindUnauthorized, "Chưa xác thực")
	}
	if err := u.requireMember(ctx, conversationID, actor.EmployeeID); err != nil {
		return nil, err
	}

	if limit <= 0 || limit > maxHistoryRows {
		limit = maxHistoryRows
	}

	list, err := u.calls.ListForConversation(ctx, conversationID, limit)
	if err != nil {
		return nil, apperror.Internal(err)
	}
	return list, nil
}

// =========================================================================
// HẠ TẦNG MEDIA
// =========================================================================

// ICEServers cấp danh sách STUN/TURN kèm credential ngắn hạn.
//
// Endpoint này cần đăng nhập vì credential TURN cho phép relay băng thông
// thật qua máy chủ. Mở công khai là mời người lạ dùng chùa, và không có
// cách nào thu hồi ngoài đổi khoá cho tất cả mọi người.
func (u *Usecase) ICEServers(
	ctx context.Context,
	actor *domainauth.Actor,
) ([]domaincall.ICEServer, error) {
	if actor == nil {
		return nil, apperror.New(apperror.KindUnauthorized, "Chưa xác thực")
	}
	if u.media == nil {
		return nil, apperror.New(apperror.KindUnprocessable,
			"Chức năng gọi chưa được cấu hình. Liên hệ bộ phận kỹ thuật.")
	}

	servers, err := u.media.ICEServers(ctx,
		actor.EmployeeID.String(), domaincall.ICECredentialTTL)
	if err != nil {
		return nil, apperror.Internal(err)
	}
	return servers, nil
}

// Token cấp lại access token vào phòng cho người đã ở trong cuộc gọi.
//
// Cần thiết vì token có TTL ngắn: một cuộc họp dài hơn TokenTTL sẽ phải
// xin lại, và người dùng không được thấy cuộc gọi rớt chỉ vì token hết hạn.
func (u *Usecase) Token(
	ctx context.Context,
	actor *domainauth.Actor,
	callID uuid.UUID,
) (string, error) {
	c, err := u.load(ctx, actor, callID)
	if err != nil {
		return "", err
	}
	if c.Status.Final() {
		return "", apperror.Conflict("Cuộc gọi đã kết thúc")
	}

	names, _ := u.employees.NamesOf(ctx, []uuid.UUID{actor.EmployeeID})
	return u.issueToken(ctx, c, actor.EmployeeID, names[actor.EmployeeID])
}

// =========================================================================
// CHUYỂN TIẾP SIGNALING
// =========================================================================

// RelaySignal chuyển tiếp một bản tin SDP hoặc ICE tới đúng một người.
//
// Hub gọi hàm này khi nhận call.sdp hoặc call.ice từ client. Usecase KHÔNG
// đọc nội dung `Data`: đó là SDP hoặc ICE candidate do trình duyệt sinh ra,
// và máy chủ không có lý do gì để hiểu chúng. Việc duy nhất ở đây là kiểm
// tra quyền rồi chuyển đi.
//
// Kiểm quyền là bắt buộc dù nội dung không đọc được: thiếu nó thì bất kỳ ai
// cũng bơm ICE candidate vào cuộc gọi của người khác, và cách hỏng rõ nhất
// là chèn được một luồng media lạ vào cuộc họp.
func (u *Usecase) RelaySignal(
	ctx context.Context,
	actor *domainauth.Actor,
	eventType string,
	p domainrealtime.CallSignalPayload,
) error {
	if eventType != domainrealtime.TypeCallSDP &&
		eventType != domainrealtime.TypeCallICE {
		return apperror.Invalid("Loại bản tin signaling không hợp lệ", nil)
	}
	if p.To == uuid.Nil {
		return apperror.Invalid("Thiếu người nhận", nil)
	}

	c, err := u.load(ctx, actor, p.CallID)
	if err != nil {
		return err
	}
	if c.Status.Final() {
		return apperror.Conflict("Cuộc gọi đã kết thúc")
	}

	// Người nhận cũng phải là thành viên hội thoại. Không kiểm thì một
	// thành viên hợp lệ vẫn dùng được đường này để bắn bản tin tới người
	// ngoài cuộc.
	if err := u.requireMember(ctx, c.ConversationID, p.To); err != nil {
		return apperror.NotFound("người nhận")
	}

	// Ghi đè From bằng danh tính THẬT của người gửi, không tin giá trị
	// client gửi lên. Tin vào nó là để một người mạo danh người khác trong
	// lúc thương lượng kết nối.
	p.From = actor.EmployeeID

	u.signal.PushCall(ctx, []uuid.UUID{p.To}, eventType, p)
	return nil
}
