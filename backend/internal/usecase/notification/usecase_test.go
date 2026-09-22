package notification

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	domainnotif "github.com/PhamVanPhuc2k2/manage/internal/domain/notification"
)

// =========================================================================
// GIẢ LẬP
// =========================================================================

type fakeRepo struct {
	created []*domainnotif.Notification
	unread  int

	muted   map[uuid.UUID]bool
	mutedBy func(t domainnotif.Type) (map[uuid.UUID]bool, error)

	pending []*domainnotif.Notification
	emailed []uuid.UUID
}

func (f *fakeRepo) CreateMany(_ context.Context, items []*domainnotif.Notification) error {
	for _, n := range items {
		n.ID = uuid.New()
		n.CreatedAt = time.Now()
		f.created = append(f.created, n)
	}
	return nil
}

func (f *fakeRepo) List(context.Context, domainnotif.Filter) ([]*domainnotif.Notification, error) {
	return nil, nil
}

func (f *fakeRepo) CountUnread(context.Context, uuid.UUID) (int, error) {
	return f.unread, nil
}

func (f *fakeRepo) MarkRead(context.Context, uuid.UUID, []uuid.UUID) (int64, error) {
	return 0, nil
}

func (f *fakeRepo) MarkAllRead(context.Context, uuid.UUID) (int64, error) { return 0, nil }

func (f *fakeRepo) ListMuted(context.Context, uuid.UUID) ([]domainnotif.Type, error) {
	return nil, nil
}

func (f *fakeRepo) SetMuted(context.Context, uuid.UUID, domainnotif.Type, bool) error {
	return nil
}

func (f *fakeRepo) MutedByMany(
	_ context.Context,
	_ []uuid.UUID,
	t domainnotif.Type,
) (map[uuid.UUID]bool, error) {
	if f.mutedBy != nil {
		return f.mutedBy(t)
	}
	return f.muted, nil
}

func (f *fakeRepo) PendingEmail(
	context.Context, time.Time, int,
) ([]*domainnotif.Notification, error) {
	return f.pending, nil
}

func (f *fakeRepo) MarkEmailed(_ context.Context, ids []uuid.UUID) error {
	f.emailed = append(f.emailed, ids...)
	return nil
}

type fakePusher struct {
	pushed  []uuid.UUID
	badges  map[uuid.UUID]int
	payload []any
}

func (f *fakePusher) PushNotification(_ context.Context, recipients []uuid.UUID, p any) {
	f.pushed = append(f.pushed, recipients...)
	f.payload = append(f.payload, p)
}

func (f *fakePusher) PushBadge(_ context.Context, employeeID uuid.UUID, unread int) {
	if f.badges == nil {
		f.badges = map[uuid.UUID]int{}
	}
	f.badges[employeeID] = unread
}

type fakePresence struct {
	status map[uuid.UUID]string
	err    error
}

func (f *fakePresence) StatusOf(
	context.Context, []uuid.UUID,
) (map[uuid.UUID]string, error) {
	return f.status, f.err
}

type fakeEmails struct{ addrs map[uuid.UUID]string }

func (f *fakeEmails) EmailsOf(
	context.Context, []uuid.UUID,
) (map[uuid.UUID]string, error) {
	return f.addrs, nil
}

type fakeMailer struct {
	sent []string
	err  error
}

func (f *fakeMailer) SendNotification(
	_ context.Context, email, _, _, _, _ string,
) error {
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, email)
	return nil
}

func newUC() (*Usecase, *fakeRepo, *fakePusher, *fakePresence, *fakeMailer) {
	repo := &fakeRepo{}
	pusher := &fakePusher{}
	presence := &fakePresence{}
	mailer := &fakeMailer{}
	emails := &fakeEmails{addrs: map[uuid.UUID]string{}}

	return NewUsecase(repo, pusher, presence, emails, mailer),
		repo, pusher, presence, mailer
}

// =========================================================================
// LỌC NGƯỜI NHẬN
// =========================================================================

// TestCreateExcludesActor: người vừa bấm "giao việc" đã biết mình vừa giao
// việc. Tự báo cho chính mình là nhiễu thuần tuý.
func TestCreateExcludesActor(t *testing.T) {
	uc, repo, _, _, _ := newUC()

	actor, other := uuid.New(), uuid.New()
	err := uc.Create(context.Background(), domainnotif.Request{
		Recipients: []uuid.UUID{actor, other},
		Type:       domainnotif.TypeTaskAssigned,
		Title:      "Được giao việc",
		ActorID:    &actor,
	})
	if err != nil {
		t.Fatalf("Create lỗi: %v", err)
	}

	if len(repo.created) != 1 {
		t.Fatalf("muốn 1 thông báo, được %d", len(repo.created))
	}
	if repo.created[0].EmployeeID != other {
		t.Error("thông báo phải thuộc về người KHÁC người gây ra sự kiện")
	}
}

func TestCreateDeduplicatesRecipients(t *testing.T) {
	uc, repo, _, _, _ := newUC()

	dup := uuid.New()
	err := uc.Create(context.Background(), domainnotif.Request{
		// Trùng lặp là chuyện thường: một người vừa được giao việc vừa được
		// nhắc tên trong cùng một mô tả.
		Recipients: []uuid.UUID{dup, dup, dup, uuid.Nil},
		Type:       domainnotif.TypeTaskMentioned,
		Title:      "Được nhắc tên",
	})
	if err != nil {
		t.Fatalf("Create lỗi: %v", err)
	}
	if len(repo.created) != 1 {
		t.Errorf("muốn 1 thông báo sau khi lọc trùng, được %d", len(repo.created))
	}
}

func TestCreateSkipsMutedRecipients(t *testing.T) {
	uc, repo, _, _, _ := newUC()

	quiet, noisy := uuid.New(), uuid.New()
	repo.muted = map[uuid.UUID]bool{quiet: true}

	err := uc.Create(context.Background(), domainnotif.Request{
		Recipients: []uuid.UUID{quiet, noisy},
		Type:       domainnotif.TypeNewMessage, // loại tắt được
		Title:      "Tin nhắn mới",
	})
	if err != nil {
		t.Fatalf("Create lỗi: %v", err)
	}
	if len(repo.created) != 1 || repo.created[0].EmployeeID != noisy {
		t.Error("người đã tắt loại này không được nhận thông báo")
	}
}

// TestCreateIgnoresMutesForLockedTypes: loại bắt buộc thì không tra cấu hình
// tắt, vì người dùng không tắt được nó ngay từ đầu. Tra thừa là một truy vấn
// vô ích ở mọi lần duyệt đơn.
func TestCreateIgnoresMutesForLockedTypes(t *testing.T) {
	uc, repo, _, _, _ := newUC()

	asked := false
	repo.mutedBy = func(domainnotif.Type) (map[uuid.UUID]bool, error) {
		asked = true
		return nil, nil
	}

	err := uc.Create(context.Background(), domainnotif.Request{
		Recipients: []uuid.UUID{uuid.New()},
		Type:       domainnotif.TypeLeaveDecided, // không tắt được
		Title:      "Đơn nghỉ phép có kết quả",
	})
	if err != nil {
		t.Fatalf("Create lỗi: %v", err)
	}
	if asked {
		t.Error("loại bắt buộc không được đi tra cấu hình tắt")
	}
	if len(repo.created) != 1 {
		t.Errorf("muốn 1 thông báo, được %d", len(repo.created))
	}
}

// TestCreateStillWritesWhenMuteLookupFails: gửi thừa cho người đã tắt còn hơn
// nuốt mất một thông báo thật.
func TestCreateStillWritesWhenMuteLookupFails(t *testing.T) {
	uc, repo, _, _, _ := newUC()

	repo.mutedBy = func(domainnotif.Type) (map[uuid.UUID]bool, error) {
		return nil, errors.New("database sập")
	}

	err := uc.Create(context.Background(), domainnotif.Request{
		Recipients: []uuid.UUID{uuid.New()},
		Type:       domainnotif.TypeNewMessage,
		Title:      "Tin nhắn mới",
	})
	if err != nil {
		t.Fatalf("lỗi đọc cấu hình không được làm hỏng việc tạo thông báo: %v", err)
	}
	if len(repo.created) != 1 {
		t.Errorf("muốn 1 thông báo, được %d", len(repo.created))
	}
}

func TestCreateRejectsUnknownType(t *testing.T) {
	uc, repo, _, _, _ := newUC()

	err := uc.Create(context.Background(), domainnotif.Request{
		Recipients: []uuid.UUID{uuid.New()},
		Type:       domainnotif.Type("khong_co_that"),
		Title:      "Lạ",
	})
	if err == nil {
		t.Error("loại không hợp lệ phải trả lỗi")
	}
	if len(repo.created) != 0 {
		t.Error("không được ghi gì khi loại không hợp lệ")
	}
}

func TestCreateWithNoRecipientsIsNoop(t *testing.T) {
	uc, repo, pusher, _, _ := newUC()

	actor := uuid.New()
	err := uc.Create(context.Background(), domainnotif.Request{
		// Chỉ có đúng người gây ra sự kiện — sau khi lọc là danh sách rỗng.
		Recipients: []uuid.UUID{actor},
		Type:       domainnotif.TypeTaskAssigned,
		Title:      "Tự giao cho mình",
		ActorID:    &actor,
	})
	if err != nil {
		t.Fatalf("Create lỗi: %v", err)
	}
	if len(repo.created) != 0 || len(pusher.pushed) != 0 {
		t.Error("danh sách người nhận rỗng thì không ghi và không đẩy gì")
	}
}

// TestCreatePushesBadgeWithNotification: gửi kèm số chưa đọc để chuông cập
// nhật ngay, thay vì để client hỏi lại bằng một request riêng cho mỗi thông
// báo mới.
func TestCreatePushesBadgeWithNotification(t *testing.T) {
	uc, repo, pusher, _, _ := newUC()
	repo.unread = 7

	recipient := uuid.New()
	if err := uc.Create(context.Background(), domainnotif.Request{
		Recipients: []uuid.UUID{recipient},
		Type:       domainnotif.TypeTaskAssigned,
		Title:      "Được giao việc",
	}); err != nil {
		t.Fatalf("Create lỗi: %v", err)
	}

	if len(pusher.pushed) != 1 || pusher.pushed[0] != recipient {
		t.Error("phải đẩy thông báo tới đúng người nhận")
	}
	if pusher.badges[recipient] != 7 {
		t.Errorf("huy hiệu = %d, muốn 7", pusher.badges[recipient])
	}
}

// TestPushPayloadIsView: bản tin đẩy đi phải là View có json tag, không phải
// entity domain.
//
// Đây chính là lỗi đã gặp ở Phase 5 với module chat: đẩy thẳng entity khiến
// client nhận {"ID":...} thay vì {"id":...} và im lặng bỏ qua. Không phép thử
// REST nào bắt được, nên khoá lại bằng một phép thử riêng.
func TestPushPayloadIsView(t *testing.T) {
	uc, _, pusher, _, _ := newUC()

	if err := uc.Create(context.Background(), domainnotif.Request{
		Recipients: []uuid.UUID{uuid.New()},
		Type:       domainnotif.TypeTaskAssigned,
		Title:      "Được giao việc",
	}); err != nil {
		t.Fatalf("Create lỗi: %v", err)
	}

	if len(pusher.payload) != 1 {
		t.Fatalf("muốn 1 payload, được %d", len(pusher.payload))
	}
	if _, ok := pusher.payload[0].(*View); !ok {
		t.Errorf("payload kiểu %T, muốn *View", pusher.payload[0])
	}
}

// =========================================================================
// CẤU HÌNH
// =========================================================================

func TestPreferencesListsEveryType(t *testing.T) {
	uc, _, _, _, _ := newUC()

	items, err := uc.Preferences(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("Preferences lỗi: %v", err)
	}

	// Trả cả danh mục chứ không chỉ những loại đã tắt: màn hình cấu hình cần
	// vẽ đủ các dòng, và để client tự giữ danh sách loại là cách chắc chắn để
	// nó lệch với server sau lần thêm loại tiếp theo.
	if len(items) != len(domainnotif.AllTypes()) {
		t.Errorf("trả %d dòng, muốn %d", len(items), len(domainnotif.AllTypes()))
	}

	locked := 0
	for _, it := range items {
		if it.Label == "" {
			t.Errorf("loại %s thiếu nhãn", it.Type)
		}
		if it.Locked {
			locked++
		}
	}
	if locked == 0 {
		t.Error("phải có ít nhất một loại bị khoá không cho tắt")
	}
}

func TestSetPreferenceRejectsDisablingLockedType(t *testing.T) {
	uc, _, _, _, _ := newUC()

	err := uc.SetPreference(
		context.Background(), uuid.New(), domainnotif.TypeLeaveDecided, false)
	if err == nil {
		t.Error("tắt loại bắt buộc phải trả lỗi")
	}

	// Nhưng BẬT nó thì được — đó là trạng thái mặc định, không phải thao tác lạ.
	if err := uc.SetPreference(
		context.Background(), uuid.New(), domainnotif.TypeLeaveDecided, true); err != nil {
		t.Errorf("bật loại bắt buộc phải được phép: %v", err)
	}
}

func TestSetPreferenceRejectsUnknownType(t *testing.T) {
	uc, _, _, _, _ := newUC()

	if err := uc.SetPreference(
		context.Background(), uuid.New(), domainnotif.Type("bịa"), false); err == nil {
		t.Error("loại không tồn tại phải trả lỗi")
	}
}

// =========================================================================
// EMAIL NHẮC
// =========================================================================

// TestEmailRemindersOnlyImportant: những loại còn lại vẫn phải được đánh dấu
// đã xử lý, nếu không job sẽ nhặt lại chúng ở mọi lần chạy về sau.
func TestEmailRemindersOnlyImportant(t *testing.T) {
	uc, repo, _, presence, mailer := newUC()

	offline := uuid.New()
	important := &domainnotif.Notification{
		ID: uuid.New(), EmployeeID: offline,
		Type: domainnotif.TypeTaskAssigned, Title: "Được giao việc",
	}
	chatter := &domainnotif.Notification{
		ID: uuid.New(), EmployeeID: offline,
		Type: domainnotif.TypeNewMessage, Title: "Tin nhắn mới",
	}
	repo.pending = []*domainnotif.Notification{important, chatter}
	presence.status = map[uuid.UUID]string{offline: "offline"}

	// Địa chỉ email của người nhận.
	uc.emails = &fakeEmails{addrs: map[uuid.UUID]string{offline: "a@test.local"}}

	n, err := uc.SendEmailReminders(context.Background())
	if err != nil {
		t.Fatalf("SendEmailReminders lỗi: %v", err)
	}
	if n != 1 {
		t.Errorf("gửi %d email, muốn 1 (chỉ loại quan trọng)", n)
	}
	if len(mailer.sent) != 1 {
		t.Fatalf("mailer nhận %d lượt, muốn 1", len(mailer.sent))
	}

	// CẢ HAI phải được đánh dấu: loại quan trọng vì đã gửi, loại còn lại vì
	// đã quyết định không gửi.
	if len(repo.emailed) != 2 {
		t.Errorf("đánh dấu %d thông báo, muốn 2", len(repo.emailed))
	}
}

// TestEmailRemindersSkipOnlineUsers: họ đã thấy thông báo trên màn hình rồi.
// Một email thừa là một lý do để người ta lập bộ lọc bỏ qua mọi email từ hệ
// thống — và khi đó cả những email cần đọc cũng không ai đọc.
func TestEmailRemindersSkipOnlineUsers(t *testing.T) {
	uc, repo, _, presence, mailer := newUC()

	online := uuid.New()
	repo.pending = []*domainnotif.Notification{{
		ID: uuid.New(), EmployeeID: online,
		Type: domainnotif.TypeTaskAssigned, Title: "Được giao việc",
	}}
	presence.status = map[uuid.UUID]string{online: "online"}
	uc.emails = &fakeEmails{addrs: map[uuid.UUID]string{online: "a@test.local"}}

	n, err := uc.SendEmailReminders(context.Background())
	if err != nil {
		t.Fatalf("SendEmailReminders lỗi: %v", err)
	}
	if n != 0 || len(mailer.sent) != 0 {
		t.Error("người đang online không được nhận email nhắc")
	}

	// KHÔNG đánh dấu đã gửi: nếu họ offline mà vẫn chưa đọc thì lần chạy sau
	// phải nhắc. Đánh dấu ở đây là nuốt mất lời nhắc vĩnh viễn.
	if len(repo.emailed) != 0 {
		t.Error("người đang online không được đánh dấu đã gửi mail")
	}
}

// TestEmailRemindersStillSendWhenPresenceFails: không đọc được Redis thì vẫn
// gửi. Im lặng không nhắc gì còn tệ hơn nhắc thừa.
func TestEmailRemindersStillSendWhenPresenceFails(t *testing.T) {
	uc, repo, _, presence, mailer := newUC()

	emp := uuid.New()
	repo.pending = []*domainnotif.Notification{{
		ID: uuid.New(), EmployeeID: emp,
		Type: domainnotif.TypePayslipReady, Title: "Phiếu lương đã sẵn sàng",
	}}
	presence.err = errors.New("redis sập")
	uc.emails = &fakeEmails{addrs: map[uuid.UUID]string{emp: "a@test.local"}}

	n, err := uc.SendEmailReminders(context.Background())
	if err != nil {
		t.Fatalf("SendEmailReminders lỗi: %v", err)
	}
	if n != 1 || len(mailer.sent) != 1 {
		t.Error("mất presence thì vẫn phải gửi email nhắc")
	}
}

// TestEmailRemindersMarkMissingAddressAsDone: người không có email vẫn phải
// được đánh dấu, nếu không job sẽ thử lại mãi mãi.
func TestEmailRemindersMarkMissingAddressAsDone(t *testing.T) {
	uc, repo, _, presence, mailer := newUC()

	emp := uuid.New()
	id := uuid.New()
	repo.pending = []*domainnotif.Notification{{
		ID: id, EmployeeID: emp,
		Type: domainnotif.TypeTaskAssigned, Title: "Được giao việc",
	}}
	presence.status = map[uuid.UUID]string{emp: "offline"}
	uc.emails = &fakeEmails{addrs: map[uuid.UUID]string{}} // không có email

	if _, err := uc.SendEmailReminders(context.Background()); err != nil {
		t.Fatalf("SendEmailReminders lỗi: %v", err)
	}
	if len(mailer.sent) != 0 {
		t.Error("không có email thì không được gọi mailer")
	}
	if len(repo.emailed) != 1 || repo.emailed[0] != id {
		t.Error("thông báo của người không có email vẫn phải được đánh dấu")
	}
}

// TestEmailRemindersDoNotMarkOnSendFailure: đẩy vào hàng đợi thất bại thì
// KHÔNG đánh dấu, để lần chạy sau thử lại.
func TestEmailRemindersDoNotMarkOnSendFailure(t *testing.T) {
	uc, repo, _, presence, mailer := newUC()

	emp := uuid.New()
	repo.pending = []*domainnotif.Notification{{
		ID: uuid.New(), EmployeeID: emp,
		Type: domainnotif.TypeTaskAssigned, Title: "Được giao việc",
	}}
	presence.status = map[uuid.UUID]string{emp: "offline"}
	uc.emails = &fakeEmails{addrs: map[uuid.UUID]string{emp: "a@test.local"}}
	mailer.err = errors.New("rabbitmq sập")

	n, err := uc.SendEmailReminders(context.Background())
	if err != nil {
		t.Fatalf("SendEmailReminders lỗi: %v", err)
	}
	if n != 0 {
		t.Errorf("gửi %d, muốn 0", n)
	}
	if len(repo.emailed) != 0 {
		t.Error("đẩy hàng đợi thất bại thì không được đánh dấu đã gửi")
	}
}

func TestEmailRemindersNothingPending(t *testing.T) {
	uc, _, _, _, mailer := newUC()

	n, err := uc.SendEmailReminders(context.Background())
	if err != nil {
		t.Fatalf("SendEmailReminders lỗi: %v", err)
	}
	if n != 0 || len(mailer.sent) != 0 {
		t.Error("không có gì chờ thì không gửi gì")
	}
}

// TestNotifyNewMessageBuildsTitle kiểm tra tiêu đề thông báo tin nhắn: với
// hội thoại 1-1 chỉ có tên người gửi, với nhóm thì kèm tên nhóm.
func TestNotifyNewMessageBuildsTitle(t *testing.T) {
	uc, repo, _, _, _ := newUC()

	sender := uuid.New()
	recipient := uuid.New()

	if err := uc.NotifyNewMessage(context.Background(),
		[]uuid.UUID{recipient}, sender, "Trần Văn A", "", "chào bạn", "/chat/x"); err != nil {
		t.Fatalf("NotifyNewMessage lỗi: %v", err)
	}
	if len(repo.created) != 1 {
		t.Fatalf("muốn 1 thông báo, được %d", len(repo.created))
	}
	if got := repo.created[0].Title; got != "Trần Văn A" {
		t.Errorf("tiêu đề 1-1 = %q, muốn \"Trần Văn A\"", got)
	}

	repo.created = nil
	if err := uc.NotifyNewMessage(context.Background(),
		[]uuid.UUID{recipient}, sender, "Trần Văn A", "Nhóm dự án", "chào cả nhóm",
		"/chat/y"); err != nil {
		t.Fatalf("NotifyNewMessage lỗi: %v", err)
	}
	if got := repo.created[0].Title; got != "Trần Văn A — Nhóm dự án" {
		t.Errorf("tiêu đề nhóm = %q, muốn \"Trần Văn A — Nhóm dự án\"", got)
	}
}
