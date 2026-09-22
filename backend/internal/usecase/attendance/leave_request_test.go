package attendance

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	domainatt "github.com/PhamVanPhuc2k2/manage/internal/domain/attendance"
	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
)

// Bộ kiểm thử vòng đời đơn nghỉ phép: gửi, duyệt, huỷ, và quỹ ngày phép.
//
// Phần nhạy cảm nhất là QUỸ PHÉP. Trừ nhầm hoặc quên hoàn lại là sai lệch
// âm thầm — không có thông báo lỗi nào, chỉ có một nhân viên vào tháng Mười
// Hai phát hiện mình thiếu hai ngày phép và không ai giải thích được.

func statusOf(err error) int {
	if err == nil {
		return http.StatusOK
	}
	code, _ := apperror.HTTPStatus(err)
	return code
}

// mondayOf là một thứ hai cố định, để mọi phép thử đếm ngày không đổi kết
// quả theo ngày chạy.
func mondayOf() time.Time {
	d := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	if d.Weekday() != time.Monday {
		panic("mốc kiểm thử sai: 2026-09-21 phải là thứ hai")
	}
	return d
}

func leaveActor(employeeID uuid.UUID, perms ...string) *domainauth.Actor {
	p := map[string]struct{}{}
	for _, perm := range perms {
		p[perm] = struct{}{}
	}
	return &domainauth.Actor{
		UserID:      uuid.New(),
		EmployeeID:  employeeID,
		Permissions: p,
		Scope:       domainauth.ScopeAll,
	}
}

// withOfficeHours gắn khung giờ hành chính cho mọi nhân viên, để phép đếm
// ngày bỏ đúng cuối tuần.
func (h *harness) withOfficeHours() {
	h.schedules.applicable = func(uuid.UUID, time.Time) []*domainatt.Schedule {
		return []*domainatt.Schedule{officeHours()}
	}
}

// =========================================================================
// GỬI ĐƠN
// =========================================================================

func TestCreateLeaveRejectsBadInput(t *testing.T) {
	monday := mondayOf()
	saturday := monday.AddDate(0, 0, 5)
	sunday := monday.AddDate(0, 0, 6)

	cases := []struct {
		name string
		in   LeaveInput
		want int
	}{
		{
			"loại nghỉ lạ",
			LeaveInput{Type: "du-lich-sao-hoa", StartDate: monday, EndDate: monday},
			http.StatusBadRequest,
		},
		{
			"phần ngày lạ",
			LeaveInput{Type: domainatt.LeaveAnnual, StartDate: monday, EndDate: monday,
				DayPart: "buoi-toi"},
			http.StatusBadRequest,
		},
		{
			"kết thúc trước bắt đầu",
			LeaveInput{Type: domainatt.LeaveAnnual, StartDate: monday,
				EndDate: monday.AddDate(0, 0, -1)},
			http.StatusBadRequest,
		},
		{
			"nửa ngày nhưng nhiều ngày",
			LeaveInput{Type: domainatt.LeaveAnnual, StartDate: monday,
				EndDate: monday.AddDate(0, 0, 2), DayPart: domainatt.PartMorning},
			http.StatusBadRequest,
		},
		{
			"dài quá một năm",
			LeaveInput{Type: domainatt.LeaveAnnual, StartDate: monday,
				EndDate: monday.AddDate(2, 0, 0)},
			http.StatusBadRequest,
		},
		{
			"toàn cuối tuần",
			LeaveInput{Type: domainatt.LeaveAnnual, StartDate: saturday, EndDate: sunday},
			http.StatusBadRequest,
		},
		{
			"hợp lệ",
			LeaveInput{Type: domainatt.LeaveAnnual, StartDate: monday, EndDate: monday},
			http.StatusOK,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(monday)
			h.withOfficeHours()

			_, err := h.uc.CreateLeave(
				context.Background(), leaveActor(uuid.New()), tc.in)

			if got := statusOf(err); got != tc.want {
				t.Errorf("mã lỗi = %d, muốn %d (%v)", got, tc.want, err)
			}
		})
	}
}

func TestCreateLeaveRequiresActor(t *testing.T) {
	h := newHarness(mondayOf())

	_, err := h.uc.CreateLeave(context.Background(), nil, LeaveInput{
		Type: domainatt.LeaveAnnual, StartDate: mondayOf(), EndDate: mondayOf(),
	})
	if got := statusOf(err); got != http.StatusUnauthorized {
		t.Errorf("mã lỗi = %d, muốn 401", got)
	}
}

func TestCreateLeaveDefaultsToFullDay(t *testing.T) {
	monday := mondayOf()
	h := newHarness(monday)
	h.withOfficeHours()

	if _, err := h.uc.CreateLeave(
		context.Background(), leaveActor(uuid.New()),
		LeaveInput{Type: domainatt.LeaveAnnual, StartDate: monday, EndDate: monday},
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if len(h.leaves.created) != 1 {
		t.Fatalf("số đơn đã tạo = %d, muốn 1", len(h.leaves.created))
	}
	if got := h.leaves.created[0].DayPart; got != domainatt.PartFull {
		t.Errorf("phần ngày = %q, muốn %q", got, domainatt.PartFull)
	}
}

// TestCreateLeaveRejectsOverlap: không chặn thì quỹ phép bị trừ hai lần và
// bảng công có hai nguồn sự thật mâu thuẫn cho cùng một ngày.
func TestCreateLeaveRejectsOverlap(t *testing.T) {
	monday := mondayOf()
	h := newHarness(monday)
	h.withOfficeHours()
	h.leaves.overlapping = []*domainatt.LeaveRequest{{ID: uuid.New()}}

	_, err := h.uc.CreateLeave(
		context.Background(), leaveActor(uuid.New()),
		LeaveInput{Type: domainatt.LeaveAnnual, StartDate: monday, EndDate: monday})

	if got := statusOf(err); got != http.StatusConflict {
		t.Errorf("mã lỗi = %d, muốn 409", got)
	}
	if len(h.leaves.created) != 0 {
		t.Error("đã tạo đơn dù trùng khoảng thời gian")
	}
}

// TestCreateLeaveChecksBalanceUpFront.
//
// Quỹ phép được kiểm NGAY LÚC GỬI, không đợi tới lúc duyệt. Báo sớm để nhân
// viên chuyển sang nghỉ không lương, thay vì chờ vài ngày rồi bị từ chối vì
// một lý do họ hoàn toàn có thể tự thấy trước.
func TestCreateLeaveChecksBalanceUpFront(t *testing.T) {
	monday := mondayOf()
	empID := uuid.New()

	h := newHarness(monday)
	h.withOfficeHours()
	h.balances.set(empID, monday.Year(), &domainatt.Balance{
		EmployeeID: empID, Year: monday.Year(), EntitledDays: 12, UsedDays: 11,
	})

	// Xin 3 ngày làm việc (thứ hai → thứ tư) nhưng chỉ còn 1.
	_, err := h.uc.CreateLeave(
		context.Background(), leaveActor(empID),
		LeaveInput{Type: domainatt.LeaveAnnual, StartDate: monday,
			EndDate: monday.AddDate(0, 0, 2)})

	if got := statusOf(err); got != http.StatusConflict {
		t.Fatalf("mã lỗi = %d, muốn 409", got)
	}
	if len(h.leaves.created) != 0 {
		t.Error("đã tạo đơn dù không đủ quỹ phép")
	}
}

// TestCreateLeaveWithoutBalanceRowIsAllowed.
//
// Chưa có dòng quỹ nghĩa là nhân sự chưa khởi tạo cho năm nay, không phải
// "hết phép". Chặn ở đây sẽ làm cả công ty không gửi được đơn vào đầu
// tháng Một.
func TestCreateLeaveWithoutBalanceRowIsAllowed(t *testing.T) {
	monday := mondayOf()
	h := newHarness(monday)
	h.withOfficeHours()

	if _, err := h.uc.CreateLeave(
		context.Background(), leaveActor(uuid.New()),
		LeaveInput{Type: domainatt.LeaveAnnual, StartDate: monday, EndDate: monday},
	); err != nil {
		t.Fatalf("chưa thiết lập quỹ phép không được chặn gửi đơn: %v", err)
	}
}

// TestCreateUnpaidLeaveSkipsBalanceCheck: chỉ phép năm trừ quỹ. Nghỉ không
// lương mà vẫn bị chặn vì hết phép là vô lý — đó chính là thứ người ta dùng
// khi đã hết phép.
func TestCreateUnpaidLeaveSkipsBalanceCheck(t *testing.T) {
	monday := mondayOf()
	empID := uuid.New()

	h := newHarness(monday)
	h.withOfficeHours()
	h.balances.set(empID, monday.Year(), &domainatt.Balance{
		EmployeeID: empID, Year: monday.Year(), EntitledDays: 12, UsedDays: 12,
	})

	if _, err := h.uc.CreateLeave(
		context.Background(), leaveActor(empID),
		LeaveInput{Type: domainatt.LeaveUnpaid, StartDate: monday, EndDate: monday},
	); err != nil {
		t.Fatalf("nghỉ không lương không được kiểm quỹ phép: %v", err)
	}
}

// =========================================================================
// DUYỆT ĐƠN
// =========================================================================

// TestCannotApproveOwnLeave là nguyên tắc kiểm soát nội bộ cơ bản.
//
// Luật này phải nằm ở tầng nghiệp vụ chứ không ở giao diện: ẩn nút không
// ngăn được một lời gọi API trực tiếp.
func TestCannotApproveOwnLeave(t *testing.T) {
	monday := mondayOf()
	empID := uuid.New()

	h := newHarness(monday)
	r := h.leaves.add(&domainatt.LeaveRequest{
		EmployeeID: empID, Type: domainatt.LeaveAnnual,
		StartDate: monday, EndDate: monday, Days: 1,
		Status: domainatt.StatusPending,
	})

	_, err := h.uc.DecideLeave(context.Background(),
		leaveActor(empID, domainauth.PermLeaveApprove), r.ID, true, "")

	if got := statusOf(err); got != http.StatusForbidden {
		t.Errorf("mã lỗi = %d, muốn 403", got)
	}
	if len(h.leaves.updated) != 0 {
		t.Error("đơn đã bị sửa dù không được phép duyệt")
	}
}

func TestCannotDecideTwice(t *testing.T) {
	monday := mondayOf()
	h := newHarness(monday)
	r := h.leaves.add(&domainatt.LeaveRequest{
		EmployeeID: uuid.New(), Type: domainatt.LeaveAnnual,
		StartDate: monday, EndDate: monday, Days: 1,
		Status: domainatt.StatusApproved,
	})

	_, err := h.uc.DecideLeave(context.Background(),
		leaveActor(uuid.New(), domainauth.PermLeaveApprove), r.ID, false, "")

	if got := statusOf(err); got != http.StatusConflict {
		t.Errorf("mã lỗi = %d, muốn 409", got)
	}
}

// TestApproveLeaveDeductsBalance kiểm chứng quỹ phép bị trừ đúng số ngày.
func TestApproveLeaveDeductsBalance(t *testing.T) {
	monday := mondayOf()
	empID := uuid.New()

	h := newHarness(monday)
	r := h.leaves.add(&domainatt.LeaveRequest{
		EmployeeID: empID, Type: domainatt.LeaveAnnual,
		StartDate: monday, EndDate: monday, Days: 1,
		Status: domainatt.StatusPending,
	})

	if _, err := h.uc.DecideLeave(context.Background(),
		leaveActor(uuid.New(), domainauth.PermLeaveApprove), r.ID, true, "đồng ý",
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if got := h.leaves.byID[r.ID].Status; got != domainatt.StatusApproved {
		t.Errorf("trạng thái = %q, muốn %q", got, domainatt.StatusApproved)
	}
	if got := h.balances.used[balanceKey{empID, monday.Year()}]; got != 1 {
		t.Errorf("số ngày đã trừ = %.1f, muốn 1", got)
	}
}

// TestRejectLeaveDoesNotDeductBalance: từ chối mà vẫn trừ quỹ là mất trắng
// ngày phép cho một đơn chưa bao giờ được dùng.
func TestRejectLeaveDoesNotDeductBalance(t *testing.T) {
	monday := mondayOf()
	empID := uuid.New()

	h := newHarness(monday)
	r := h.leaves.add(&domainatt.LeaveRequest{
		EmployeeID: empID, Type: domainatt.LeaveAnnual,
		StartDate: monday, EndDate: monday, Days: 1,
		Status: domainatt.StatusPending,
	})

	if _, err := h.uc.DecideLeave(context.Background(),
		leaveActor(uuid.New(), domainauth.PermLeaveApprove), r.ID, false, "bận việc",
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if got := h.leaves.byID[r.ID].Status; got != domainatt.StatusRejected {
		t.Errorf("trạng thái = %q, muốn %q", got, domainatt.StatusRejected)
	}
	if got := h.balances.used[balanceKey{empID, monday.Year()}]; got != 0 {
		t.Errorf("số ngày đã trừ = %.1f, muốn 0", got)
	}
}

// TestApproveSickLeaveDoesNotDeductAnnualBalance: nghỉ ốm có quy định riêng,
// gộp chung vào quỹ phép năm là sai về nghiệp vụ.
func TestApproveSickLeaveDoesNotDeductAnnualBalance(t *testing.T) {
	monday := mondayOf()
	empID := uuid.New()

	h := newHarness(monday)
	r := h.leaves.add(&domainatt.LeaveRequest{
		EmployeeID: empID, Type: domainatt.LeaveSick,
		StartDate: monday, EndDate: monday, Days: 1,
		Status: domainatt.StatusPending,
	})

	if _, err := h.uc.DecideLeave(context.Background(),
		leaveActor(uuid.New(), domainauth.PermLeaveApprove), r.ID, true, "",
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if got := h.balances.used[balanceKey{empID, monday.Year()}]; got != 0 {
		t.Errorf("số ngày đã trừ = %.1f, muốn 0", got)
	}
}

// TestApproveLeaveRecordsDecision: ai duyệt và duyệt lúc nào phải được ghi
// lại — nếu không thì khi có tranh cãi không ai trả lời được.
func TestApproveLeaveRecordsDecision(t *testing.T) {
	monday := mondayOf()
	approver := uuid.New()

	h := newHarness(monday)
	r := h.leaves.add(&domainatt.LeaveRequest{
		EmployeeID: uuid.New(), Type: domainatt.LeaveAnnual,
		StartDate: monday, EndDate: monday, Days: 1,
		Status: domainatt.StatusPending,
	})

	if _, err := h.uc.DecideLeave(context.Background(),
		leaveActor(approver, domainauth.PermLeaveApprove), r.ID, true, "  đồng ý  ",
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	got := h.leaves.byID[r.ID]
	if got.ApproverID == nil || *got.ApproverID != approver {
		t.Errorf("người duyệt = %v, muốn %v", got.ApproverID, approver)
	}
	if got.DecidedAt == nil || !got.DecidedAt.Equal(monday) {
		t.Errorf("thời điểm duyệt = %v, muốn %v", got.DecidedAt, monday)
	}
	if got.DecisionNote != "đồng ý" {
		t.Errorf("ghi chú = %q, khoảng trắng chưa được cắt", got.DecisionNote)
	}
}

func TestDecideMissingLeaveReturns404(t *testing.T) {
	h := newHarness(mondayOf())

	_, err := h.uc.DecideLeave(context.Background(),
		leaveActor(uuid.New(), domainauth.PermLeaveApprove), uuid.New(), true, "")

	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
}

// =========================================================================
// HUỶ ĐƠN
// =========================================================================

// TestCancelApprovedLeaveRefundsBalance.
//
// Huỷ đơn ĐÃ DUYỆT mà không hoàn quỹ là nhân viên mất trắng số ngày đó. Đây
// là sai lệch một chiều: nó chỉ bao giờ thiệt cho nhân viên, nên không ai ở
// phía công ty có động cơ phát hiện ra.
func TestCancelApprovedLeaveRefundsBalance(t *testing.T) {
	monday := mondayOf()
	empID := uuid.New()

	h := newHarness(monday)
	r := h.leaves.add(&domainatt.LeaveRequest{
		EmployeeID: empID, Type: domainatt.LeaveAnnual,
		StartDate: monday, EndDate: monday, Days: 2,
		Status: domainatt.StatusApproved,
	})

	if err := h.uc.CancelLeave(
		context.Background(), leaveActor(empID), r.ID,
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if got := h.leaves.byID[r.ID].Status; got != domainatt.StatusCancelled {
		t.Errorf("trạng thái = %q, muốn %q", got, domainatt.StatusCancelled)
	}
	if got := h.balances.used[balanceKey{empID, monday.Year()}]; got != -2 {
		t.Errorf("số ngày hoàn = %.1f, muốn -2", got)
	}
}

// TestCancelPendingLeaveDoesNotTouchBalance: đơn chờ duyệt chưa trừ quỹ, nên
// huỷ nó mà hoàn lại là CỘNG THÊM ngày phép từ không khí.
func TestCancelPendingLeaveDoesNotTouchBalance(t *testing.T) {
	monday := mondayOf()
	empID := uuid.New()

	h := newHarness(monday)
	r := h.leaves.add(&domainatt.LeaveRequest{
		EmployeeID: empID, Type: domainatt.LeaveAnnual,
		StartDate: monday, EndDate: monday, Days: 1,
		Status: domainatt.StatusPending,
	})

	if err := h.uc.CancelLeave(
		context.Background(), leaveActor(empID), r.ID,
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if got := h.balances.used[balanceKey{empID, monday.Year()}]; got != 0 {
		t.Errorf("quỹ phép thay đổi %.1f ngày, muốn 0", got)
	}
}

func TestOnlyOwnerCanCancelLeave(t *testing.T) {
	monday := mondayOf()
	h := newHarness(monday)
	r := h.leaves.add(&domainatt.LeaveRequest{
		EmployeeID: uuid.New(), Type: domainatt.LeaveAnnual,
		StartDate: monday, EndDate: monday, Days: 1,
		Status: domainatt.StatusPending,
	})

	err := h.uc.CancelLeave(context.Background(),
		leaveActor(uuid.New(), domainauth.PermLeaveApprove), r.ID)

	if got := statusOf(err); got != http.StatusForbidden {
		t.Errorf("mã lỗi = %d, muốn 403", got)
	}
}

// TestCancelAlreadyCancelledIsNoOp: bấm huỷ hai lần không có lý do gì để
// báo lỗi, và quan trọng hơn là không được hoàn quỹ phép lần thứ hai.
func TestCancelAlreadyCancelledIsNoOp(t *testing.T) {
	monday := mondayOf()
	empID := uuid.New()

	h := newHarness(monday)
	r := h.leaves.add(&domainatt.LeaveRequest{
		EmployeeID: empID, Type: domainatt.LeaveAnnual,
		StartDate: monday, EndDate: monday, Days: 1,
		Status: domainatt.StatusCancelled,
	})

	if err := h.uc.CancelLeave(
		context.Background(), leaveActor(empID), r.ID,
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if got := h.balances.used[balanceKey{empID, monday.Year()}]; got != 0 {
		t.Errorf("quỹ phép thay đổi %.1f ngày, muốn 0", got)
	}
}

func TestCancelRejectedLeaveIsConflict(t *testing.T) {
	monday := mondayOf()
	empID := uuid.New()

	h := newHarness(monday)
	r := h.leaves.add(&domainatt.LeaveRequest{
		EmployeeID: empID, Type: domainatt.LeaveAnnual,
		StartDate: monday, EndDate: monday, Days: 1,
		Status: domainatt.StatusRejected,
	})

	err := h.uc.CancelLeave(context.Background(), leaveActor(empID), r.ID)
	if got := statusOf(err); got != http.StatusConflict {
		t.Errorf("mã lỗi = %d, muốn 409", got)
	}
}

// =========================================================================
// PHẠM VI DỮ LIỆU
// =========================================================================

// TestListLeavesIgnoresScopeFromQueryString: hai trường phạm vi nằm cùng
// struct với các trường lọc thường, nên chúng phải bị xoá trước khi usecase
// tự đặt lại.
func TestListLeavesIgnoresScopeFromQueryString(t *testing.T) {
	empID := uuid.New()
	h := newHarness(mondayOf())

	forged := domainatt.LeaveFilter{
		ScopedEmployeeIDs: []uuid.UUID{uuid.New(), uuid.New()},
		RestrictScope:     false,
	}

	if _, err := h.uc.ListLeaves(
		context.Background(), leaveActor(empID), forged,
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	f := h.leaves.lastFilter
	if !f.RestrictScope {
		t.Error("phạm vi giả mạo đã tắt được giới hạn")
	}
	if len(f.ScopedEmployeeIDs) != 1 || f.ScopedEmployeeIDs[0] != empID {
		t.Errorf("danh sách phạm vi = %v, muốn [%v]", f.ScopedEmployeeIDs, empID)
	}
}

// TestLeaveScopeNeedsApprovePermission: không có quyền duyệt thì chỉ thấy
// đơn của chính mình, bất kể phạm vi vai trò rộng tới đâu.
func TestLeaveScopeNeedsApprovePermission(t *testing.T) {
	empID := uuid.New()
	h := newHarness(mondayOf())

	if _, err := h.uc.ListLeaves(
		context.Background(), leaveActor(empID), domainatt.LeaveFilter{},
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	f := h.leaves.lastFilter
	if !f.RestrictScope || len(f.ScopedEmployeeIDs) != 1 {
		t.Errorf("phạm vi = %v (giới hạn %v), muốn chỉ mình %v",
			f.ScopedEmployeeIDs, f.RestrictScope, empID)
	}
}

func TestLeaveScopeAllSeesEverything(t *testing.T) {
	h := newHarness(mondayOf())

	if _, err := h.uc.ListLeaves(
		context.Background(),
		leaveActor(uuid.New(), domainauth.PermLeaveApprove),
		domainatt.LeaveFilter{},
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if h.leaves.lastFilter.RestrictScope {
		t.Error("phạm vi toàn công ty không được bị giới hạn")
	}
}

// TestLeaveScopeDepartmentIncludesSelf: trưởng phòng có thể không thuộc
// phòng mình quản lý (ví dụ phó giám đốc kiêm nhiệm), nên danh sách phải
// luôn có chính họ.
func TestLeaveScopeDepartmentIncludesSelf(t *testing.T) {
	empID := uuid.New()
	member := uuid.New()

	h := newHarness(mondayOf())
	h.employees.activeIDs = []uuid.UUID{member}

	actor := leaveActor(empID, domainauth.PermLeaveApprove)
	actor.Scope = domainauth.ScopeDepartment
	actor.ManagedDepartmentIDs = []uuid.UUID{uuid.New()}

	if _, err := h.uc.ListLeaves(
		context.Background(), actor, domainatt.LeaveFilter{},
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	ids := h.leaves.lastFilter.ScopedEmployeeIDs
	if len(ids) != 2 || ids[0] != member || ids[1] != empID {
		t.Errorf("phạm vi = %v, muốn [%v %v]", ids, member, empID)
	}
}

// TestLeaveScopeDepartmentWithoutDepartmentsFallsBackToSelf: người duyệt
// chưa được gán phòng nào phải rơi về "chỉ thấy mình", không phải "thấy tất".
func TestLeaveScopeDepartmentWithoutDepartmentsFallsBackToSelf(t *testing.T) {
	empID := uuid.New()
	h := newHarness(mondayOf())

	actor := leaveActor(empID, domainauth.PermLeaveApprove)
	actor.Scope = domainauth.ScopeDepartment

	if _, err := h.uc.ListLeaves(
		context.Background(), actor, domainatt.LeaveFilter{},
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	ids := h.leaves.lastFilter.ScopedEmployeeIDs
	if !h.leaves.lastFilter.RestrictScope || len(ids) != 1 || ids[0] != empID {
		t.Errorf("phạm vi = %v, muốn chỉ [%v]", ids, empID)
	}
}

// TestListLeavesOfOtherPersonNeedsScope: lọc theo employee_id của người khác
// phải bị chặn bằng 404, không phải 403.
func TestListLeavesOfOtherPersonNeedsScope(t *testing.T) {
	other := uuid.New()
	h := newHarness(mondayOf())

	_, err := h.uc.ListLeaves(
		context.Background(), leaveActor(uuid.New()),
		domainatt.LeaveFilter{EmployeeID: &other})

	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
}

// =========================================================================
// QUỸ NGÀY PHÉP
// =========================================================================

// TestGetBalanceReturnsZeroInsteadOf404.
//
// Màn hình "số phép còn lại" luôn phải hiện được một con số, kể cả con số 0.
// Trả 404 khi nhân sự chưa khởi tạo quỹ sẽ làm cả trang gãy vào đầu năm.
func TestGetBalanceReturnsZeroInsteadOf404(t *testing.T) {
	empID := uuid.New()
	h := newHarness(mondayOf())

	got, err := h.uc.GetBalance(
		context.Background(), leaveActor(empID), empID, 2026)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if got.Remaining() != 0 {
		t.Errorf("số ngày còn lại = %.1f, muốn 0", got.Remaining())
	}
}

func TestGetBalanceOfOtherPersonNeedsScope(t *testing.T) {
	other := uuid.New()
	h := newHarness(mondayOf())

	_, err := h.uc.GetBalance(
		context.Background(), leaveActor(uuid.New()), other, 2026)

	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
}

func TestSetBalanceRejectsBadInput(t *testing.T) {
	cases := []struct {
		name        string
		year        int
		entitled    float64
		carriedOver float64
	}{
		{"số ngày âm", 2026, -1, 0},
		{"ngày chuyển sang âm", 2026, 12, -1},
		{"năm quá xa quá khứ", 1999, 12, 0},
		{"năm quá xa tương lai", 2201, 12, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(mondayOf())

			_, err := h.uc.SetBalance(
				context.Background(), uuid.New(), tc.year, tc.entitled, tc.carriedOver)

			if got := statusOf(err); got != http.StatusBadRequest {
				t.Errorf("mã lỗi = %d, muốn 400", got)
			}
			if len(h.balances.upserted) != 0 {
				t.Error("đã ghi dù đầu vào không hợp lệ")
			}
		})
	}
}

func TestSetBalanceRejectsUnknownEmployee(t *testing.T) {
	empID := uuid.New()
	h := newHarness(mondayOf())
	h.employees.missing[empID] = true

	_, err := h.uc.SetBalance(context.Background(), empID, 2026, 12, 0)
	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
}

func TestSetBalance(t *testing.T) {
	empID := uuid.New()
	h := newHarness(mondayOf())

	got, err := h.uc.SetBalance(context.Background(), empID, 2026, 12, 3)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if got.Remaining() != 15 {
		t.Errorf("số ngày còn lại = %.1f, muốn 15 (12 + 3 chuyển sang)",
			got.Remaining())
	}
}

func TestListBalances(t *testing.T) {
	empID := uuid.New()
	h := newHarness(mondayOf())
	h.balances.set(empID, 2026, &domainatt.Balance{
		EmployeeID: empID, Year: 2026, EntitledDays: 12,
	})

	got, err := h.uc.ListBalances(
		context.Background(),
		leaveActor(uuid.New(), domainauth.PermLeaveApprove), 2026)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("số dòng quỹ = %d, muốn 1", len(got))
	}
}

// =========================================================================
// ĐỊNH DẠNG SỐ NGÀY
// =========================================================================

// TestFormatDays: thông báo "còn lại 3 ngày" dễ đọc hơn "còn lại 3.0 ngày",
// nhưng nửa ngày thì phải giữ nguyên.
func TestFormatDays(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "0"},
		{1, "1"},
		{3, "3"},
		{12, "12"},
		{2.5, "2.5"},
		{0.5, "0.5"},
	}

	for _, tc := range cases {
		if got := formatDays(tc.in); got != tc.want {
			t.Errorf("formatDays(%.1f) = %q, muốn %q", tc.in, got, tc.want)
		}
	}
}

func TestItoaHandlesNegative(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{0, "0"},
		{7, "7"},
		{-7, "-7"},
		{-1234, "-1234"},
	}

	for _, tc := range cases {
		if got := itoa(tc.in); got != tc.want {
			t.Errorf("itoa(%d) = %q, muốn %q", tc.in, got, tc.want)
		}
	}
}
