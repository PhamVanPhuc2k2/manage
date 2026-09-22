package auth

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/PhamVanPhuc2k2/manage/pkg/hash"
)

// Bộ kiểm thử đổi và đặt lại mật khẩu.
//
// Hai thao tác này là nơi một tài khoản bị chiếm được giành lại — hoặc bị
// mất hẳn. Luật quan trọng nhất ở đây không phải là độ mạnh mật khẩu mà là
// CẮT PHIÊN: người ta đổi mật khẩu vì nghi bị lộ, và nếu phiên cũ vẫn sống
// thì việc đổi mật khẩu chẳng thay đổi được gì.

// =========================================================================
// ĐỘ MẠNH MẬT KHẨU
// =========================================================================

// TestValidatePassword khoá lại một quyết định thiết kế có chủ ý: KHÔNG bắt
// buộc ký tự đặc biệt.
//
// Quy tắc phức tạp khiến người dùng chọn kiểu "Password1!" — dễ đoán hơn
// một cụm từ dài. Phép thử này tồn tại để ai đó sau này không "siết thêm
// cho an toàn" mà không biết vì sao nó đang như vậy.
func TestValidatePassword(t *testing.T) {
	cases := []struct {
		name string
		in   string
		ok   bool
	}{
		{"đủ dài, có chữ và số", "MatKhau2026", true},
		{"cụm từ dài không ký tự đặc biệt", "concho chay qua duong 2026", true},
		{"đúng 10 ký tự", "MatKhau123", true},
		{"tiếng Việt có dấu đếm theo rune", "Mật khẩu số 1", true},
		{"quá ngắn", "Ngan123", false},
		{"chỉ có chữ", "matkhaukhongso", false},
		{"chỉ có số", "1234567890", false},
		{"rỗng", "", false},
		{"quá 200 ký tự", strings.Repeat("a1", 101), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePassword(tc.in)
			if tc.ok && err != nil {
				t.Errorf("mật khẩu hợp lệ bị từ chối: %v", err)
			}
			if !tc.ok && err == nil {
				t.Error("mật khẩu không hợp lệ được chấp nhận")
			}
		})
	}
}

// =========================================================================
// ĐỔI MẬT KHẨU
// =========================================================================

func TestChangePassword(t *testing.T) {
	h := newAuthHarness(t, false)
	u := h.seedUser(t, "a@test.local", testPassword)

	newPassword := "MatKhauMoi2026"
	if err := h.uc.ChangePassword(
		context.Background(), u.ID, uuid.New(), testPassword, newPassword,
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	stored := h.users.passwordUpdates[u.ID]
	if stored == "" {
		t.Fatal("chưa ghi mật khẩu mới")
	}
	if err := hash.Verify(stored, newPassword); err != nil {
		t.Error("mật khẩu đã lưu không khớp với mật khẩu mới")
	}
}

func TestChangePasswordRejectsWrongCurrent(t *testing.T) {
	h := newAuthHarness(t, false)
	u := h.seedUser(t, "a@test.local", testPassword)

	err := h.uc.ChangePassword(context.Background(), u.ID, uuid.New(),
		"SaiHoanToan2026", "MatKhauMoi2026")

	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
	if len(h.users.passwordUpdates) != 0 {
		t.Error("đã đổi mật khẩu dù mật khẩu hiện tại sai")
	}
}

func TestChangePasswordRejectsWeakAndUnchanged(t *testing.T) {
	cases := []struct {
		name string
		next string
	}{
		{"mật khẩu mới quá yếu", "ngan"},
		{"trùng mật khẩu cũ", testPassword},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newAuthHarness(t, false)
			u := h.seedUser(t, "a@test.local", testPassword)

			err := h.uc.ChangePassword(
				context.Background(), u.ID, uuid.New(), testPassword, tc.next)

			if got := statusOf(err); got != http.StatusBadRequest {
				t.Errorf("mã lỗi = %d, muốn 400", got)
			}
			if len(h.users.passwordUpdates) != 0 {
				t.Error("đã đổi mật khẩu dù đầu vào không hợp lệ")
			}
		})
	}
}

// TestChangePasswordKillsOtherSessionsButNotThisOne.
//
// Người ta đổi mật khẩu vì nghi bị lộ — không cắt phiên cũ thì kẻ tấn công
// vẫn đang đăng nhập và việc đổi mật khẩu chẳng thay đổi gì. Nhưng phiên
// HIỆN TẠI phải được giữ, nếu không người dùng bị đá ra ngay sau khi tự
// đổi mật khẩu của mình.
func TestChangePasswordKillsOtherSessionsButNotThisOne(t *testing.T) {
	h := newAuthHarness(t, false)
	h.seedUser(t, "a@test.local", testPassword)

	// Ba thiết bị đang đăng nhập.
	first := h.login(t, "a@test.local")
	h.login(t, "a@test.local")
	h.login(t, "a@test.local")

	claims, err := h.jwt.Verify(first.AccessToken)
	if err != nil {
		t.Fatalf("đọc token lỗi: %v", err)
	}

	sessions, err := h.uc.ListSessions(context.Background(), claims.UserID)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if len(sessions) != 3 {
		t.Fatalf("số phiên trước khi đổi = %d, muốn 3", len(sessions))
	}

	if err := h.uc.ChangePassword(context.Background(),
		claims.UserID, claims.SessionID, testPassword, "MatKhauMoi2026",
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	left, err := h.uc.ListSessions(context.Background(), claims.UserID)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if len(left) != 1 {
		t.Fatalf("số phiên còn lại = %d, muốn 1", len(left))
	}
	if left[0].ID != claims.SessionID {
		t.Error("phiên còn lại không phải phiên hiện tại")
	}
}

func TestChangePasswordOfUnknownUserIsUnauthorized(t *testing.T) {
	h := newAuthHarness(t, false)

	err := h.uc.ChangePassword(context.Background(), uuid.New(), uuid.New(),
		testPassword, "MatKhauMoi2026")

	if got := statusOf(err); got != http.StatusUnauthorized {
		t.Errorf("mã lỗi = %d, muốn 401", got)
	}
}

// =========================================================================
// QUÊN MẬT KHẨU
// =========================================================================

// TestForgotPasswordIsSilentAboutUnknownEmails.
//
// LUÔN trả nil dù email có tồn tại hay không. Trả lỗi khi email không tồn
// tại là biến endpoint này thành công cụ dò danh sách email nhân viên — ai
// cũng gọi được, không cần đăng nhập.
func TestForgotPasswordIsSilentAboutUnknownEmails(t *testing.T) {
	h := newAuthHarness(t, false)
	h.seedUser(t, "a@test.local", testPassword)

	if err := h.uc.ForgotPassword(
		context.Background(), "khongtontai@test.local",
	); err != nil {
		t.Errorf("email không tồn tại phải trả nil, nhận: %v", err)
	}
	if len(h.mailer.resetURLs) != 0 {
		t.Error("đã gửi mail cho email không tồn tại")
	}
}

// TestForgotPasswordIsSilentAboutDisabledAccounts: cùng lý do — phân biệt
// được "tài khoản bị khoá" với "không tồn tại" cũng là rò rỉ.
func TestForgotPasswordIsSilentAboutDisabledAccounts(t *testing.T) {
	h := newAuthHarness(t, false)
	u := h.seedUser(t, "a@test.local", testPassword)
	u.IsActive = false

	if err := h.uc.ForgotPassword(
		context.Background(), "a@test.local",
	); err != nil {
		t.Errorf("tài khoản bị khoá phải trả nil, nhận: %v", err)
	}
	if len(h.mailer.resetURLs) != 0 {
		t.Error("đã gửi mail cho tài khoản bị khoá")
	}
}

// TestForgotPasswordSendsUsableLink: liên kết phải chứa token đã mã hoá URL
// và trỏ đúng vào miền công khai đã cấu hình.
func TestForgotPasswordSendsUsableLink(t *testing.T) {
	h := newAuthHarness(t, false)
	h.seedUser(t, "a@test.local", testPassword)

	if err := h.uc.ForgotPassword(
		context.Background(), "a@test.local",
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if len(h.mailer.resetURLs) != 1 {
		t.Fatalf("số mail đã gửi = %d, muốn 1", len(h.mailer.resetURLs))
	}

	link := h.mailer.resetURLs[0]
	if !strings.HasPrefix(link, "https://manage.test/reset-password?token=") {
		t.Errorf("liên kết = %q, sai tiền tố", link)
	}

	parsed, err := url.Parse(link)
	if err != nil {
		t.Fatalf("liên kết không phân tích được: %v", err)
	}
	if parsed.Query().Get("token") == "" {
		t.Error("liên kết không mang token")
	}
	if len(h.reset.saved) != 1 {
		t.Errorf("số token đã lưu = %d, muốn 1", len(h.reset.saved))
	}
}

// TestForgotPasswordReportsMailFailure.
//
// Khác với các luồng khác, lỗi gửi mail Ở ĐÂY phải báo ra: mail là cách duy
// nhất người dùng nhận được liên kết, nên nuốt lỗi nghĩa là họ ngồi chờ
// mãi một thư không bao giờ tới.
func TestForgotPasswordReportsMailFailure(t *testing.T) {
	h := newAuthHarness(t, false)
	h.seedUser(t, "a@test.local", testPassword)
	h.mailer.err = errors.New("SMTP sập")

	err := h.uc.ForgotPassword(context.Background(), "a@test.local")
	if err == nil {
		t.Error("phải báo lỗi khi không gửi được mail đặt lại mật khẩu")
	}
}

// =========================================================================
// ĐẶT LẠI MẬT KHẨU
// =========================================================================

// resetTokenFrom lấy token thô từ liên kết vừa gửi trong mail.
func resetTokenFrom(t *testing.T, link string) string {
	t.Helper()

	parsed, err := url.Parse(link)
	if err != nil {
		t.Fatalf("liên kết không phân tích được: %v", err)
	}
	raw := parsed.Query().Get("token")
	if raw == "" {
		t.Fatal("liên kết không mang token")
	}
	return raw
}

func TestResetPassword(t *testing.T) {
	h := newAuthHarness(t, false)
	u := h.seedUser(t, "a@test.local", testPassword)

	if err := h.uc.ForgotPassword(
		context.Background(), "a@test.local",
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	raw := resetTokenFrom(t, h.mailer.resetURLs[0])

	newPassword := "MatKhauMoi2026"
	if err := h.uc.ResetPassword(
		context.Background(), raw, newPassword,
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	stored := h.users.passwordUpdates[u.ID]
	if err := hash.Verify(stored, newPassword); err != nil {
		t.Error("mật khẩu đã lưu không khớp với mật khẩu mới")
	}
}

// TestResetTokenIsSingleUse.
//
// Consume xoá token ngay trong một thao tác nguyên khối, nên hai request
// đến cùng lúc chỉ có một request dùng được. Không có luật này thì một liên
// kết rò ra ngoài dùng lại được mãi mãi.
func TestResetTokenIsSingleUse(t *testing.T) {
	h := newAuthHarness(t, false)
	h.seedUser(t, "a@test.local", testPassword)

	if err := h.uc.ForgotPassword(
		context.Background(), "a@test.local",
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	raw := resetTokenFrom(t, h.mailer.resetURLs[0])

	if err := h.uc.ResetPassword(
		context.Background(), raw, "MatKhauMoi2026",
	); err != nil {
		t.Fatalf("lần một phải thành công: %v", err)
	}

	err := h.uc.ResetPassword(context.Background(), raw, "MatKhauKhac2026")
	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi lần hai = %d, muốn 400", got)
	}
}

func TestResetPasswordRejectsUnknownToken(t *testing.T) {
	h := newAuthHarness(t, false)

	err := h.uc.ResetPassword(
		context.Background(), "token-bia-ra", "MatKhauMoi2026")

	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
}

// TestResetPasswordValidatesBeforeConsumingToken.
//
// Kiểm tra độ mạnh TRƯỚC khi tiêu token. Ngược lại thì người dùng gõ một
// mật khẩu quá ngắn là mất luôn liên kết, phải xin lại từ đầu — và họ sẽ
// nghĩ hệ thống hỏng.
func TestResetPasswordValidatesBeforeConsumingToken(t *testing.T) {
	h := newAuthHarness(t, false)
	h.seedUser(t, "a@test.local", testPassword)

	if err := h.uc.ForgotPassword(
		context.Background(), "a@test.local",
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	raw := resetTokenFrom(t, h.mailer.resetURLs[0])

	if err := h.uc.ResetPassword(
		context.Background(), raw, "ngan",
	); err == nil {
		t.Fatal("mật khẩu quá yếu phải bị từ chối")
	}

	// Token vẫn dùng được.
	if err := h.uc.ResetPassword(
		context.Background(), raw, "MatKhauMoi2026",
	); err != nil {
		t.Errorf("token đã bị tiêu mất dù mật khẩu không hợp lệ: %v", err)
	}
}

// TestResetPasswordKillsEverySession.
//
// Khác với ChangePassword: ở đây người dùng chưa đăng nhập nên không có
// phiên nào cần giữ. Và nếu tài khoản đang bị chiếm thì mọi phiên hiện có
// đều đáng ngờ.
func TestResetPasswordKillsEverySession(t *testing.T) {
	h := newAuthHarness(t, false)
	h.seedUser(t, "a@test.local", testPassword)

	pair := h.login(t, "a@test.local")
	h.login(t, "a@test.local")

	claims, err := h.jwt.Verify(pair.AccessToken)
	if err != nil {
		t.Fatalf("đọc token lỗi: %v", err)
	}

	if err := h.uc.ForgotPassword(
		context.Background(), "a@test.local",
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	raw := resetTokenFrom(t, h.mailer.resetURLs[0])

	if err := h.uc.ResetPassword(
		context.Background(), raw, "MatKhauMoi2026",
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	left, err := h.uc.ListSessions(context.Background(), claims.UserID)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if len(left) != 0 {
		t.Errorf("số phiên còn lại = %d, muốn 0", len(left))
	}
}

// =========================================================================
// THÔNG TIN NGƯỜI DÙNG
// =========================================================================

// TestMeReadsFromDatabaseNotToken.
//
// Token cố ý chỉ mang id, vai trò và quyền. Nhét thêm email và họ tên vào
// sẽ làm token phình ra, và thông tin trong đó cũ đi ngay khi người dùng
// sửa hồ sơ.
func TestMeReadsFromDatabaseNotToken(t *testing.T) {
	h := newAuthHarness(t, false)
	u := h.seedUser(t, "a@test.local", testPassword)

	// Sửa hồ sơ SAU khi token đã cấp.
	u.EmployeeName = "Nguyễn Văn A"

	got, err := h.uc.Me(context.Background(), u.ID)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if got.EmployeeName != "Nguyễn Văn A" {
		t.Errorf("họ tên = %q, muốn giá trị mới nhất từ database",
			got.EmployeeName)
	}
}

func TestMeOfUnknownUserIsUnauthorized(t *testing.T) {
	h := newAuthHarness(t, false)

	_, err := h.uc.Me(context.Background(), uuid.New())
	if got := statusOf(err); got != http.StatusUnauthorized {
		t.Errorf("mã lỗi = %d, muốn 401", got)
	}
}
