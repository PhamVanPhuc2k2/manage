package payroll

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	domainpay "github.com/PhamVanPhuc2k2/manage/internal/domain/payroll"
)

// Bộ kiểm thử phiếu lương.
//
// Lương là dữ liệu nhạy cảm nhất trong hệ thống, và phân quyền ở đây KHÁC
// mọi module khác: mặc định chỉ xem được của mình, và KHÔNG có ngoại lệ
// theo phòng ban — trưởng phòng không xem được lương nhân viên phòng mình.

// draftPeriod dựng một kỳ lương còn nháp kèm một phiếu lương.
func draftPeriod(h *harness, employeeID uuid.UUID) (*domainpay.Period, *domainpay.Payslip) {
	p := h.periods.add(&domainpay.Period{
		CompanyID:   h.company.id,
		Year:        2026,
		Month:       8,
		Name:        "Lương tháng 08/2026",
		Status:      domainpay.StatusDraft,
		PeriodStart: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC),
	})

	s := h.payslips.add(&domainpay.Payslip{
		PeriodID:     p.ID,
		EmployeeID:   employeeID,
		EmployeeName: "Nguyễn Văn A",
		EmployeeCode: "NV001",

		BaseSalary:  money(20_000_000),
		GrossSalary: money(20_000_000),

		InsuranceEmployee: money(2_100_000),
		TaxableIncome:     money(20_000_000),
		PersonalDeduction: money(11_000_000),
		AssessableIncome:  money(9_000_000),
		IncomeTax:         money(650_000),
		NetSalary:         money(17_250_000),
	})
	return p, s
}

// =========================================================================
// PHÂN QUYỀN
// =========================================================================

// TestGetPayslipOfOtherPersonReturns404.
//
// 404 chứ không 403: xác nhận "người này có phiếu lương nhưng bạn không
// được xem" đã là rò rỉ. Cùng nguyên tắc với nhân sự và chấm công.
func TestGetPayslipOfOtherPersonReturns404(t *testing.T) {
	h := newHarness(nowAfterAugust())
	_, s := draftPeriod(h, uuid.New())

	_, err := h.uc.GetPayslip(
		context.Background(), payActor(uuid.New()), s.ID)

	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
}

// TestDepartmentScopeDoesNotOpenPayroll là điểm khác biệt quan trọng nhất
// của module này.
//
// Trưởng phòng có phạm vi "department" xem được bảng công cả phòng, nhưng
// KHÔNG xem được lương. Phạm vi vai trò không mở được dữ liệu lương — chỉ
// quyền payroll:read_all mới mở.
func TestDepartmentScopeDoesNotOpenPayroll(t *testing.T) {
	h := newHarness(nowAfterAugust())
	_, s := draftPeriod(h, uuid.New())

	manager := payActor(uuid.New())
	manager.Scope = domainauth.ScopeDepartment
	manager.ManagedDepartmentIDs = []uuid.UUID{uuid.New()}

	_, err := h.uc.GetPayslip(context.Background(), manager, s.ID)
	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404 — phạm vi phòng ban không mở được dữ liệu lương", got)
	}
}

func TestGetOwnPayslipIsAllowed(t *testing.T) {
	empID := uuid.New()
	h := newHarness(nowAfterAugust())
	_, s := draftPeriod(h, empID)

	got, err := h.uc.GetPayslip(context.Background(), payActor(empID), s.ID)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if got.ID != s.ID {
		t.Errorf("trả về %v, muốn %v", got.ID, s.ID)
	}
}

func TestGetPayslipWithReadAllIsAllowed(t *testing.T) {
	h := newHarness(nowAfterAugust())
	_, s := draftPeriod(h, uuid.New())

	if _, err := h.uc.GetPayslip(context.Background(),
		payActor(uuid.New(), domainauth.PermPayrollReadAll), s.ID,
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
}

func TestMyPayslipsRequiresActor(t *testing.T) {
	h := newHarness(nowAfterAugust())

	_, err := h.uc.MyPayslips(context.Background(), nil)
	if got := statusOf(err); got != http.StatusUnauthorized {
		t.Errorf("mã lỗi = %d, muốn 401", got)
	}
}

func TestMyPayslipsReturnsOwnRowsOnly(t *testing.T) {
	empID := uuid.New()
	h := newHarness(nowAfterAugust())
	draftPeriod(h, empID)
	draftPeriod(h, uuid.New())

	got, err := h.uc.MyPayslips(context.Background(), payActor(empID))
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("số phiếu = %d, muốn 1", len(got))
	}
}

// TestListPayslipsAppliesScope: người thường bị ép lọc theo chính mình, kể
// cả khi phạm vi vai trò là toàn công ty.
func TestListPayslipsAppliesScope(t *testing.T) {
	empID := uuid.New()
	h := newHarness(nowAfterAugust())

	if _, err := h.uc.ListPayslips(
		context.Background(), payActor(empID), domainpay.PayslipFilter{},
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	f := h.payslips.lastFilter
	if !f.RestrictScope || len(f.ScopedEmployeeIDs) != 1 ||
		f.ScopedEmployeeIDs[0] != empID {
		t.Errorf("phạm vi = %v (giới hạn %v), muốn chỉ [%v]",
			f.ScopedEmployeeIDs, f.RestrictScope, empID)
	}
}

// TestListPayslipsIgnoresScopeFromQueryString.
func TestListPayslipsIgnoresScopeFromQueryString(t *testing.T) {
	empID := uuid.New()
	h := newHarness(nowAfterAugust())

	forged := domainpay.PayslipFilter{
		ScopedEmployeeIDs: []uuid.UUID{uuid.New()},
		RestrictScope:     false,
	}

	if _, err := h.uc.ListPayslips(
		context.Background(), payActor(empID), forged,
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	f := h.payslips.lastFilter
	if !f.RestrictScope || len(f.ScopedEmployeeIDs) != 1 ||
		f.ScopedEmployeeIDs[0] != empID {
		t.Errorf("phạm vi giả mạo đã lọt qua: %v (giới hạn %v)",
			f.ScopedEmployeeIDs, f.RestrictScope)
	}
}

func TestListPayslipsNormalizesPageSize(t *testing.T) {
	h := newHarness(nowAfterAugust())

	if _, err := h.uc.ListPayslips(context.Background(),
		payActor(uuid.New()),
		domainpay.PayslipFilter{Page: -1, PageSize: 1000000},
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	f := h.payslips.lastFilter
	if f.Page != 1 || f.PageSize != maxPageSize {
		t.Errorf("page/size = %d/%d, muốn 1/%d", f.Page, f.PageSize, maxPageSize)
	}
}

// =========================================================================
// NHẬT KÝ TRUY CẬP
// =========================================================================

// TestViewingPayslipIsAudited.
//
// Nhật ký ghi cả lượt XEM, không chỉ lượt sửa. Rò rỉ bảng lương thường là
// do đọc chứ không do ghi, và không có nhật ký đọc thì không bao giờ biết
// ai đã xem gì.
func TestViewingPayslipIsAudited(t *testing.T) {
	empID := uuid.New()
	h := newHarness(nowAfterAugust())
	_, s := draftPeriod(h, empID)

	if _, err := h.uc.GetPayslip(
		context.Background(), payActor(empID), s.ID,
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if !slices.Contains(h.audit.actions(), domainpay.AuditViewPayslip) {
		t.Errorf("nhật ký = %v, phải có %q",
			h.audit.actions(), domainpay.AuditViewPayslip)
	}
}

// TestListingWholePayrollIsAudited: xem cả bảng lương công ty là thao tác
// đáng ghi nhất trong module.
func TestListingWholePayrollIsAudited(t *testing.T) {
	h := newHarness(nowAfterAugust())

	if _, err := h.uc.ListPayslips(context.Background(),
		payActor(uuid.New(), domainauth.PermPayrollReadAll),
		domainpay.PayslipFilter{},
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if !slices.Contains(h.audit.actions(), domainpay.AuditListPayroll) {
		t.Errorf("nhật ký = %v, phải có %q",
			h.audit.actions(), domainpay.AuditListPayroll)
	}
}

// TestAuditFailureDoesNotBreakTheRequest.
//
// Ghi nhật ký hỏng thì thao tác nghiệp vụ vẫn thành công — nó đã xong rồi,
// trả lỗi lúc này chỉ khiến người dùng thấy "thất bại" trong khi việc của
// họ đã hoàn tất.
func TestAuditFailureDoesNotBreakTheRequest(t *testing.T) {
	empID := uuid.New()
	h := newHarness(nowAfterAugust())
	h.audit.logErr = errFake
	_, s := draftPeriod(h, empID)

	if _, err := h.uc.GetPayslip(
		context.Background(), payActor(empID), s.ID,
	); err != nil {
		t.Errorf("nhật ký hỏng không được làm hỏng thao tác: %v", err)
	}
}

// TestAuditMetaTravelsThroughContext: IP và request id là dữ liệu của tầng
// vận chuyển, đi qua context để usecase không có chữ "IP" trong chữ ký hàm.
func TestAuditMetaTravelsThroughContext(t *testing.T) {
	empID := uuid.New()
	h := newHarness(nowAfterAugust())
	_, s := draftPeriod(h, empID)

	ctx := WithAuditMeta(context.Background(), "10.0.0.7", "req-123")

	if _, err := h.uc.GetPayslip(ctx, payActor(empID), s.ID); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if len(h.audit.entries) == 0 {
		t.Fatal("chưa ghi dòng nhật ký nào")
	}
	e := h.audit.entries[0]
	if e.IP != "10.0.0.7" || e.RequestID != "req-123" {
		t.Errorf("nhật ký ghi IP %q / request %q, muốn 10.0.0.7 / req-123",
			e.IP, e.RequestID)
	}
}

func TestListAudit(t *testing.T) {
	empID := uuid.New()
	h := newHarness(nowAfterAugust())
	_, s := draftPeriod(h, empID)

	if _, err := h.uc.GetPayslip(
		context.Background(), payActor(empID), s.ID,
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	got, err := h.uc.ListAudit(context.Background(), "payslip", nil)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("số dòng = %d, muốn 1", len(got))
	}
}

// =========================================================================
// SỬA PHIẾU LƯƠNG
// =========================================================================

// TestUpdatePayslipRecalculatesTaxFromScratch.
//
// Thưởng tăng thì thu nhập chịu thuế tăng, và thuế phải tính LẠI theo biểu
// luỹ tiến chứ không cộng thêm theo tỷ lệ cũ. Cộng chênh lệch vào con số cũ
// sẽ ra sai mỗi khi phần thưởng đẩy người đó sang bậc thuế cao hơn.
func TestUpdatePayslipRecalculatesTaxFromScratch(t *testing.T) {
	empID := uuid.New()
	h := newHarness(nowAfterAugust())
	_, s := draftPeriod(h, empID)

	before := s.IncomeTax

	got, err := h.uc.UpdatePayslip(context.Background(),
		payActor(uuid.New(), domainauth.PermPayrollReadAll),
		s.ID, money(10_000_000), 0, "thưởng dự án")
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if got.Bonuses != money(10_000_000) {
		t.Errorf("thưởng = %d, muốn 10000000", got.Bonuses)
	}
	if got.GrossSalary != money(30_000_000) {
		t.Errorf("tổng thu nhập = %d, muốn 30000000", got.GrossSalary)
	}
	if got.IncomeTax <= before {
		t.Errorf("thuế = %d, phải cao hơn %d sau khi cộng thưởng",
			got.IncomeTax, before)
	}

	// Thu nhập tính thuế mới = 30tr - 11tr giảm trừ = 19tr.
	// Luỹ tiến: 5tr×5% + 5tr×10% + 8tr×15% + 1tr×20% = 250k+500k+1.2tr+200k.
	if got.AssessableIncome != money(19_000_000) {
		t.Errorf("thu nhập tính thuế = %d, muốn 19000000", got.AssessableIncome)
	}
	if got.IncomeTax != money(2_150_000) {
		t.Errorf("thuế = %d, muốn 2150000 (tính tay theo biểu luỹ tiến)",
			got.IncomeTax)
	}
}

// TestUpdatePayslipNetIsConsistent: thực nhận phải bằng tổng thu nhập trừ
// đi đúng ba khoản. Đây là con số duy nhất nhân viên thật sự nhìn.
func TestUpdatePayslipNetIsConsistent(t *testing.T) {
	h := newHarness(nowAfterAugust())
	_, s := draftPeriod(h, uuid.New())

	got, err := h.uc.UpdatePayslip(context.Background(),
		payActor(uuid.New(), domainauth.PermPayrollReadAll),
		s.ID, money(5_000_000), money(1_000_000), "")
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	want := got.GrossSalary - got.InsuranceEmployee - got.IncomeTax - got.OtherDeductions
	if got.NetSalary != want {
		t.Errorf("thực nhận = %d, muốn %d", got.NetSalary, want)
	}
}

func TestUpdatePayslipRejectsNegativeAmounts(t *testing.T) {
	cases := []struct {
		name      string
		bonuses   domainpay.Money
		deduction domainpay.Money
	}{
		{"thưởng âm", money(-1), 0},
		{"khấu trừ âm", 0, money(-1)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(nowAfterAugust())
			_, s := draftPeriod(h, uuid.New())

			_, err := h.uc.UpdatePayslip(context.Background(),
				payActor(uuid.New(), domainauth.PermPayrollReadAll),
				s.ID, tc.bonuses, tc.deduction, "")

			if got := statusOf(err); got != http.StatusBadRequest {
				t.Errorf("mã lỗi = %d, muốn 400", got)
			}
			if len(h.payslips.updated) != 0 {
				t.Error("đã ghi dù số tiền âm")
			}
		})
	}
}

func TestUpdatePayslipRejectsLockedPeriod(t *testing.T) {
	h := newHarness(nowAfterAugust())
	p, s := draftPeriod(h, uuid.New())
	h.periods.byID[p.ID].Status = domainpay.StatusLocked

	_, err := h.uc.UpdatePayslip(context.Background(),
		payActor(uuid.New(), domainauth.PermPayrollReadAll),
		s.ID, money(1_000_000), 0, "")

	if got := statusOf(err); got != http.StatusConflict {
		t.Errorf("mã lỗi = %d, muốn 409", got)
	}
	if len(h.payslips.updated) != 0 {
		t.Error("đã sửa phiếu của kỳ đã khoá")
	}
}

// TestUpdatePayslipHonoursRepositoryLock: kỳ có thể bị khoá SAU khi usecase
// đọc nó. Repository chặn bằng ErrLocked và usecase phải dịch nó thành 409,
// không phải 500.
func TestUpdatePayslipHonoursRepositoryLock(t *testing.T) {
	h := newHarness(nowAfterAugust())
	_, s := draftPeriod(h, uuid.New())
	h.payslips.updateErr = domainpay.ErrLocked

	_, err := h.uc.UpdatePayslip(context.Background(),
		payActor(uuid.New(), domainauth.PermPayrollReadAll),
		s.ID, money(1_000_000), 0, "")

	if got := statusOf(err); got != http.StatusConflict {
		t.Errorf("mã lỗi = %d, muốn 409", got)
	}
}

func TestUpdatePayslipUpdatesPeriodTotals(t *testing.T) {
	h := newHarness(nowAfterAugust())
	p, s := draftPeriod(h, uuid.New())

	if _, err := h.uc.UpdatePayslip(context.Background(),
		payActor(uuid.New(), domainauth.PermPayrollReadAll),
		s.ID, money(1_000_000), 0, "  ghi chú  ",
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if !slices.Contains(h.periods.totalsFor, p.ID) {
		t.Error("chưa cập nhật số tổng của kỳ sau khi sửa phiếu")
	}
	if got := h.payslips.byID[s.ID].Note; got != "ghi chú" {
		t.Errorf("ghi chú = %q, khoảng trắng chưa được cắt", got)
	}
}

func TestUpdateMissingPayslipReturns404(t *testing.T) {
	h := newHarness(nowAfterAugust())

	_, err := h.uc.UpdatePayslip(context.Background(),
		payActor(uuid.New(), domainauth.PermPayrollReadAll),
		uuid.New(), 0, 0, "")

	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
}

// =========================================================================
// BÁO CÁO CHI PHÍ
// =========================================================================

// TestCostReportsNeedReadAll: chi phí nhân sự theo phòng ban cho phép suy ra
// lương từng người ở những phòng ít người.
func TestCostReportsNeedReadAll(t *testing.T) {
	h := newHarness(nowAfterAugust())

	t.Run("theo phòng ban", func(t *testing.T) {
		_, err := h.uc.CostByDepartment(
			context.Background(), payActor(uuid.New()), uuid.New())
		if got := statusOf(err); got != http.StatusForbidden {
			t.Errorf("mã lỗi = %d, muốn 403", got)
		}
	})

	t.Run("theo tháng", func(t *testing.T) {
		_, err := h.uc.CostByMonth(
			context.Background(), payActor(uuid.New()), 2026)
		if got := statusOf(err); got != http.StatusForbidden {
			t.Errorf("mã lỗi = %d, muốn 403", got)
		}
	})
}

func TestCostByMonthRejectsBadYear(t *testing.T) {
	h := newHarness(nowAfterAugust())

	for _, year := range []int{1999, 2201} {
		_, err := h.uc.CostByMonth(context.Background(),
			payActor(uuid.New(), domainauth.PermPayrollReadAll), year)

		if got := statusOf(err); got != http.StatusBadRequest {
			t.Errorf("năm %d: mã lỗi = %d, muốn 400", year, got)
		}
	}
}

func TestCostByDepartmentIsAudited(t *testing.T) {
	h := newHarness(nowAfterAugust())

	if _, err := h.uc.CostByDepartment(context.Background(),
		payActor(uuid.New(), domainauth.PermPayrollReadAll), uuid.New(),
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if !slices.Contains(h.audit.actions(), domainpay.AuditListPayroll) {
		t.Errorf("nhật ký = %v, phải có %q",
			h.audit.actions(), domainpay.AuditListPayroll)
	}
}

// =========================================================================
// GỬI PHIẾU LƯƠNG
// =========================================================================

// TestSendPayslipsRejectsDraftPeriod.
//
// Gửi phiếu của kỳ còn nháp nghĩa là gửi con số sắp thay đổi, và một email
// đính chính sau đó không lấy lại được niềm tin đã mất.
func TestSendPayslipsRejectsDraftPeriod(t *testing.T) {
	empID := uuid.New()
	h := newHarness(nowAfterAugust())
	p, _ := draftPeriod(h, empID)

	_, err := h.uc.SendPayslips(context.Background(),
		payActor(uuid.New()), p.ID,
		map[uuid.UUID]string{empID: "a@abc.vn"})

	if got := statusOf(err); got != http.StatusConflict {
		t.Errorf("mã lỗi = %d, muốn 409", got)
	}
	if len(h.mailer.sent) != 0 {
		t.Error("đã gửi phiếu lương của kỳ còn nháp")
	}
}

func TestSendPayslipsFromLockedPeriod(t *testing.T) {
	empID := uuid.New()
	h := newHarness(nowAfterAugust())
	p, _ := draftPeriod(h, empID)
	h.periods.byID[p.ID].Status = domainpay.StatusLocked

	sent, err := h.uc.SendPayslips(context.Background(),
		payActor(uuid.New()), p.ID,
		map[uuid.UUID]string{empID: "a@abc.vn"})
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if sent != 1 || len(h.mailer.sent) != 1 {
		t.Errorf("đã gửi %d, mailer nhận %v; muốn 1", sent, h.mailer.sent)
	}
}

// TestSendPayslipsSkipsMissingEmails: người chưa có email thì bỏ qua, không
// làm hỏng cả đợt gửi cho những người khác.
func TestSendPayslipsSkipsMissingEmails(t *testing.T) {
	withEmail, without := uuid.New(), uuid.New()
	h := newHarness(nowAfterAugust())
	p, _ := draftPeriod(h, withEmail)
	draftPeriodSlipOnly(h, p.ID, without)
	h.periods.byID[p.ID].Status = domainpay.StatusLocked

	sent, err := h.uc.SendPayslips(context.Background(),
		payActor(uuid.New()), p.ID,
		map[uuid.UUID]string{withEmail: "a@abc.vn"})
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if sent != 1 {
		t.Errorf("đã gửi %d, muốn 1", sent)
	}
}

// TestSendPayslipsSurvivesMailFailure: một địa chỉ hỏng không được chặn cả
// đợt gửi của công ty.
func TestSendPayslipsSurvivesMailFailure(t *testing.T) {
	empID := uuid.New()
	h := newHarness(nowAfterAugust())
	p, _ := draftPeriod(h, empID)
	h.periods.byID[p.ID].Status = domainpay.StatusLocked
	h.mailer.err = errFake

	sent, err := h.uc.SendPayslips(context.Background(),
		payActor(uuid.New()), p.ID,
		map[uuid.UUID]string{empID: "a@abc.vn"})
	if err != nil {
		t.Fatalf("lỗi gửi mail không được chặn cả đợt: %v", err)
	}
	if sent != 0 {
		t.Errorf("đã gửi %d, muốn 0", sent)
	}
}

func TestSendPayslipsWithoutMailerIsUnprocessable(t *testing.T) {
	h := newHarness(nowAfterAugust())
	h.uc.mailer = nil
	p, _ := draftPeriod(h, uuid.New())

	_, err := h.uc.SendPayslips(
		context.Background(), payActor(uuid.New()), p.ID, nil)

	if got := statusOf(err); got != http.StatusUnprocessableEntity {
		t.Errorf("mã lỗi = %d, muốn 422", got)
	}
}

// draftPeriodSlipOnly thêm một phiếu nữa vào kỳ đã có.
func draftPeriodSlipOnly(h *harness, periodID, employeeID uuid.UUID) {
	h.payslips.add(&domainpay.Payslip{
		PeriodID: periodID, EmployeeID: employeeID,
		EmployeeName: "Trần Thị B", EmployeeCode: "NV002",
		NetSalary: money(15_000_000),
	})
}

// =========================================================================
// KẾT XUẤT TÀI LIỆU
// =========================================================================

func TestRenderPayslipProducesDownloadableName(t *testing.T) {
	empID := uuid.New()
	h := newHarness(nowAfterAugust())
	_, s := draftPeriod(h, empID)

	got, err := h.uc.RenderPayslip(context.Background(), payActor(empID), s.ID)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if got.FileName != "phieu-luong-NV001-2026-08.html" {
		t.Errorf("tên tệp = %q, muốn phieu-luong-NV001-2026-08.html", got.FileName)
	}
	if !strings.Contains(got.HTML, "Nguyễn Văn A") {
		t.Error("nội dung phiếu thiếu tên nhân viên")
	}
}

// TestRenderPayslipSanitisesFileName: mã nhân viên có dấu gạch chéo sẽ tạo
// ra một đường dẫn thư mục trong header Content-Disposition.
func TestRenderPayslipSanitisesFileName(t *testing.T) {
	empID := uuid.New()
	h := newHarness(nowAfterAugust())
	_, s := draftPeriod(h, empID)
	h.payslips.byID[s.ID].EmployeeCode = "NV/001"

	got, err := h.uc.RenderPayslip(context.Background(), payActor(empID), s.ID)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if strings.Contains(got.FileName, "/") {
		t.Errorf("tên tệp = %q, còn dấu gạch chéo", got.FileName)
	}
}

func TestRenderPayslipOfOtherPersonReturns404(t *testing.T) {
	h := newHarness(nowAfterAugust())
	_, s := draftPeriod(h, uuid.New())

	_, err := h.uc.RenderPayslip(
		context.Background(), payActor(uuid.New()), s.ID)

	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
}

// TestFormatVNDGroupsThousands: phiếu lương in ra cho người đọc, và
// "17250000" là con số không ai đọc được.
func TestFormatVND(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 ₫"},
		{500, "500 ₫"},
		{1_000, "1.000 ₫"},
		{17_250_000, "17.250.000 ₫"},
		{1_000_000_000, "1.000.000.000 ₫"},
		{-2_100_000, "-2.100.000 ₫"},
	}

	for _, tc := range cases {
		if got := FormatVND(money(tc.in)); got != tc.want {
			t.Errorf("FormatVND(%d) = %q, muốn %q", tc.in, got, tc.want)
		}
	}
}
