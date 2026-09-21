package payroll

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	domainpay "github.com/PhamVanPhuc2k2/manage/internal/domain/payroll"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
)

type ListPayslipsResult struct {
	Items      []*domainpay.Payslip
	Page       int
	PageSize   int
	TotalItems int
	TotalPages int
}

// ListPayslips trả về bảng lương của một kỳ, đã áp quyền của actor.
func (u *Usecase) ListPayslips(
	ctx context.Context,
	actor *domainauth.Actor,
	f domainpay.PayslipFilter,
) (*ListPayslipsResult, error) {
	// Xoá sạch giá trị phạm vi có thể lọt vào từ query string.
	f.ScopedEmployeeIDs = nil
	f.RestrictScope = false
	f.ScopedEmployeeIDs, f.RestrictScope = scopeFor(actor)

	f.Page, f.PageSize = normalizePage(f.Page, f.PageSize)

	items, total, err := u.payslips.List(ctx, f)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	// Ghi nhật ký MỖI LẦN XEM bảng lương, không chỉ lần sửa.
	//
	// Rò rỉ bảng lương thường là do đọc chứ không phải do ghi, và không có
	// nhật ký đọc thì không bao giờ biết ai đã xem gì.
	if f.RestrictScope == false || len(items) > 1 {
		u.logAccess(ctx, actor, domainpay.AuditListPayroll, "payroll_period", f.PeriodID,
			map[string]any{"count": len(items)})
	}

	return &ListPayslipsResult{
		Items:      items,
		Page:       f.Page,
		PageSize:   f.PageSize,
		TotalItems: total,
		TotalPages: (total + f.PageSize - 1) / f.PageSize,
	}, nil
}

// GetPayslip đọc một phiếu lương, kèm dòng chi tiết.
func (u *Usecase) GetPayslip(
	ctx context.Context,
	actor *domainauth.Actor,
	id uuid.UUID,
) (*domainpay.Payslip, error) {
	s, err := u.payslips.GetByID(ctx, id)
	if err != nil {
		return nil, apperror.NotFound("phiếu lương")
	}
	if err := requireOwnOrAll(actor, s.EmployeeID); err != nil {
		return nil, err
	}

	u.logAccess(ctx, actor, domainpay.AuditViewPayslip, "payslip", &id,
		map[string]any{"employee_id": s.EmployeeID.String()})

	return s, nil
}

// MyPayslips trả về lịch sử phiếu lương của chính actor.
//
// Chỉ gồm kỳ đã KHOÁ hoặc ĐÃ TRẢ — xem ListForEmployee ở repository. Phiếu
// của kỳ còn nháp chưa phải con số cuối cùng, và cho nhân viên thấy nó sẽ
// sinh ra hàng loạt câu hỏi về những con số sắp thay đổi.
func (u *Usecase) MyPayslips(
	ctx context.Context,
	actor *domainauth.Actor,
) ([]*domainpay.Payslip, error) {
	if actor == nil {
		return nil, apperror.New(apperror.KindUnauthorized, "Chưa xác thực")
	}

	list, err := u.payslips.ListForEmployee(ctx, actor.EmployeeID, maxPayslips)
	if err != nil {
		return nil, apperror.Internal(err)
	}
	return list, nil
}

// UpdatePayslip sửa một phiếu lương khi kỳ còn nháp.
//
// Chỉ cho sửa những con số CÓ THỂ có ngoại lệ thật: thưởng, khấu trừ khác
// và ghi chú. Không cho sửa thẳng thuế hay bảo hiểm — chúng là kết quả tính
// từ quy định, và sửa tay sẽ tạo ra phiếu lương không giải thích được.
func (u *Usecase) UpdatePayslip(
	ctx context.Context,
	actor *domainauth.Actor,
	id uuid.UUID,
	bonuses, otherDeductions domainpay.Money,
	note string,
) (*domainpay.Payslip, error) {
	if bonuses < 0 || otherDeductions < 0 {
		return nil, apperror.Invalid("Số tiền không được âm", nil)
	}

	s, err := u.payslips.GetByID(ctx, id)
	if err != nil {
		return nil, apperror.NotFound("phiếu lương")
	}

	p, err := u.periods.GetByID(ctx, s.PeriodID)
	if err != nil {
		return nil, apperror.NotFound("kỳ lương")
	}
	if !p.Status.Editable() {
		return nil, apperror.Conflict("Kỳ lương đã khoá, không sửa được phiếu")
	}

	settings, err := u.settings.Current(ctx, p.CompanyID, p.PeriodEnd)
	if err != nil {
		return nil, apperror.New(apperror.KindUnprocessable,
			"Chưa cấu hình tham số tính lương")
	}

	// Tính LẠI từ đầu với con số thưởng mới.
	//
	// Cộng chênh lệch vào các con số cũ sẽ sai: thưởng tăng thì thu nhập
	// chịu thuế tăng, và thuế phải tính lại theo biểu luỹ tiến chứ không
	// cộng thêm theo tỷ lệ cũ.
	delta := bonuses - s.Bonuses
	s.Bonuses = bonuses
	s.GrossSalary += delta
	s.TaxableIncome = (s.TaxableIncome + delta).NonNegative()
	s.AssessableIncome =
		(s.TaxableIncome - s.PersonalDeduction - s.DependentDeduction).NonNegative()
	s.IncomeTax = settings.CalculateTax(s.AssessableIncome)

	s.OtherDeductions = otherDeductions
	s.NetSalary = s.GrossSalary - s.InsuranceEmployee - s.IncomeTax - s.OtherDeductions
	s.Note = strings.TrimSpace(note)

	if err := u.payslips.Update(ctx, s); err != nil {
		if err == domainpay.ErrLocked {
			return nil, apperror.Conflict("Kỳ lương đã khoá, không sửa được phiếu")
		}
		return nil, apperror.Internal(err)
	}
	if err := u.periods.UpdateTotals(ctx, s.PeriodID); err != nil {
		return nil, apperror.Internal(err)
	}

	u.logAccess(ctx, actor, domainpay.AuditUpdateSalary, "payslip", &id,
		map[string]any{
			"bonuses":          int64(bonuses),
			"other_deductions": int64(otherDeductions),
		})

	return u.payslips.GetByID(ctx, id)
}

// =========================================================================
// BÁO CÁO CHI PHÍ NHÂN SỰ
// =========================================================================

func (u *Usecase) CostByDepartment(
	ctx context.Context,
	actor *domainauth.Actor,
	periodID uuid.UUID,
) ([]*domainpay.CostRow, error) {
	if !canReadAll(actor) {
		return nil, apperror.Forbidden("Bạn không có quyền xem chi phí nhân sự")
	}

	list, err := u.payslips.CostByDepartment(ctx, periodID)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	u.logAccess(ctx, actor, domainpay.AuditListPayroll, "payroll_cost", &periodID, nil)
	return list, nil
}

func (u *Usecase) CostByMonth(
	ctx context.Context,
	actor *domainauth.Actor,
	year int,
) ([]*domainpay.CostRow, error) {
	if !canReadAll(actor) {
		return nil, apperror.Forbidden("Bạn không có quyền xem chi phí nhân sự")
	}
	if year < 2000 || year > 2200 {
		return nil, apperror.Invalid("Năm không hợp lệ", nil)
	}

	companyID, err := u.company.CurrentCompanyID(ctx)
	if err != nil {
		return nil, err
	}

	list, err := u.payslips.CostByMonth(ctx, companyID, year)
	if err != nil {
		return nil, apperror.Internal(err)
	}
	return list, nil
}

// =========================================================================
// GỬI PHIẾU LƯƠNG QUA EMAIL
// =========================================================================

// SendPayslips gửi phiếu lương của cả kỳ qua email.
//
// Chỉ gửi được khi kỳ đã KHOÁ. Gửi phiếu của kỳ còn nháp nghĩa là gửi con
// số sắp thay đổi, và một email đính chính sau đó không bao giờ lấy lại
// được niềm tin đã mất.
func (u *Usecase) SendPayslips(
	ctx context.Context,
	actor *domainauth.Actor,
	periodID uuid.UUID,
	emailOf map[uuid.UUID]string,
) (int, error) {
	if u.mailer == nil {
		return 0, apperror.New(apperror.KindUnprocessable,
			"Chức năng gửi mail chưa được cấu hình")
	}

	p, err := u.periods.GetByID(ctx, periodID)
	if err != nil {
		return 0, apperror.NotFound("kỳ lương")
	}
	if p.Status != domainpay.StatusLocked && p.Status != domainpay.StatusPaid {
		return 0, apperror.Conflict(
			"Chỉ gửi được phiếu lương của kỳ đã khoá")
	}

	f := domainpay.PayslipFilter{PeriodID: &periodID, Page: 1, PageSize: maxPageSize}
	slips, _, err := u.payslips.List(ctx, f)
	if err != nil {
		return 0, apperror.Internal(err)
	}

	sent := 0
	for _, s := range slips {
		email := emailOf[s.EmployeeID]
		if email == "" {
			continue
		}

		// Nạp dòng chi tiết cho từng phiếu: email phải giải thích được từng
		// con số, nếu không nhân viên sẽ hỏi lại kế toán từng người một.
		full, err := u.payslips.GetByID(ctx, s.ID)
		if err != nil {
			continue
		}

		subject := "Phiếu lương " + p.Name
		body := RenderPayslipHTML(full, p)

		if err := u.mailer.SendPayslip(ctx, email, full.EmployeeName, subject, body); err != nil {
			continue
		}
		sent++
	}

	u.logAccess(ctx, actor, domainpay.AuditExportPayroll, "payroll_period", &periodID,
		map[string]any{"sent": sent, "total": len(slips)})

	return sent, nil
}

// PayslipDocument là phiếu lương ở dạng tài liệu in được.
type PayslipDocument struct {
	HTML     string
	FileName string
}

// RenderPayslip dựng tài liệu phiếu lương cho một người.
func (u *Usecase) RenderPayslip(
	ctx context.Context,
	actor *domainauth.Actor,
	id uuid.UUID,
) (*PayslipDocument, error) {
	s, err := u.payslips.GetByID(ctx, id)
	if err != nil {
		return nil, apperror.NotFound("phiếu lương")
	}
	if err := requireOwnOrAll(actor, s.EmployeeID); err != nil {
		return nil, err
	}

	p, err := u.periods.GetByID(ctx, s.PeriodID)
	if err != nil {
		return nil, apperror.NotFound("kỳ lương")
	}

	u.logAccess(ctx, actor, domainpay.AuditViewPayslip, "payslip", &id,
		map[string]any{"format": "document"})

	name := strings.ReplaceAll(s.EmployeeCode, "/", "-")
	if name == "" {
		name = s.EmployeeID.String()[:8]
	}

	return &PayslipDocument{
		HTML:     RenderPayslipHTML(s, p),
		FileName: "phieu-luong-" + name + "-" + periodSlug(p) + ".html",
	}, nil
}

func periodSlug(p *domainpay.Period) string {
	return time.Date(p.Year, time.Month(p.Month), 1, 0, 0, 0, 0, time.UTC).
		Format("2006-01")
}
