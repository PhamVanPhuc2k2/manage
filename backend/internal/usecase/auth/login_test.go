package auth

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
)

const testPassword = "MatKhau#Manh2026"

func statusOf(err error) int {
	if err == nil {
		return http.StatusOK
	}
	code, _ := apperror.HTTPStatus(err)
	return code
}

func loginInput(email string) LoginInput {
	return LoginInput{
		Email:     email,
		Password:  testPassword,
		IP:        "10.0.0.1",
		UserAgent: "test-agent",
	}
}

// =========================================================================
// ĐĂNG NHẬP
// =========================================================================

func TestLoginSuccessWithoutOTP(t *testing.T) {
	h := newAuthHarness(t, false)
	h.seedUser(t, "a@test.local", testPassword)

	res, err := h.uc.Login(context.Background(), loginInput("a@test.local"))
	if err != nil {
		t.Fatalf("Login lỗi: %v", err)
	}
	if res.Pair == nil || res.Pair.AccessToken == "" {
		t.Fatal("không cấp access token")
	}
	if res.Pair.RefreshToken == "" {
		t.Fatal("không cấp refresh token")
	}
	if res.Challenge != nil {
		t.Error("tắt OTP thì không được tạo thử thách")
	}
}

// TestLoginWithOTPIssuesNoTokens là phép thử BẢO MẬT cốt lõi của bước hai.
//
// Mật khẩu đúng KHÔNG đồng nghĩa với đăng nhập xong. Cấp bất kỳ token nào ở
// bước một là vô hiệu hoá toàn bộ lớp xác minh thứ hai — kẻ có mật khẩu sẽ
// vào được mà không cần mã.
func TestLoginWithOTPIssuesNoTokens(t *testing.T) {
	h := newAuthHarness(t, true)
	h.seedUser(t, "a@test.local", testPassword)

	res, err := h.uc.Login(context.Background(), loginInput("a@test.local"))
	if err != nil {
		t.Fatalf("Login lỗi: %v", err)
	}

	if res.Pair != nil {
		t.Fatal("bước một KHÔNG được cấp token nào")
	}
	if res.Challenge == nil || res.Challenge.ChallengeID == "" {
		t.Fatal("không tạo thử thách OTP")
	}
	if len(h.sessions.byID) != 0 {
		t.Error("bước một không được tạo phiên nào")
	}
	if len(h.mailer.otpCodes) != 1 {
		t.Errorf("gửi %d mã OTP, muốn 1", len(h.mailer.otpCodes))
	}
}

// TestLoginSameErrorForUnknownEmailAndWrongPassword chống DÒ EMAIL.
//
// Hai trường hợp phải cho cùng một thông báo và cùng một mã lỗi. Phân biệt
// chúng là tặng kẻ tấn công một công cụ liệt kê email nhân viên của công ty.
func TestLoginSameErrorForUnknownEmailAndWrongPassword(t *testing.T) {
	h := newAuthHarness(t, false)
	h.seedUser(t, "a@test.local", testPassword)

	unknown := h.uc
	_, errUnknown := unknown.Login(context.Background(), loginInput("khong-ton-tai@test.local"))

	in := loginInput("a@test.local")
	in.Password = "sai-mat-khau"
	_, errWrong := h.uc.Login(context.Background(), in)

	if errUnknown == nil || errWrong == nil {
		t.Fatal("cả hai trường hợp đều phải lỗi")
	}
	if statusOf(errUnknown) != statusOf(errWrong) {
		t.Errorf("mã lỗi khác nhau: %d vs %d", statusOf(errUnknown), statusOf(errWrong))
	}
	if errUnknown.Error() != errWrong.Error() {
		t.Errorf("thông báo khác nhau:\n  email lạ:   %q\n  sai mật khẩu: %q",
			errUnknown.Error(), errWrong.Error())
	}
}

// TestLoginRecordsFailureForUnknownEmail: bộ đếm chặn dò phải tăng CẢ KHI
// email không tồn tại.
//
// Không tăng thì kẻ tấn công quét danh sách email thoải mái không bị chặn, và
// việc chặn chỉ có tác dụng sau khi họ đã tìm ra email đúng.
func TestLoginRecordsFailureForUnknownEmail(t *testing.T) {
	h := newAuthHarness(t, false)

	if _, err := h.uc.Login(
		context.Background(), loginInput("khong-ton-tai@test.local")); err == nil {
		t.Fatal("phải trả lỗi")
	}
	if h.throttle.failures != 1 {
		t.Errorf("ghi %d lần thất bại, muốn 1", h.throttle.failures)
	}
}

func TestLoginBlockedByThrottle(t *testing.T) {
	h := newAuthHarness(t, false)
	h.seedUser(t, "a@test.local", testPassword)
	h.throttle.wait = 5 * time.Minute

	_, err := h.uc.Login(context.Background(), loginInput("a@test.local"))
	if got := statusOf(err); got != http.StatusTooManyRequests {
		t.Errorf("mã lỗi = %d, muốn 429", got)
	}
}

// TestLoginChecksThrottleBeforeDatabase: chặn dò phải xảy ra TRƯỚC khi chạm
// database, nếu không mỗi lần thử vẫn là một truy vấn và việc chặn không giảm
// được tải lúc bị tấn công.
func TestLoginChecksThrottleBeforeDatabase(t *testing.T) {
	h := newAuthHarness(t, false)
	h.throttle.wait = time.Minute

	if _, err := h.uc.Login(
		context.Background(), loginInput("a@test.local")); err == nil {
		t.Fatal("phải bị chặn")
	}
	if len(h.throttle.checkedAt) != 1 {
		t.Errorf("kiểm tra chặn %d lần, muốn 1", len(h.throttle.checkedAt))
	}
	// Không có lần ghi thất bại nào: request đã bị chặn trước khi thử.
	if h.throttle.failures != 0 {
		t.Error("request bị chặn không được tính là một lần thử sai")
	}
}

// TestLoginChecksAccountStatusAfterPassword: kiểm tra tài khoản có bị khoá
// SAU khi xác minh mật khẩu.
//
// Kiểm tra trước sẽ để lộ trạng thái tài khoản cho người KHÔNG biết mật khẩu —
// một cách khác để liệt kê nhân viên, và còn cho biết ai vừa nghỉ việc.
func TestLoginChecksAccountStatusAfterPassword(t *testing.T) {
	h := newAuthHarness(t, false)
	u := h.seedUser(t, "a@test.local", testPassword)
	u.IsActive = false

	// Sai mật khẩu + tài khoản khoá → phải là lỗi "sai thông tin đăng nhập",
	// KHÔNG phải "tài khoản bị khoá".
	in := loginInput("a@test.local")
	in.Password = "sai-mat-khau"
	_, errWrongPass := h.uc.Login(context.Background(), in)

	// Đúng mật khẩu + tài khoản khoá → mới được nói tài khoản bị khoá.
	_, errLocked := h.uc.Login(context.Background(), loginInput("a@test.local"))

	if statusOf(errWrongPass) != http.StatusUnauthorized {
		t.Errorf("sai mật khẩu: mã lỗi = %d, muốn 401", statusOf(errWrongPass))
	}
	if statusOf(errLocked) != http.StatusForbidden {
		t.Errorf("tài khoản khoá: mã lỗi = %d, muốn 403", statusOf(errLocked))
	}
	if errWrongPass.Error() == errLocked.Error() {
		t.Error("hai thông báo phải khác nhau — nhưng chỉ sau khi đã qua mật khẩu")
	}
}

func TestLoginResetsThrottleOnSuccess(t *testing.T) {
	h := newAuthHarness(t, false)
	h.seedUser(t, "a@test.local", testPassword)

	if _, err := h.uc.Login(
		context.Background(), loginInput("a@test.local")); err != nil {
		t.Fatalf("Login lỗi: %v", err)
	}
	if h.throttle.resets != 1 {
		t.Errorf("đặt lại bộ đếm %d lần, muốn 1", h.throttle.resets)
	}
	if h.users.failedResets != 1 {
		t.Errorf("đặt lại số lần sai của tài khoản %d lần, muốn 1", h.users.failedResets)
	}
}

// =========================================================================
// XOAY VÒNG REFRESH TOKEN
// =========================================================================

// login đăng nhập rồi trả về cặp token. Dùng chung cho các phép thử refresh.
func (h *authHarness) login(t *testing.T, email string) *TokenPair {
	t.Helper()

	res, err := h.uc.Login(context.Background(), loginInput(email))
	if err != nil {
		t.Fatalf("Login lỗi: %v", err)
	}
	if res.Pair == nil {
		t.Fatal("không cấp token")
	}
	return res.Pair
}

func TestRefreshRotatesToken(t *testing.T) {
	h := newAuthHarness(t, false)
	h.seedUser(t, "a@test.local", testPassword)

	first := h.login(t, "a@test.local")

	second, err := h.uc.Refresh(
		context.Background(), first.RefreshToken, "10.0.0.1", "test-agent")
	if err != nil {
		t.Fatalf("Refresh lỗi: %v", err)
	}

	// Token MỚI phải khác token cũ. Cấp lại đúng token cũ nghĩa là không có
	// xoay vòng, và cơ chế phát hiện đánh cắp mất hết tác dụng.
	if second.RefreshToken == first.RefreshToken {
		t.Error("refresh token không được xoay vòng")
	}
	if second.AccessToken == first.AccessToken {
		t.Error("access token không được cấp lại")
	}
}

// TestRefreshReuseRevokesEverySession là phép thử chống ĐÁNH CẮP TOKEN.
//
// Refresh token dùng một lần. Một token đã tiêu xuất hiện lần nữa (ngoài thời
// gian ân hạn) chỉ có thể là ai đó đã sao chép nó. Chưa biết ai là người thật,
// nên phải cắt HẾT và bắt mọi thiết bị đăng nhập lại.
func TestRefreshReuseRevokesEverySession(t *testing.T) {
	h := newAuthHarness(t, false)
	u := h.seedUser(t, "a@test.local", testPassword)

	stolen := h.login(t, "a@test.local")

	// Chủ nhân dùng token bình thường.
	if _, err := h.uc.Refresh(
		context.Background(), stolen.RefreshToken, "10.0.0.1", "test-agent"); err != nil {
		t.Fatalf("Refresh lần đầu lỗi: %v", err)
	}

	// Đẩy thời gian ra NGOÀI thời gian ân hạn: đây mới là đánh cắp thật, không
	// phải hai tab cùng gọi.
	h.refresh.now = func() time.Time { return time.Now().Add(time.Hour) }

	// Kẻ trộm dùng lại token cũ.
	_, err := h.uc.Refresh(
		context.Background(), stolen.RefreshToken, "203.0.113.9", "ke-trom")
	if got := statusOf(err); got != http.StatusUnauthorized {
		t.Errorf("mã lỗi = %d, muốn 401", got)
	}

	// TOÀN BỘ phiên của người dùng phải bị huỷ.
	if len(h.sessions.deletedAll) != 1 || h.sessions.deletedAll[0] != u.ID {
		t.Errorf("không huỷ toàn bộ phiên của người dùng; đã gọi: %v", h.sessions.deletedAll)
	}

	// Và phải gửi email cảnh báo.
	if len(h.mailer.suspicious) != 1 {
		t.Errorf("gửi %d email cảnh báo, muốn 1", len(h.mailer.suspicious))
	}
}

// TestRefreshInGraceReusesSameSession là mặt còn lại của phép thử trên.
//
// Hai tab cùng gọi refresh là chuyện xảy ra hằng ngày. Coi đó là đánh cắp sẽ
// đá người dùng ra ngoài mỗi lần họ mở hai tab; còn cấp một phiên MỚI cho mỗi
// lần dùng lại sẽ đẻ ra phiên rác và — nghiêm trọng hơn — làm đăng xuất bị vô
// hiệu, vì phiên vừa xoá được hồi sinh dưới id khác.
func TestRefreshInGraceReusesSameSession(t *testing.T) {
	h := newAuthHarness(t, false)
	h.seedUser(t, "a@test.local", testPassword)

	first := h.login(t, "a@test.local")

	// Tab 1 refresh.
	pair1, err := h.uc.Refresh(
		context.Background(), first.RefreshToken, "10.0.0.1", "tab-1")
	if err != nil {
		t.Fatalf("tab 1 refresh lỗi: %v", err)
	}

	sessionsAfterFirst := len(h.sessions.byID)

	// Tab 2 gửi lại CÙNG token cũ, vẫn trong thời gian ân hạn.
	pair2, err := h.uc.Refresh(
		context.Background(), first.RefreshToken, "10.0.0.1", "tab-2")
	if err != nil {
		t.Fatalf("tab 2 trong ân hạn phải thành công, không bị coi là đánh cắp: %v", err)
	}
	if pair2 == nil || pair2.AccessToken == "" {
		t.Fatal("tab 2 không nhận được token")
	}

	// KHÔNG được tạo thêm phiên nào.
	if len(h.sessions.byID) != sessionsAfterFirst {
		t.Errorf("số phiên tăng từ %d lên %d — dùng lại trong ân hạn đang đẻ phiên mới",
			sessionsAfterFirst, len(h.sessions.byID))
	}

	// Và KHÔNG được huỷ toàn bộ phiên.
	if len(h.sessions.deletedAll) != 0 {
		t.Error("dùng lại trong ân hạn bị nhầm thành đánh cắp")
	}

	_ = pair1
}

// TestLogoutBeatsGracePeriod: đăng xuất phải THẮNG thời gian ân hạn.
//
// Đây là lỗi tinh vi nhất của cơ chế ân hạn: đăng xuất xong, một tab khác gửi
// lại token cũ trong vòng vài giây và có phiên mới hợp lệ — phiên vừa xoá được
// hồi sinh. Khi đó nút "đăng xuất" chỉ là trang trí.
func TestLogoutBeatsGracePeriod(t *testing.T) {
	h := newAuthHarness(t, false)
	h.seedUser(t, "a@test.local", testPassword)

	first := h.login(t, "a@test.local")

	// Tab 1 refresh → tạo phiên thay thế.
	if _, err := h.uc.Refresh(
		context.Background(), first.RefreshToken, "10.0.0.1", "tab-1"); err != nil {
		t.Fatalf("refresh lỗi: %v", err)
	}

	// Người dùng bấm đăng xuất: xoá MỌI phiên đang có.
	for id := range h.sessions.byID {
		if err := h.uc.Logout(context.Background(), id); err != nil {
			t.Fatalf("Logout lỗi: %v", err)
		}
	}

	// Tab 2 gửi lại token cũ, vẫn trong ân hạn.
	_, err := h.uc.Refresh(
		context.Background(), first.RefreshToken, "10.0.0.1", "tab-2")
	if err == nil {
		t.Fatal("đăng xuất rồi mà dùng lại token trong ân hạn vẫn cấp được token mới " +
			"— phiên đã xoá bị hồi sinh")
	}
}

func TestRefreshWithUnknownTokenFails(t *testing.T) {
	h := newAuthHarness(t, false)

	_, err := h.uc.Refresh(
		context.Background(), "token-khong-ton-tai", "10.0.0.1", "test")
	if got := statusOf(err); got != http.StatusUnauthorized {
		t.Errorf("mã lỗi = %d, muốn 401", got)
	}
}

// TestRefreshFailsWhenAccountDisabled: vô hiệu hoá tài khoản phải chặn được
// cả đường refresh, nếu không người đã nghỉ việc vẫn dùng hệ thống tiếp cho
// tới khi refresh token hết hạn — tức là tới 7 ngày.
func TestRefreshFailsWhenAccountDisabled(t *testing.T) {
	h := newAuthHarness(t, false)
	u := h.seedUser(t, "a@test.local", testPassword)

	pair := h.login(t, "a@test.local")
	u.IsActive = false

	_, err := h.uc.Refresh(
		context.Background(), pair.RefreshToken, "10.0.0.1", "test")
	if got := statusOf(err); got != http.StatusUnauthorized {
		t.Errorf("mã lỗi = %d, muốn 401", got)
	}
}

// TestRefreshDeletesOldSession: phiên cũ phải bị xoá, không tái dùng id.
//
// Giữ lại phiên cũ nghĩa là mỗi chu kỳ refresh để lại một phiên mồ côi trong
// Redis, sống tới 7 ngày.
func TestRefreshDeletesOldSession(t *testing.T) {
	h := newAuthHarness(t, false)
	h.seedUser(t, "a@test.local", testPassword)

	pair := h.login(t, "a@test.local")
	before := len(h.sessions.byID)

	if _, err := h.uc.Refresh(
		context.Background(), pair.RefreshToken, "10.0.0.1", "test"); err != nil {
		t.Fatalf("Refresh lỗi: %v", err)
	}

	if len(h.sessions.byID) != before {
		t.Errorf("số phiên đổi từ %d thành %d — refresh phải thay thế chứ không cộng thêm",
			before, len(h.sessions.byID))
	}
}

// =========================================================================
// ĐĂNG XUẤT
// =========================================================================

func TestLogoutAllRemovesEverySession(t *testing.T) {
	h := newAuthHarness(t, false)
	u := h.seedUser(t, "a@test.local", testPassword)

	// Ba lần đăng nhập = ba thiết bị.
	h.login(t, "a@test.local")
	h.login(t, "a@test.local")
	h.login(t, "a@test.local")

	if len(h.sessions.byID) != 3 {
		t.Fatalf("có %d phiên, muốn 3", len(h.sessions.byID))
	}

	if err := h.uc.LogoutAll(context.Background(), u.ID); err != nil {
		t.Fatalf("LogoutAll lỗi: %v", err)
	}
	if len(h.sessions.byID) != 0 {
		t.Errorf("còn %d phiên sau khi đăng xuất tất cả", len(h.sessions.byID))
	}
}

// =========================================================================
// TOKEN TRUY CẬP
// =========================================================================

// TestAccessTokenCarriesPermissions: token phải mang sẵn quyền, nếu không mỗi
// request sẽ là một truy vấn tính quyền.
func TestAccessTokenCarriesPermissions(t *testing.T) {
	h := newAuthHarness(t, false)
	u := h.seedUser(t, "a@test.local", testPassword)

	h.users.roles[u.ID] = []string{"employee"}
	h.uc.auth = &fakeAuthReader{authz: &domainauth.Authorization{
		Roles:       []string{"employee"},
		Permissions: []string{domainauth.PermChatRead, domainauth.PermTaskRead},
		Scope:       domainauth.ScopeSelf,
	}}

	pair := h.login(t, "a@test.local")

	claims, err := h.jwt.Verify(pair.AccessToken)
	if err != nil {
		t.Fatalf("token vừa cấp không xác minh được: %v", err)
	}
	if claims.UserID != u.ID {
		t.Errorf("uid = %s, muốn %s", claims.UserID, u.ID)
	}
	if claims.EmployeeID != u.EmployeeID {
		t.Errorf("eid = %s, muốn %s", claims.EmployeeID, u.EmployeeID)
	}
	if len(claims.Permissions) != 2 {
		t.Errorf("token mang %d quyền, muốn 2", len(claims.Permissions))
	}
	if claims.Scope != string(domainauth.ScopeSelf) {
		t.Errorf("scope = %q, muốn %q", claims.Scope, domainauth.ScopeSelf)
	}
	if claims.SessionID == uuid.Nil {
		t.Error("token không mang id phiên — không thu hồi được")
	}
}
