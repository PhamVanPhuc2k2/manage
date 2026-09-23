// Package call là tầng nghiệp vụ của module gọi video và gọi thoại.
//
// Trách nhiệm: quyết định ai được gọi ai, cuộc gọi đi qua những trạng thái
// nào, và ai nhận được bản tin nào. Việc truyền media nằm hoàn toàn ngoài
// package này — trình duyệt nói chuyện thẳng với nhau hoặc với SFU.
package call

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	domaincall "github.com/PhamVanPhuc2k2/manage/internal/domain/call"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
	"github.com/PhamVanPhuc2k2/manage/pkg/logger"
	"github.com/PhamVanPhuc2k2/manage/pkg/metrics"
)

// maxHistoryRows là trần số cuộc gọi trả về khi xem lịch sử.
const maxHistoryRows = 100

// Clock cho phép thay đồng hồ trong test.
type Clock interface{ Now() time.Time }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

type Usecase struct {
	calls        domaincall.Repository
	participants domaincall.ParticipantRepository

	conversations domaincall.ConversationLookup
	employees     domaincall.EmployeeLookup
	messenger     domaincall.SystemMessenger

	media  domaincall.MediaServer
	signal domaincall.Signaler

	clock Clock
}

func NewUsecase(
	calls domaincall.Repository,
	participants domaincall.ParticipantRepository,
	conversations domaincall.ConversationLookup,
	employees domaincall.EmployeeLookup,
	messenger domaincall.SystemMessenger,
	media domaincall.MediaServer,
	signal domaincall.Signaler,
) *Usecase {
	return &Usecase{
		calls:         calls,
		participants:  participants,
		conversations: conversations,
		employees:     employees,
		messenger:     messenger,
		media:         media,
		signal:        signal,
		clock:         systemClock{},
	}
}

func (u *Usecase) SetClock(c Clock) { u.clock = c }

// =========================================================================
// PHÂN QUYỀN
// =========================================================================

// requireMember kiểm tra actor có trong hội thoại không.
//
// Đây là hàng rào phân quyền DUY NHẤT của module này. Middleware chỉ biết
// "người này có quyền call:start", nó không biết họ định gọi vào hội thoại
// nào — bỏ bước này là bất kỳ ai cũng gọi chen vào cuộc họp của phòng khác.
//
// Trả 404 chứ không 403: xác nhận "hội thoại này có tồn tại nhưng bạn không
// ở trong đó" đã là rò rỉ. Cùng nguyên tắc với chat và nhân sự.
func (u *Usecase) requireMember(
	ctx context.Context,
	conversationID, employeeID uuid.UUID,
) error {
	ok, err := u.conversations.IsMember(ctx, conversationID, employeeID)
	if err != nil {
		return apperror.Internal(err)
	}
	if !ok {
		return apperror.NotFound("hội thoại")
	}
	return nil
}

// notFoundOr đổi lỗi kho dữ liệu thành lỗi HTTP, giấu chi tiết nội bộ.
func notFoundOr(err error) error {
	if errors.Is(err, domaincall.ErrNotFound) {
		return apperror.NotFound("cuộc gọi")
	}
	return apperror.Internal(err)
}

// =========================================================================
// BẮT ĐẦU CUỘC GỌI
// =========================================================================

// StartResult là thứ người gọi nhận được ngay sau khi bấm nút.
type StartResult struct {
	Call *domaincall.Call `json:"call"`
	// Token vào phòng SFU, cấp luôn để người gọi không phải gọi thêm một
	// lượt nữa — mỗi lượt gọi thêm là thêm một khoảng lặng trước khi thấy
	// hình mình.
	Token string `json:"token"`
	// Ringing là những người thật sự đang được đổ chuông.
	Ringing []uuid.UUID `json:"ringing"`
	// Busy là những người bị bỏ qua vì đang trong cuộc gọi khác.
	Busy []uuid.UUID `json:"busy,omitempty"`
}

// Start mở một cuộc gọi trong hội thoại.
func (u *Usecase) Start(
	ctx context.Context,
	actor *domainauth.Actor,
	conversationID uuid.UUID,
	kind domaincall.Kind,
) (*StartResult, error) {
	if actor == nil {
		return nil, apperror.New(apperror.KindUnauthorized, "Chưa xác thực")
	}
	if !kind.Valid() {
		return nil, apperror.Invalid("Loại cuộc gọi không hợp lệ", nil)
	}
	if err := u.requireMember(ctx, conversationID, actor.EmployeeID); err != nil {
		return nil, err
	}

	// Đã có cuộc gọi đang chạy thì THAM GIA nó, không mở cuộc mới.
	//
	// Người bấm nút gọi trong lúc đồng nghiệp vừa mở cuộc gọi sẽ mong vào
	// đúng cuộc đó. Tạo cuộc thứ hai là cách chắc chắn để hai người ngồi ở
	// hai phòng trống.
	if live, err := u.calls.LiveInConversation(ctx, conversationID); err == nil {
		return u.joinExisting(ctx, actor, live)
	} else if !errors.Is(err, domaincall.ErrNotFound) {
		return nil, apperror.Internal(err)
	}

	members, err := u.conversations.MemberIDs(ctx, conversationID)
	if err != nil {
		return nil, apperror.Internal(err)
	}
	if len(members) < 2 {
		return nil, apperror.Invalid(
			"Hội thoại chưa có ai khác để gọi", nil)
	}

	now := u.clock.Now()
	c := &domaincall.Call{
		ID:             uuid.New(),
		ConversationID: conversationID,
		InitiatorID:    &actor.EmployeeID,
		Kind:           kind,
		Status:         domaincall.StatusRinging,
		StartedAt:      now,
	}
	c.RoomName = domaincall.RoomNameFor(c.ID)

	if err := u.calls.Create(ctx, c); err != nil {
		return nil, apperror.Internal(err)
	}
	metrics.CallsStarted.WithLabelValues(string(kind)).Inc()

	// Ghi người được mời TRƯỚC khi đổ chuông: một cuộc gọi nhỡ vẫn phải
	// hiện ra với đúng những người đã bị gọi, kể cả khi không ai bắt máy.
	if err := u.participants.Invite(ctx, c.ID, members); err != nil {
		return nil, apperror.Internal(err)
	}
	if err := u.participants.Join(ctx, c.ID, actor.EmployeeID, now); err != nil {
		return nil, apperror.Internal(err)
	}

	names, _ := u.employees.NamesOf(ctx, []uuid.UUID{actor.EmployeeID})
	token, err := u.issueToken(ctx, c, actor.EmployeeID, names[actor.EmployeeID])
	if err != nil {
		return nil, err
	}

	ringing, busy := u.splitByAvailability(ctx, members, actor.EmployeeID)

	u.signal.PushCall(ctx, ringing, domaincall.EventIncoming, incomingPayload{
		CallID:         c.ID,
		ConversationID: conversationID,
		Kind:           kind,
		InitiatorID:    actor.EmployeeID,
		InitiatorName:  names[actor.EmployeeID],
		ExpiresAt:      now.Add(domaincall.RingTimeout),
	})

	// Báo cho chính người gọi biết đầu kia đang đổ chuông, kèm danh sách
	// người bận. Không có bước này thì họ nhìn màn hình im lặng và không
	// biết hệ thống đã làm gì.
	u.signal.PushCall(ctx, []uuid.UUID{actor.EmployeeID}, domaincall.EventRinging,
		ringingPayload{CallID: c.ID, Ringing: ringing, Busy: busy})

	c.Participants, _ = u.participants.ListForCall(ctx, c.ID)

	return &StartResult{Call: c, Token: token, Ringing: ringing, Busy: busy}, nil
}

// splitByAvailability chia thành viên thành nhóm đổ chuông và nhóm đang bận.
//
// Người đang trong một cuộc gọi khác bị bỏ qua thay vì đổ chuông — đây là
// quyết định "từ chối ngay khi bận" đã chốt trước khi code. Lỗi khi tra cứu
// được coi là RẢNH: thà làm phiền một người đang bận còn hơn im lặng không
// gọi được ai vì Redis chớp một nhịp.
func (u *Usecase) splitByAvailability(
	ctx context.Context,
	members []uuid.UUID,
	caller uuid.UUID,
) (ringing, busy []uuid.UUID) {
	log := logger.FromContext(ctx)

	for _, id := range members {
		if id == caller {
			continue
		}
		other, err := u.calls.LiveForEmployee(ctx, id)
		switch {
		case err == nil && other != nil:
			busy = append(busy, id)
		case err != nil && !errors.Is(err, domaincall.ErrNotFound):
			log.Warn().Err(err).Str("employee_id", id.String()).
				Msg("không tra được trạng thái bận, coi như rảnh")
			ringing = append(ringing, id)
		default:
			ringing = append(ringing, id)
		}
	}
	return ringing, busy
}

// joinExisting đưa người bấm gọi vào cuộc gọi đang chạy sẵn.
func (u *Usecase) joinExisting(
	ctx context.Context,
	actor *domainauth.Actor,
	c *domaincall.Call,
) (*StartResult, error) {
	now := u.clock.Now()
	if err := u.participants.Join(ctx, c.ID, actor.EmployeeID, now); err != nil {
		return nil, apperror.Internal(err)
	}

	names, _ := u.employees.NamesOf(ctx, []uuid.UUID{actor.EmployeeID})
	token, err := u.issueToken(ctx, c, actor.EmployeeID, names[actor.EmployeeID])
	if err != nil {
		return nil, err
	}

	u.announceParticipant(ctx, c, actor.EmployeeID, names[actor.EmployeeID], true)
	c.Participants, _ = u.participants.ListForCall(ctx, c.ID)

	return &StartResult{Call: c, Token: token}, nil
}

// issueToken cấp access token vào phòng SFU.
func (u *Usecase) issueToken(
	ctx context.Context,
	c *domaincall.Call,
	employeeID uuid.UUID,
	name string,
) (string, error) {
	if u.media == nil {
		return "", apperror.New(apperror.KindUnprocessable,
			"Chức năng gọi chưa được cấu hình. Liên hệ bộ phận kỹ thuật.")
	}
	if name == "" {
		name = "Thành viên"
	}

	token, err := u.media.IssueToken(ctx, c.RoomName, employeeID.String(), name,
		true, domaincall.TokenTTL)
	if err != nil {
		return "", apperror.Internal(err)
	}
	return token, nil
}
