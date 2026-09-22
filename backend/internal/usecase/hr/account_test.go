package hr

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"testing"

	"github.com/google/uuid"

	domainhr "github.com/PhamVanPhuc2k2/manage/internal/domain/hr"
	"github.com/PhamVanPhuc2k2/manage/pkg/hash"
)

// Bộ kiểm thử tài khoản đăng nhập và vai trò.
//
// Đây là nơi tập trung các luật chống LEO THANG ĐẶC QUYỀN và chống TỰ KHOÁ
// MÌNH RA NGOÀI. Cả hai đều là loại lỗi không gây ra triệu chứng nào cho tới
// lúc bị khai thác, hoặc tới lúc không còn ai vào được hệ thống.

// stubMailer ghi lại lời gọi thay vì gửi thật.
type stubMailer struct {
	calls []string
	err   error
}

func (m *stubMailer) SendWelcome(_ context.Context, email, _, tempPassword string) error {
	m.calls = append(m.calls, email+"|"+tempPassword)
	return m.err
}

// =========================================================================
// TẠO TÀI KHOẢN
// =========================================================================

// TestCreateAccountIssuesUsablePassword kiểm tra mật khẩu tạm trả về đúng là
// mật khẩu đã băm và lưu.
//
// Trả về một mật khẩu khác với thứ đã lưu là lỗi im lặng hoàn hảo: API báo
// thành công, HR chép mật khẩu đưa cho nhân viên, và nhân viên không đăng
// nhập được mà không ai biết vì sao.
func TestCreateAccountIssuesUsablePassword(t *testing.T) {
	h := newHRHarness()
	emp := h.employee("NV002", nil)

	got, err := h.uc.CreateAccount(context.Background(), actorAll(), emp.ID)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	user := h.users.byEmployeeID[emp.ID]
	if user == nil {
		t.Fatal("chưa tạo tài khoản")
	}
	if err := hash.Verify(user.PasswordHash, got.TempPassword); err != nil {
		t.Error("mật khẩu trả về không khớp với mật khẩu đã lưu")
	}
	if got.TempPassword == user.PasswordHash {
		t.Error("mật khẩu bị lưu dạng thô, chưa băm")
	}
}

// TestCreateAccountForcesPasswordChange: mật khẩu tạm đã đi qua tay HR và
// qua hộp thư, nên nó không còn là bí mật của riêng nhân viên.
func TestCreateAccountForcesPasswordChange(t *testing.T) {
	h := newHRHarness()
	emp := h.employee("NV002", nil)

	if _, err := h.uc.CreateAccount(
		context.Background(), actorAll(), emp.ID,
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	user := h.users.byEmployeeID[emp.ID]
	if !user.MustChangePassword {
		t.Error("tài khoản mới phải bị bắt đổi mật khẩu ở lần đăng nhập đầu")
	}
	if !user.IsActive {
		t.Error("tài khoản mới phải ở trạng thái hoạt động")
	}
}

// TestCreateAccountGrantsLeastPrivilege: tài khoản mới chỉ được vai trò
// "employee".
//
// Nguyên tắc đặc quyền tối thiểu. Gán sẵn vai trò cao hơn để "cho tiện" là
// cách một công ty vài trăm người kết thúc với vài chục tài khoản admin mà
// không ai nhớ vì sao.
func TestCreateAccountGrantsLeastPrivilege(t *testing.T) {
	h := newHRHarness()
	emp := h.employee("NV002", nil)

	if _, err := h.uc.CreateAccount(
		context.Background(), actorAll(), emp.ID,
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if len(h.users.assigned) != 1 {
		t.Fatalf("số vai trò được gán = %d, muốn 1", len(h.users.assigned))
	}
	want := h.users.byEmployeeID[emp.ID].ID.String() + ":" +
		h.roles.byCode["employee"].ID.String()
	if h.users.assigned[0] != want {
		t.Errorf("vai trò gán = %q, muốn %q", h.users.assigned[0], want)
	}
}

func TestCreateAccountRejectsDuplicate(t *testing.T) {
	h := newHRHarness()
	emp := h.employee("NV002", nil)
	h.users.add(&domainhr.User{EmployeeID: emp.ID, Email: emp.Email})

	_, err := h.uc.CreateAccount(context.Background(), actorAll(), emp.ID)
	if got := statusOf(err); got != http.StatusConflict {
		t.Errorf("mã lỗi = %d, muốn 409", got)
	}
}

func TestCreateAccountRejectsResignedEmployee(t *testing.T) {
	h := newHRHarness()
	emp := h.employee("NV002", nil)
	emp.Status = domainhr.StatusResigned

	_, err := h.uc.CreateAccount(context.Background(), actorAll(), emp.ID)
	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
}

func TestCreateAccountOutsideScopeReturns404(t *testing.T) {
	h := newHRHarness()
	other := h.employee("NV002", nil)

	_, err := h.uc.CreateAccount(
		context.Background(), actorSelf(uuid.New()), other.ID)

	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
	if len(h.users.byID) != 0 {
		t.Error("đã tạo tài khoản cho hồ sơ ngoài phạm vi")
	}
}

// TestCreateAccountSurvivesMailFailure.
//
// Mail lỗi KHÔNG được làm hỏng cả thao tác: mật khẩu vẫn nằm trong response
// để HR đưa tay. Chặn lại ở đây là để một sự cố SMTP làm tê liệt việc tiếp
// nhận nhân sự.
func TestCreateAccountSurvivesMailFailure(t *testing.T) {
	h := newHRHarness()
	mailer := &stubMailer{err: errors.New("SMTP sập")}
	h.uc.SetWelcomeMailer(mailer)

	emp := h.employee("NV002", nil)

	got, err := h.uc.CreateAccount(context.Background(), actorAll(), emp.ID)
	if err != nil {
		t.Fatalf("mail lỗi không được làm hỏng việc tạo tài khoản: %v", err)
	}
	if got.TempPassword == "" {
		t.Error("vẫn phải trả về mật khẩu tạm để HR đưa tay")
	}
	if len(mailer.calls) != 1 {
		t.Errorf("số lần gửi mail = %d, muốn 1", len(mailer.calls))
	}
}

func TestCreateAccountNormalizesEmail(t *testing.T) {
	h := newHRHarness()
	emp := h.employee("NV002", nil)
	emp.Email = "  NV002@ABC.VN  "

	got, err := h.uc.CreateAccount(context.Background(), actorAll(), emp.ID)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if got.Email != "nv002@abc.vn" {
		t.Errorf("email = %q, muốn %q", got.Email, "nv002@abc.vn")
	}
}

// =========================================================================
// BẬT / TẮT TÀI KHOẢN
// =========================================================================

// TestDeactivateAccountKillsSessions: tắt tài khoản mà không cắt phiên thì
// người đó vẫn dùng được hệ thống cho tới khi access token hết hạn.
func TestDeactivateAccountKillsSessions(t *testing.T) {
	h := newHRHarness()
	emp := h.employee("NV002", nil)
	user := h.users.add(&domainhr.User{EmployeeID: emp.ID, Email: emp.Email})

	if err := h.uc.SetAccountActive(
		context.Background(), actorAll(), emp.ID, false,
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if h.users.actives[user.ID] {
		t.Error("tài khoản chưa bị tắt")
	}
	if len(h.killedSessions) != 1 || h.killedSessions[0] != user.ID {
		t.Errorf("phiên bị cắt = %v, muốn [%v]", h.killedSessions, user.ID)
	}
}

// TestActivateAccountDoesNotKillSessions: bật lại không có lý do gì để đá
// người dùng ra ngoài.
func TestActivateAccountDoesNotKillSessions(t *testing.T) {
	h := newHRHarness()
	emp := h.employee("NV002", nil)
	h.users.add(&domainhr.User{EmployeeID: emp.ID, Email: emp.Email})

	if err := h.uc.SetAccountActive(
		context.Background(), actorAll(), emp.ID, true,
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if len(h.killedSessions) != 0 {
		t.Errorf("không được cắt phiên khi bật tài khoản: %v", h.killedSessions)
	}
}

func TestCannotDeactivateOwnAccount(t *testing.T) {
	h := newHRHarness()
	emp := h.employee("NV001", nil)
	user := h.users.add(&domainhr.User{EmployeeID: emp.ID, Email: emp.Email})

	actor := actorAll()
	actor.UserID = user.ID
	actor.EmployeeID = emp.ID

	err := h.uc.SetAccountActive(context.Background(), actor, emp.ID, false)
	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
}

func TestSetAccountActiveWithoutAccountReturns404(t *testing.T) {
	h := newHRHarness()
	emp := h.employee("NV002", nil) // có hồ sơ, chưa có tài khoản

	err := h.uc.SetAccountActive(context.Background(), actorAll(), emp.ID, false)
	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
}

// =========================================================================
// VAI TRÒ
// =========================================================================

func TestGetEmployeeRoles(t *testing.T) {
	h := newHRHarness()
	emp := h.employee("NV002", nil)
	h.users.add(&domainhr.User{EmployeeID: emp.ID, Email: emp.Email}, "employee", "hr")

	got, err := h.uc.GetEmployeeRoles(context.Background(), actorAll(), emp.ID)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if len(got.Roles) != 2 {
		t.Errorf("số vai trò = %d, muốn 2", len(got.Roles))
	}
}

func TestGetEmployeeRolesOutsideScopeReturns404(t *testing.T) {
	h := newHRHarness()
	other := h.employee("NV002", nil)
	h.users.add(&domainhr.User{EmployeeID: other.ID, Email: other.Email}, "employee")

	_, err := h.uc.GetEmployeeRoles(
		context.Background(), actorSelf(uuid.New()), other.ID)

	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
}

// TestCannotRemoveOwnAdminRole chặn việc tự khoá mình ra ngoài.
//
// Gỡ vai trò admin của chính mình là mất luôn quyền gán lại. Nếu đó là admin
// duy nhất thì cả hệ thống không còn ai quản trị được và phải sửa tay trong
// database — một thao tác mà phần lớn người vận hành không dám làm.
func TestCannotRemoveOwnAdminRole(t *testing.T) {
	h := newHRHarness()
	emp := h.employee("NV001", nil)
	user := h.users.add(&domainhr.User{EmployeeID: emp.ID, Email: emp.Email},
		"admin", "employee")

	actor := actorAll("admin")
	actor.UserID = user.ID
	actor.EmployeeID = emp.ID

	_, err := h.uc.SetEmployeeRoles(
		context.Background(), actor, emp.ID, []string{"employee"})

	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
	if len(h.users.removed) != 0 {
		t.Error("không được gỡ vai trò nào")
	}
}

// TestCanChangeOwnNonAdminRoles: luật trên chỉ áp cho vai trò admin. Tự sửa
// các vai trò khác của mình không khoá ai ra ngoài cả.
func TestCanChangeOwnNonAdminRoles(t *testing.T) {
	h := newHRHarness()
	emp := h.employee("NV001", nil)
	user := h.users.add(&domainhr.User{EmployeeID: emp.ID, Email: emp.Email},
		"hr", "employee")

	actor := actorAll()
	actor.UserID = user.ID
	actor.EmployeeID = emp.ID

	if _, err := h.uc.SetEmployeeRoles(
		context.Background(), actor, emp.ID, []string{"employee"},
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if len(h.users.removed) != 1 {
		t.Errorf("số vai trò bị gỡ = %d, muốn 1", len(h.users.removed))
	}
}

// TestOnlyAdminCanGrantAdmin chặn leo thang đặc quyền.
//
// Thiếu kiểm tra này, bất kỳ ai có quyền role:assign — nhân sự chẳng hạn —
// đều tự nâng mình lên admin được, và quyền role:assign vốn không được coi
// là quyền nhạy cảm nhất khi thiết kế vai trò.
func TestOnlyAdminCanGrantAdmin(t *testing.T) {
	h := newHRHarness()
	emp := h.employee("NV002", nil)
	h.users.add(&domainhr.User{EmployeeID: emp.ID, Email: emp.Email}, "employee")

	_, err := h.uc.SetEmployeeRoles(
		context.Background(), actorAll("hr"), emp.ID, []string{"admin"})

	if got := statusOf(err); got != http.StatusForbidden {
		t.Errorf("mã lỗi = %d, muốn 403", got)
	}
	if len(h.users.assigned) != 0 {
		t.Error("vai trò admin đã bị gán bởi người không phải admin")
	}
}

func TestAdminCanGrantAdmin(t *testing.T) {
	h := newHRHarness()
	emp := h.employee("NV002", nil)
	h.users.add(&domainhr.User{EmployeeID: emp.ID, Email: emp.Email}, "employee")

	if _, err := h.uc.SetEmployeeRoles(
		context.Background(), actorAll("admin"), emp.ID,
		[]string{"admin", "employee"},
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if len(h.users.assigned) != 1 {
		t.Errorf("số vai trò được gán = %d, muốn 1 (employee đã có sẵn)",
			len(h.users.assigned))
	}
}

// TestSetRolesValidatesBeforeWriting: mọi mã vai trò phải có thật TRƯỚC khi
// ghi bất cứ thứ gì.
//
// Ghi nửa chừng rồi mới phát hiện mã sai sẽ để lại một người có vai trò dở
// dang: đã gỡ cái cũ, chưa gán cái mới. API báo lỗi, người quản trị tưởng
// không có gì thay đổi, còn người kia thì mất quyền.
func TestSetRolesValidatesBeforeWriting(t *testing.T) {
	h := newHRHarness()
	emp := h.employee("NV002", nil)
	h.users.add(&domainhr.User{EmployeeID: emp.ID, Email: emp.Email}, "hr")

	_, err := h.uc.SetEmployeeRoles(
		context.Background(), actorAll("admin"), emp.ID,
		[]string{"employee", "vai-tro-khong-ton-tai"})

	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
	if len(h.users.removed) != 0 || len(h.users.assigned) != 0 {
		t.Errorf("đã ghi dù đầu vào không hợp lệ: gỡ %d, gán %d",
			len(h.users.removed), len(h.users.assigned))
	}
}

func TestSetRolesRejectsEmptyList(t *testing.T) {
	h := newHRHarness()
	emp := h.employee("NV002", nil)
	h.users.add(&domainhr.User{EmployeeID: emp.ID, Email: emp.Email}, "employee")

	_, err := h.uc.SetEmployeeRoles(
		context.Background(), actorAll("admin"), emp.ID, nil)

	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
}

// TestSetRolesOnlyTouchesTheDifference: vai trò đã có không bị gỡ rồi gán lại.
//
// Gỡ rồi gán lại tạo ra một khoảnh khắc người đó không có quyền. Với hệ
// thống đang chạy, đó là một request thất bại không giải thích được.
func TestSetRolesOnlyTouchesTheDifference(t *testing.T) {
	h := newHRHarness()
	emp := h.employee("NV002", nil)
	h.users.add(&domainhr.User{EmployeeID: emp.ID, Email: emp.Email},
		"employee", "hr")

	if _, err := h.uc.SetEmployeeRoles(
		context.Background(), actorAll("admin"), emp.ID,
		[]string{"employee"},
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if len(h.users.assigned) != 0 {
		t.Errorf("không được gán lại vai trò đã có: %v", h.users.assigned)
	}
	if len(h.users.removed) != 1 {
		t.Errorf("số vai trò bị gỡ = %d, muốn 1", len(h.users.removed))
	}
}

// TestSetRolesKillsSessionsOfOtherPeople.
//
// Quyền nằm trong access token, nên người bị đổi vai trò vẫn giữ quyền CŨ tới
// khi token hết hạn. Điều đó nguy hiểm nhất khi đang GỠ quyền của ai đó.
func TestSetRolesKillsSessionsOfOtherPeople(t *testing.T) {
	h := newHRHarness()
	emp := h.employee("NV002", nil)
	user := h.users.add(&domainhr.User{EmployeeID: emp.ID, Email: emp.Email}, "hr")

	if _, err := h.uc.SetEmployeeRoles(
		context.Background(), actorAll("admin"), emp.ID,
		[]string{"employee"},
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if !slices.Contains(h.killedSessions, user.ID) {
		t.Errorf("phiên bị cắt = %v, phải có %v", h.killedSessions, user.ID)
	}
}

// TestSetRolesDoesNotKillOwnSession: tự sửa vai trò của mình mà bị đá ra
// ngoài giữa chừng là trải nghiệm tệ và không cần thiết — người đó đang
// ngồi ngay đó và biết mình vừa làm gì.
func TestSetRolesDoesNotKillOwnSession(t *testing.T) {
	h := newHRHarness()
	emp := h.employee("NV001", nil)
	user := h.users.add(&domainhr.User{EmployeeID: emp.ID, Email: emp.Email},
		"hr", "employee")

	actor := actorAll("admin")
	actor.UserID = user.ID
	actor.EmployeeID = emp.ID

	if _, err := h.uc.SetEmployeeRoles(
		context.Background(), actor, emp.ID, []string{"employee"},
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if len(h.killedSessions) != 0 {
		t.Errorf("không được tự cắt phiên của mình: %v", h.killedSessions)
	}
}

func TestSetRolesWithoutAccountReturns404(t *testing.T) {
	h := newHRHarness()
	emp := h.employee("NV002", nil) // chưa có tài khoản

	_, err := h.uc.SetEmployeeRoles(
		context.Background(), actorAll("admin"), emp.ID, []string{"employee"})

	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
}

func TestListRoles(t *testing.T) {
	h := newHRHarness()

	got, err := h.uc.ListRoles(context.Background())
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("số vai trò = %d, muốn 3", len(got))
	}
}
