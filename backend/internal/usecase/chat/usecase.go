// Package chat là tầng nghiệp vụ của module chat.
//
// Nguyên tắc xuyên suốt: quyền thô (chat:read, chat:create) chỉ trả lời "người
// này có được dùng chat không". Câu "người này có được đụng vào hội thoại NÀY
// không" do bảng conversation_members trả lời, và mọi thao tác ở đây đều hỏi
// nó trước.
package chat

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	domainchat "github.com/PhamVanPhuc2k2/manage/internal/domain/chat"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
)

const (
	// TTL của URL ký cho tệp đính kèm.
	//
	// URL tải ngắn hơn URL tải lên vì nó nằm trong dữ liệu trả về của API và
	// dễ bị chuyển tiếp; URL tải lên chỉ sống trong một thao tác của người gửi.
	uploadURLTTL = 15 * time.Minute
	viewURLTTL   = 30 * time.Minute

	maxAttachmentsPerMessage = 10
	maxAttachmentSize        = 25 << 20 // 25MB
)

// CompanyLookup là cổng lấy công ty hiện tại.
type CompanyLookup interface {
	CurrentCompanyID(ctx context.Context) (uuid.UUID, error)
}

type Usecase struct {
	convs    domainchat.ConversationRepository
	msgs     domainchat.MessageRepository
	employee domainchat.EmployeeLookup
	sources  domainchat.SourceLister
	presence domainchat.PresenceReader
	pusher   domainchat.Pusher
	notifier domainchat.Notifier
	storage  domainchat.Storage
	company  CompanyLookup
}

func NewUsecase(
	convs domainchat.ConversationRepository,
	msgs domainchat.MessageRepository,
	employee domainchat.EmployeeLookup,
	sources domainchat.SourceLister,
	presence domainchat.PresenceReader,
	pusher domainchat.Pusher,
	notifier domainchat.Notifier,
	storage domainchat.Storage,
	company CompanyLookup,
) *Usecase {
	return &Usecase{
		convs:    convs,
		msgs:     msgs,
		employee: employee,
		sources:  sources,
		presence: presence,
		pusher:   pusher,
		notifier: notifier,
		storage:  storage,
		company:  company,
	}
}

// requireMember là cửa kiểm soát truy cập của TOÀN module.
//
// Trả về 404 chứ không phải 403 khi người này không ở trong hội thoại: 403
// xác nhận rằng hội thoại đó có tồn tại, và với chat thì chính sự tồn tại của
// một cuộc trò chuyện đã là thông tin không nên rò ra.
func (u *Usecase) requireMember(
	ctx context.Context,
	conversationID, employeeID uuid.UUID,
) (*domainchat.Member, error) {
	m, err := u.convs.Member(ctx, conversationID, employeeID)
	if err != nil {
		if errors.Is(err, domainchat.ErrNotMember) || errors.Is(err, domainchat.ErrNotFound) {
			return nil, apperror.NotFound("hội thoại")
		}
		return nil, err
	}
	return m, nil
}

// requireAdmin dùng cho các thao tác quản trị nhóm.
func (u *Usecase) requireAdmin(
	ctx context.Context,
	conversationID, employeeID uuid.UUID,
) (*domainchat.Conversation, error) {
	c, err := u.convs.ByID(ctx, conversationID, employeeID)
	if err != nil {
		return nil, notFoundOr(err)
	}
	if c.Kind.Managed() {
		return nil, apperror.Invalid(
			"nhóm theo phòng ban hoặc dự án do hệ thống quản lý, không sửa thành viên được", nil)
	}
	if !c.IsAdmin {
		return nil, apperror.Forbidden("chỉ quản trị nhóm mới làm được việc này")
	}
	return c, nil
}

func notFoundOr(err error) error {
	if errors.Is(err, domainchat.ErrNotFound) || errors.Is(err, domainchat.ErrNotMember) {
		return apperror.NotFound("hội thoại")
	}
	return err
}

// fillStatus gắn trạng thái online cho danh sách nhân viên.
//
// Lỗi đọc presence KHÔNG làm hỏng lời gọi: chấm xanh là thông tin phụ, còn
// danh sách hội thoại là thông tin chính. Không có Redis thì mọi người hiện
// offline, và đó vẫn là một màn hình dùng được.
func (u *Usecase) statusOf(ctx context.Context, ids []uuid.UUID) map[uuid.UUID]string {
	if u.presence == nil || len(ids) == 0 {
		return map[uuid.UUID]string{}
	}
	st, err := u.presence.StatusOf(ctx, ids)
	if err != nil {
		return map[uuid.UUID]string{}
	}
	return st
}
