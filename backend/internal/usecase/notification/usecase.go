// Package notification là tầng nghiệp vụ của module thông báo.
//
// Nó có hai mặt: một mặt hướng vào trong, cho các module khác gọi để TẠO
// thông báo; một mặt hướng ra ngoài, cho người dùng ĐỌC thông báo của chính
// mình. Mặt đọc khoá cứng theo actor, không có đường nào đọc của người khác.
package notification

import (
	"context"
	"time"

	"github.com/google/uuid"

	domainnotif "github.com/PhamVanPhuc2k2/manage/internal/domain/notification"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
	"github.com/PhamVanPhuc2k2/manage/pkg/logger"
)

const (
	defaultLimit = 20
	maxLimit     = 100
)

type Usecase struct {
	repo     domainnotif.Repository
	pusher   domainnotif.Pusher
	presence domainnotif.PresenceReader
	emails   domainnotif.EmailLookup
	mailer   domainnotif.Mailer
}

func NewUsecase(
	repo domainnotif.Repository,
	pusher domainnotif.Pusher,
	presence domainnotif.PresenceReader,
	emails domainnotif.EmailLookup,
	mailer domainnotif.Mailer,
) *Usecase {
	return &Usecase{repo: repo, pusher: pusher, presence: presence, emails: emails, mailer: mailer}
}

// =========================================================================
// TẠO — mặt hướng vào trong
// =========================================================================

// Create ghi thông báo cho danh sách người nhận rồi đẩy realtime.
//
// Thứ tự GHI TRƯỚC, ĐẨY SAU là có chủ ý: đẩy trước rồi ghi hỏng sẽ để lại một
// thông báo hiện trên màn hình mà tải lại trang là mất, và người dùng không
// có cách nào tìm lại nó.
func (u *Usecase) Create(ctx context.Context, req domainnotif.Request) error {
	if !req.Type.Valid() {
		return apperror.Invalid("loại thông báo không hợp lệ", nil)
	}

	recipients := u.filterRecipients(ctx, req)
	if len(recipients) == 0 {
		return nil
	}

	items := make([]*domainnotif.Notification, 0, len(recipients))
	for _, id := range recipients {
		items = append(items, &domainnotif.Notification{
			EmployeeID: id,
			Type:       req.Type,
			Title:      req.Title,
			Body:       req.Body,
			Link:       req.Link,
			ActorID:    req.ActorID,
			Resource:   req.Resource,
			ResourceID: req.ResourceID,
		})
	}

	if err := u.repo.CreateMany(ctx, items); err != nil {
		return err
	}

	u.push(ctx, items)
	return nil
}

// filterRecipients bỏ người tự gây ra sự kiện và người đã tắt loại này.
//
// Tự báo cho chính mình là nhiễu thuần tuý: người vừa bấm "giao việc" đã biết
// mình vừa giao việc. Lọc ở đây, một chỗ, thay vì bắt mọi module gọi phải nhớ.
func (u *Usecase) filterRecipients(ctx context.Context, req domainnotif.Request) []uuid.UUID {
	log := logger.FromContext(ctx)

	uniq := make(map[uuid.UUID]struct{}, len(req.Recipients))
	ids := make([]uuid.UUID, 0, len(req.Recipients))
	for _, id := range req.Recipients {
		if id == uuid.Nil {
			continue
		}
		if req.ActorID != nil && *req.ActorID == id {
			continue
		}
		if _, dup := uniq[id]; dup {
			continue
		}
		uniq[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 || !req.Type.Mutable() {
		return ids
	}

	muted, err := u.repo.MutedByMany(ctx, ids, req.Type)
	if err != nil {
		// Không chặn việc tạo thông báo vì đọc được cấu hình hay không.
		// Gửi thừa cho người đã tắt còn hơn nuốt mất một thông báo thật.
		log.Warn().Err(err).Msg("không đọc được cấu hình tắt thông báo")
		return ids
	}

	out := ids[:0]
	for _, id := range ids {
		if !muted[id] {
			out = append(out, id)
		}
	}
	return out
}

// push đẩy thông báo và số chưa đọc mới tới từng người.
//
// Gửi kèm số chưa đọc thay vì để client tự hỏi lại: chuông cần con số đó ngay,
// và một lượt gọi API cho mỗi thông báo mới sẽ nhân số request lên theo số
// người nhận.
func (u *Usecase) push(ctx context.Context, items []*domainnotif.Notification) {
	if u.pusher == nil {
		return
	}

	for _, n := range items {
		u.pusher.PushNotification(ctx, []uuid.UUID{n.EmployeeID}, toView(n))

		unread, err := u.repo.CountUnread(ctx, n.EmployeeID)
		if err != nil {
			continue
		}
		u.pusher.PushBadge(ctx, n.EmployeeID, unread)
	}
}

// NotifyNewMessage hiện thực chat.Notifier.
//
// Nằm ở đây chứ không ở module chat: chỉ module thông báo mới biết ai đã tắt
// loại "tin nhắn mới", và chỉ nó mới biết cách ghi vào bảng notifications.
func (u *Usecase) NotifyNewMessage(
	ctx context.Context,
	recipients []uuid.UUID,
	senderID uuid.UUID,
	senderName, conversationName, preview, link string,
) error {
	title := senderName
	if conversationName != "" {
		title = senderName + " — " + conversationName
	}

	return u.Create(ctx, domainnotif.Request{
		Recipients: recipients,
		Type:       domainnotif.TypeNewMessage,
		Title:      title,
		Body:       preview,
		Link:       link,
		ActorID:    &senderID,
		Resource:   "conversation",
	})
}

// =========================================================================
// ĐỌC — mặt hướng ra ngoài
// =========================================================================

// List trả về thông báo của CHÍNH actor. EmployeeID không nhận từ request.
func (u *Usecase) List(
	ctx context.Context,
	employeeID uuid.UUID,
	unreadOnly bool,
	typ *domainnotif.Type,
	before *time.Time,
	limit int,
) ([]*View, error) {
	if limit <= 0 || limit > maxLimit {
		limit = defaultLimit
	}

	items, err := u.repo.List(ctx, domainnotif.Filter{
		EmployeeID: employeeID,
		UnreadOnly: unreadOnly,
		Type:       typ,
		Before:     before,
		Limit:      limit,
	})
	if err != nil {
		return nil, err
	}

	out := make([]*View, 0, len(items))
	for _, n := range items {
		out = append(out, toView(n))
	}
	return out, nil
}

func (u *Usecase) Summary(ctx context.Context, employeeID uuid.UUID) (domainnotif.Summary, error) {
	n, err := u.repo.CountUnread(ctx, employeeID)
	if err != nil {
		return domainnotif.Summary{}, err
	}
	return domainnotif.Summary{Unread: n}, nil
}

func (u *Usecase) MarkRead(
	ctx context.Context,
	employeeID uuid.UUID,
	ids []uuid.UUID,
) (domainnotif.Summary, error) {
	if _, err := u.repo.MarkRead(ctx, employeeID, ids); err != nil {
		return domainnotif.Summary{}, err
	}
	return u.afterRead(ctx, employeeID)
}

func (u *Usecase) MarkAllRead(
	ctx context.Context,
	employeeID uuid.UUID,
) (domainnotif.Summary, error) {
	if _, err := u.repo.MarkAllRead(ctx, employeeID); err != nil {
		return domainnotif.Summary{}, err
	}
	return u.afterRead(ctx, employeeID)
}

// afterRead đếm lại và đẩy số mới sang các phiên khác của cùng người.
//
// Người dùng hay mở hệ thống ở nhiều tab. Đọc ở tab này mà chuông tab kia vẫn
// đỏ là lỗi nhìn thấy ngay, và đẩy qua WebSocket là cách duy nhất sửa được mà
// không bắt mọi tab phải hỏi lại liên tục.
func (u *Usecase) afterRead(
	ctx context.Context,
	employeeID uuid.UUID,
) (domainnotif.Summary, error) {
	unread, err := u.repo.CountUnread(ctx, employeeID)
	if err != nil {
		return domainnotif.Summary{}, err
	}
	if u.pusher != nil {
		u.pusher.PushBadge(ctx, employeeID, unread)
	}
	return domainnotif.Summary{Unread: unread}, nil
}

// =========================================================================
// CẤU HÌNH
// =========================================================================

// PrefItem là một dòng trên màn hình cấu hình nhận thông báo.
type PrefItem struct {
	Type    domainnotif.Type `json:"type"`
	Label   string           `json:"label"`
	Enabled bool             `json:"enabled"`
	Locked  bool             `json:"locked"`
}

// Preferences trả về TOÀN BỘ danh mục loại, kèm trạng thái bật/tắt.
//
// Trả cả danh mục chứ không chỉ những loại đã tắt: màn hình cấu hình cần vẽ
// đủ các dòng, và để client tự giữ danh sách loại là cách chắc chắn để nó
// lệch với server sau lần thêm loại tiếp theo.
func (u *Usecase) Preferences(ctx context.Context, employeeID uuid.UUID) ([]PrefItem, error) {
	muted, err := u.repo.ListMuted(ctx, employeeID)
	if err != nil {
		return nil, err
	}

	off := make(map[domainnotif.Type]bool, len(muted))
	for _, t := range muted {
		off[t] = true
	}

	all := domainnotif.AllTypes()
	out := make([]PrefItem, 0, len(all))
	for _, t := range all {
		out = append(out, PrefItem{
			Type:    t,
			Label:   t.Label(),
			Enabled: !off[t],
			Locked:  !t.Mutable(),
		})
	}
	return out, nil
}

func (u *Usecase) SetPreference(
	ctx context.Context,
	employeeID uuid.UUID,
	t domainnotif.Type,
	enabled bool,
) error {
	if !t.Valid() {
		return apperror.Invalid("loại thông báo không hợp lệ", nil)
	}
	if !t.Mutable() && !enabled {
		return apperror.Invalid("loại thông báo này không thể tắt", nil)
	}
	return u.repo.SetMuted(ctx, employeeID, t, !enabled)
}

// =========================================================================
// NHẮC QUA EMAIL
// =========================================================================

// EmailDelay là thời gian chờ trước khi nhắc qua email.
//
// Đủ dài để người đang online kịp thấy và đọc thông báo trong ứng dụng, đủ
// ngắn để việc nhắc còn kịp có ích.
const EmailDelay = 15 * time.Minute

// emailBatch giới hạn số email mỗi lần chạy.
//
// Có trần để một sự cố (ví dụ giao việc hàng loạt) không biến thành vài nghìn
// email trong một phút — đủ để hộp thư của cả công ty bị nhà cung cấp chặn.
const emailBatch = 100

// SendEmailReminders là thân của job nhắc, worker gọi định kỳ.
//
// Trả về số email đã đẩy vào hàng đợi, để log nói được điều gì đó có ích thay
// vì chỉ "đã chạy".
func (u *Usecase) SendEmailReminders(ctx context.Context) (int, error) {
	if u.mailer == nil || u.emails == nil {
		return 0, nil
	}
	log := logger.FromContext(ctx)

	pending, err := u.repo.PendingEmail(ctx, time.Now().Add(-EmailDelay), emailBatch)
	if err != nil {
		return 0, err
	}
	if len(pending) == 0 {
		return 0, nil
	}

	// Chỉ nhắc những loại quan trọng. Những loại còn lại vẫn được đánh dấu
	// đã xử lý để job không nhặt lại chúng ở mọi lần chạy về sau.
	var (
		important []*domainnotif.Notification
		skipped   []uuid.UUID
	)
	for _, n := range pending {
		if n.Type.Important() {
			important = append(important, n)
		} else {
			skipped = append(skipped, n.ID)
		}
	}

	if err := u.repo.MarkEmailed(ctx, skipped); err != nil {
		log.Warn().Err(err).Msg("không đánh dấu được thông báo bỏ qua email")
	}
	if len(important) == 0 {
		return 0, nil
	}

	targets := u.offlineOnly(ctx, important)
	if len(targets) == 0 {
		return 0, nil
	}

	ids := make([]uuid.UUID, 0, len(targets))
	for _, n := range targets {
		ids = append(ids, n.EmployeeID)
	}
	addrs, err := u.emails.EmailsOf(ctx, ids)
	if err != nil {
		return 0, err
	}

	var (
		sent []uuid.UUID
		n    int
	)
	for _, item := range targets {
		email := addrs[item.EmployeeID]
		if email == "" {
			// Không có email thì cũng đánh dấu, nếu không job sẽ thử lại mãi.
			sent = append(sent, item.ID)
			continue
		}

		if err := u.mailer.SendNotification(ctx, email, "", item.Title, item.Body, item.Link); err != nil {
			log.Warn().Err(err).Str("notification_id", item.ID.String()).
				Msg("không đẩy được email nhắc vào hàng đợi")
			continue
		}
		sent = append(sent, item.ID)
		n++
	}

	// Đánh dấu ngay sau khi đẩy vào hàng đợi, không chờ gửi xong: worker gửi
	// mail có cơ chế thử lại riêng, còn ở đây thử lại nghĩa là email trùng.
	if err := u.repo.MarkEmailed(ctx, sent); err != nil {
		return n, err
	}
	return n, nil
}

// offlineOnly bỏ những người đang online.
//
// Họ đã thấy thông báo trên màn hình rồi. Một email thừa là một lý do để người
// ta lập bộ lọc bỏ qua mọi email từ hệ thống — và khi đó cả những email thật
// sự cần đọc cũng không ai đọc.
func (u *Usecase) offlineOnly(
	ctx context.Context,
	items []*domainnotif.Notification,
) []*domainnotif.Notification {
	if u.presence == nil {
		return items
	}
	log := logger.FromContext(ctx)

	ids := make([]uuid.UUID, 0, len(items))
	for _, n := range items {
		ids = append(ids, n.EmployeeID)
	}

	status, err := u.presence.StatusOf(ctx, ids)
	if err != nil {
		log.Warn().Err(err).Msg("không đọc được trạng thái online, vẫn gửi email nhắc")
		return items
	}

	out := items[:0]
	for _, n := range items {
		if status[n.EmployeeID] == "" || status[n.EmployeeID] == "offline" {
			out = append(out, n)
		}
	}
	return out
}
