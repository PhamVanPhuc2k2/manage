package call

import (
	"context"
	"errors"

	"github.com/google/uuid"

	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	domaincall "github.com/PhamVanPhuc2k2/manage/internal/domain/call"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
	"github.com/PhamVanPhuc2k2/manage/pkg/logger"
)

// Phần còn lại của vòng đời cuộc gọi: bắt máy, từ chối, rời phòng, kết thúc.

// =========================================================================
// BẮT MÁY
// =========================================================================

// AcceptResult là thứ người bắt máy nhận được.
type AcceptResult struct {
	Call  *domaincall.Call `json:"call"`
	Token string           `json:"token"`
}

// Accept: người nhận bấm nghe.
func (u *Usecase) Accept(
	ctx context.Context,
	actor *domainauth.Actor,
	callID uuid.UUID,
) (*AcceptResult, error) {
	c, err := u.load(ctx, actor, callID)
	if err != nil {
		return nil, err
	}
	if c.Status.Final() {
		return nil, apperror.Conflict("Cuộc gọi đã kết thúc")
	}

	now := u.clock.Now()

	// Chuyển ringing → active. Bỏ qua lỗi nếu người khác đã chuyển trước:
	// trong cuộc gọi nhóm, người thứ hai bắt máy không làm gì sai cả.
	if c.Status == domaincall.StatusRinging {
		if err := u.calls.UpdateStatus(ctx, c.ID,
			domaincall.StatusRinging, domaincall.StatusActive, ""); err != nil &&
			!errors.Is(err, domaincall.ErrNotFound) {
			return nil, apperror.Internal(err)
		}
		c.Status = domaincall.StatusActive
	}

	if err := u.participants.Join(ctx, c.ID, actor.EmployeeID, now); err != nil {
		return nil, apperror.Internal(err)
	}

	names, _ := u.employees.NamesOf(ctx, []uuid.UUID{actor.EmployeeID})
	token, err := u.issueToken(ctx, c, actor.EmployeeID, names[actor.EmployeeID])
	if err != nil {
		return nil, err
	}

	// Báo cho mọi người trong hội thoại rằng đã có người nghe.
	u.broadcast(ctx, c, domaincall.EventAccepted, statusPayload{
		CallID: c.ID,
		Status: domaincall.StatusActive,
		By:     actor.EmployeeID,
	})

	// Tắt chuông ở CÁC THIẾT BỊ CÒN LẠI của chính người vừa bắt máy.
	//
	// Đây là phần hay bị quên nhất của tính năng gọi: không có nó thì điện
	// thoại vẫn reo trong túi sau khi người ta đã nghe trên máy tính. Hub
	// gửi tới mọi kết nối của một nhân viên, nên chính thiết bị vừa bắt máy
	// cũng nhận — client tự bỏ qua vì nó biết mình là người đã accept.
	u.signal.PushCall(ctx, []uuid.UUID{actor.EmployeeID},
		domaincall.EventCancelled, statusPayload{
			CallID: c.ID,
			Status: domaincall.StatusActive,
			By:     actor.EmployeeID,
			Reason: "answered_elsewhere",
		})

	c.Participants, _ = u.participants.ListForCall(ctx, c.ID)
	return &AcceptResult{Call: c, Token: token}, nil
}

// =========================================================================
// TỪ CHỐI
// =========================================================================

// Reject: người nhận bấm từ chối.
//
// Với hội thoại nhóm, một người từ chối KHÔNG kết thúc cuộc gọi — những
// người khác vẫn đang đổ chuông. Chỉ khi hội thoại 1-1 thì từ chối mới là
// kết thúc, và điều đó được suy ra từ việc không còn ai đổ chuông nữa.
func (u *Usecase) Reject(
	ctx context.Context,
	actor *domainauth.Actor,
	callID uuid.UUID,
) error {
	c, err := u.load(ctx, actor, callID)
	if err != nil {
		return err
	}
	if c.Status.Final() {
		return nil // đã kết thúc rồi, bấm từ chối lúc này không phải lỗi
	}

	now := u.clock.Now()
	if err := u.participants.Leave(ctx, c.ID, actor.EmployeeID, now); err != nil {
		return apperror.Internal(err)
	}

	u.broadcast(ctx, c, domaincall.EventRejected, statusPayload{
		CallID: c.ID,
		Status: c.Status,
		By:     actor.EmployeeID,
		Reason: domaincall.ReasonRejected,
	})

	// Tắt chuông ở các thiết bị còn lại của người vừa từ chối.
	u.signal.PushCall(ctx, []uuid.UUID{actor.EmployeeID},
		domaincall.EventCancelled, statusPayload{
			CallID: c.ID,
			By:     actor.EmployeeID,
			Reason: "rejected_elsewhere",
		})

	// Không còn ai trong phòng và không ai còn đổ chuông thì kết thúc hẳn.
	return u.endIfDeserted(ctx, c, domaincall.StatusRejected, domaincall.ReasonRejected)
}

// =========================================================================
// RỜI PHÒNG VÀ KẾT THÚC
// =========================================================================

// Leave: một người rời cuộc gọi.
//
// Khác Reject ở chỗ người này đã từng ở trong phòng. Cuộc gọi chỉ kết thúc
// khi người CUỐI CÙNG rời đi — trong cuộc họp sáu người, một người tắt máy
// không được làm năm người còn lại văng ra.
func (u *Usecase) Leave(
	ctx context.Context,
	actor *domainauth.Actor,
	callID uuid.UUID,
) error {
	c, err := u.load(ctx, actor, callID)
	if err != nil {
		return err
	}
	if c.Status.Final() {
		return nil
	}

	now := u.clock.Now()
	if err := u.participants.Leave(ctx, c.ID, actor.EmployeeID, now); err != nil {
		return apperror.Internal(err)
	}

	names, _ := u.employees.NamesOf(ctx, []uuid.UUID{actor.EmployeeID})
	u.announceParticipant(ctx, c, actor.EmployeeID, names[actor.EmployeeID], false)

	return u.endIfDeserted(ctx, c, domaincall.StatusEnded, domaincall.ReasonHangup)
}

// End kết thúc cuộc gọi cho TẤT CẢ mọi người.
//
// Tách khỏi Leave vì hai việc khác nhau: rời phòng là "tôi xong rồi", kết
// thúc là "cuộc họp này xong rồi". Chỉ người khởi tạo được kết thúc thay
// mọi người; người khác chỉ rời được.
func (u *Usecase) End(
	ctx context.Context,
	actor *domainauth.Actor,
	callID uuid.UUID,
) error {
	c, err := u.load(ctx, actor, callID)
	if err != nil {
		return err
	}
	if c.Status.Final() {
		return nil
	}
	if c.InitiatorID == nil || *c.InitiatorID != actor.EmployeeID {
		return apperror.Forbidden(
			"Chỉ người bắt đầu cuộc gọi mới kết thúc được cho tất cả. " +
				"Bạn có thể rời cuộc gọi.")
	}

	next := domaincall.StatusEnded
	reason := domaincall.ReasonHangup
	// Chưa ai bắt máy mà người gọi cúp: đó là huỷ, không phải kết thúc.
	// Phân biệt này đi thẳng vào câu hiển thị trong khung chat.
	if c.Status == domaincall.StatusRinging {
		next = domaincall.StatusCancelled
		reason = domaincall.ReasonHangup
	}

	return u.finish(ctx, c, next, reason, actor.EmployeeID)
}

// endIfDeserted kết thúc cuộc gọi khi nó không còn là một cuộc gọi nữa.
//
// ĐIỀU KIỆN KHÔNG PHẢI "PHÒNG RỖNG"
//
// Một người ngồi một mình trong phòng không phải một cuộc gọi. Gọi 1-1 mà
// đầu kia từ chối thì người gọi VẪN đang trong phòng — nếu chỉ hỏi "phòng
// có rỗng không" thì cuộc gọi treo mãi ở 'ringing', và chỉ mục một phần
// khiến hội thoại đó không gọi được nữa.
//
// Luật đúng: cuộc gọi còn sống khi có TỪ HAI người trong phòng, HOẶC còn
// ít nhất một người trong phòng và một người chưa bắt máy (vẫn có thể vào).
func (u *Usecase) endIfDeserted(
	ctx context.Context,
	c *domaincall.Call,
	status domaincall.Status,
	reason string,
) error {
	inRoom, pending, err := u.participants.RoomState(ctx, c.ID)
	if err != nil {
		return apperror.Internal(err)
	}
	if inRoom >= 2 || (inRoom >= 1 && pending >= 1) {
		return nil
	}
	return u.finish(ctx, c, status, reason, uuid.Nil)
}

// finish là đường DUY NHẤT kết thúc một cuộc gọi.
//
// Gom vào một chỗ vì có sáu đường dẫn tới đây (cúp máy, từ chối, hết giờ,
// phòng rỗng, mất mạng, lỗi media) và mỗi đường đều phải làm đủ bốn việc:
// đổi trạng thái, đóng phòng SFU, báo cho mọi người, ghi tin nhắn hệ thống.
// Tách ra thì sẽ có đường quên một việc, và cái quên thường xuyên nhất là
// đóng phòng — phòng rác trên SFU không gây lỗi nào, chỉ âm thầm ăn tài
// nguyên.
func (u *Usecase) finish(
	ctx context.Context,
	c *domaincall.Call,
	status domaincall.Status,
	reason string,
	by uuid.UUID,
) error {
	log := logger.FromContext(ctx)

	if err := u.calls.UpdateStatus(ctx, c.ID, c.Status, status, reason); err != nil {
		if errors.Is(err, domaincall.ErrNotFound) {
			// Người khác vừa kết thúc trước. Không phải lỗi: hai người cùng
			// bấm cúp máy là chuyện bình thường.
			return nil
		}
		return apperror.Internal(err)
	}

	now := u.clock.Now()
	c.Status = status
	c.EndedAt = &now
	c.EndReason = reason

	// Đóng phòng SFU. Lỗi ở đây không chặn: cuộc gọi đã kết thúc về mặt
	// nghiệp vụ, và LiveKit tự dọn phòng rỗng sau một lúc.
	if u.media != nil {
		if err := u.media.CloseRoom(ctx, c.RoomName); err != nil {
			log.Warn().Err(err).Str("room", c.RoomName).
				Msg("không đóng được phòng trên SFU")
		}
	}

	u.broadcast(ctx, c, domaincall.EventEnded, statusPayload{
		CallID:          c.ID,
		Status:          status,
		By:              by,
		Reason:          reason,
		DurationSeconds: int(c.Duration().Seconds()),
	})

	// Ghi tin nhắn hệ thống vào hội thoại.
	//
	// Nuốt lỗi có chủ ý: cuộc gọi đã kết thúc đúng, và không ghi được một
	// dòng tóm tắt không đáng để báo lỗi cho người dùng.
	if u.messenger != nil {
		if err := u.messenger.PostSystem(ctx, c.ConversationID, c.SystemMessage()); err != nil {
			log.Warn().Err(err).Str("call_id", c.ID.String()).
				Msg("không ghi được tin nhắn hệ thống cho cuộc gọi")
		}
	}

	log.Info().
		Str("call_id", c.ID.String()).
		Str("status", string(status)).
		Str("reason", reason).
		Int("duration_seconds", int(c.Duration().Seconds())).
		Msg("cuộc gọi kết thúc")

	return nil
}

// =========================================================================
// HẾT GIỜ ĐỔ CHUÔNG
// =========================================================================

// ExpireRinging đánh dấu nhỡ cho mọi cuộc gọi đổ chuông quá hạn.
//
// Chạy định kỳ ở worker. Cần thiết dù client tự tắt chuông theo ExpiresAt:
// client có thể đã đóng tab, và một cuộc gọi kẹt ở 'ringing' vĩnh viễn sẽ
// chặn mọi cuộc gọi sau trong cùng hội thoại — chỉ mục một phần không cho
// hai cuộc cùng sống.
func (u *Usecase) ExpireRinging(ctx context.Context) (int, error) {
	cutoff := u.clock.Now().Add(-domaincall.RingTimeout)

	expired, err := u.calls.ExpireRinging(ctx, cutoff)
	if err != nil {
		return 0, apperror.Internal(err)
	}

	log := logger.FromContext(ctx)
	for _, c := range expired {
		now := u.clock.Now()
		c.EndedAt = &now
		c.Status = domaincall.StatusMissed
		c.EndReason = domaincall.ReasonTimeout

		if u.media != nil {
			if err := u.media.CloseRoom(ctx, c.RoomName); err != nil {
				log.Warn().Err(err).Str("room", c.RoomName).
					Msg("không đóng được phòng của cuộc gọi nhỡ")
			}
		}

		u.broadcast(ctx, c, domaincall.EventCancelled, statusPayload{
			CallID: c.ID,
			Status: domaincall.StatusMissed,
			Reason: domaincall.ReasonTimeout,
		})

		if u.messenger != nil {
			_ = u.messenger.PostSystem(ctx, c.ConversationID, c.SystemMessage())
		}
	}

	return len(expired), nil
}

// =========================================================================
// MẤT KẾT NỐI SIGNALING
// =========================================================================

// HandleDisconnect xử lý việc một người rớt kết nối WebSocket.
//
// Gọi từ hub khi kết nối cuối cùng của một nhân viên đóng. Không có bước
// này thì một người đóng laptop giữa cuộc gọi sẽ để cuộc gọi treo mãi ở
// 'active', và hội thoại đó không gọi được nữa.
//
// Không trả lỗi: đây là đường dọn dẹp chạy trong lúc kết nối đang đóng, và
// không có ai để báo lỗi cho.
func (u *Usecase) HandleDisconnect(ctx context.Context, employeeID uuid.UUID) {
	log := logger.FromContext(ctx)

	c, err := u.calls.LiveForEmployee(ctx, employeeID)
	if err != nil {
		if !errors.Is(err, domaincall.ErrNotFound) {
			log.Warn().Err(err).Msg("không tra được cuộc gọi đang chạy khi ngắt kết nối")
		}
		return
	}

	now := u.clock.Now()
	if err := u.participants.Leave(ctx, c.ID, employeeID, now); err != nil {
		log.Warn().Err(err).Msg("không ghi được việc rời phòng khi ngắt kết nối")
		return
	}

	names, _ := u.employees.NamesOf(ctx, []uuid.UUID{employeeID})
	u.announceParticipant(ctx, c, employeeID, names[employeeID], false)

	if err := u.endIfDeserted(ctx, c, domaincall.StatusEnded, domaincall.ReasonNetwork); err != nil {
		log.Warn().Err(err).Msg("không kết thúc được cuộc gọi bỏ trống")
	}
}

// =========================================================================
// TIỆN ÍCH
// =========================================================================

// load đọc cuộc gọi và kiểm tra actor có quyền đụng vào nó.
func (u *Usecase) load(
	ctx context.Context,
	actor *domainauth.Actor,
	callID uuid.UUID,
) (*domaincall.Call, error) {
	if actor == nil {
		return nil, apperror.New(apperror.KindUnauthorized, "Chưa xác thực")
	}

	c, err := u.calls.GetByID(ctx, callID)
	if err != nil {
		return nil, notFoundOr(err)
	}
	// Quyền nằm ở HỘI THOẠI chứa cuộc gọi, không ở bản thân cuộc gọi. Bỏ
	// bước này là ai biết id cuộc gọi cũng chen vào được.
	if err := u.requireMember(ctx, c.ConversationID, actor.EmployeeID); err != nil {
		return nil, apperror.NotFound("cuộc gọi")
	}
	return c, nil
}

// broadcast gửi một sự kiện tới mọi thành viên hội thoại.
func (u *Usecase) broadcast(
	ctx context.Context,
	c *domaincall.Call,
	event string,
	payload any,
) {
	if u.signal == nil {
		return
	}
	members, err := u.conversations.MemberIDs(ctx, c.ConversationID)
	if err != nil {
		log := logger.FromContext(ctx)
		log.Warn().Err(err).Msg("không lấy được người nhận sự kiện cuộc gọi")
		return
	}
	u.signal.PushCall(ctx, members, event, payload)
}

// announceParticipant báo có người vào hoặc rời phòng.
func (u *Usecase) announceParticipant(
	ctx context.Context,
	c *domaincall.Call,
	employeeID uuid.UUID,
	name string,
	joined bool,
) {
	u.broadcast(ctx, c, domaincall.EventParticipant, participantPayload{
		CallID:     c.ID,
		EmployeeID: employeeID,
		Name:       name,
		Joined:     joined,
	})
}
