package payroll

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	domainpay "github.com/PhamVanPhuc2k2/manage/internal/domain/payroll"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
	"github.com/PhamVanPhuc2k2/manage/pkg/logger"
)

func (u *Usecase) ListPeriods(ctx context.Context) ([]*domainpay.Period, error) {
	companyID, err := u.company.CurrentCompanyID(ctx)
	if err != nil {
		return nil, err
	}
	list, err := u.periods.List(ctx, companyID, maxPeriods)
	if err != nil {
		return nil, apperror.Internal(err)
	}
	return list, nil
}

func (u *Usecase) GetPeriod(ctx context.Context, id uuid.UUID) (*domainpay.Period, error) {
	p, err := u.periods.GetByID(ctx, id)
	if err != nil {
		return nil, apperror.NotFound("kỳ lương")
	}
	return p, nil
}

// CreatePeriod tạo kỳ lương cho một tháng.
func (u *Usecase) CreatePeriod(
	ctx context.Context,
	actor *domainauth.Actor,
	year, month int,
	name string,
) (*domainpay.Period, error) {
	if month < 1 || month > 12 {
		return nil, apperror.Invalid("Tháng không hợp lệ", nil)
	}
	if year < 2000 || year > 2200 {
		return nil, apperror.Invalid("Năm không hợp lệ", nil)
	}

	companyID, err := u.company.CurrentCompanyID(ctx)
	if err != nil {
		return nil, err
	}

	loc := u.location(ctx)
	start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, loc)
	end := start.AddDate(0, 1, -1)

	// Không cho tạo kỳ cho tháng CHƯA KẾT THÚC.
	//
	// Dữ liệu công của tháng đang chạy còn thay đổi mỗi ngày, nên bảng lương
	// tính ra sẽ sai ngay khi vừa tính xong. Chặn ở đây thay vì để người
	// dùng tự phát hiện sau khi đã gửi phiếu lương đi.
	if end.After(u.clock.Now().In(loc)) {
		return nil, apperror.Conflict(
			"Tháng này chưa kết thúc. Chỉ tạo được kỳ lương cho tháng đã qua.")
	}

	exists, err := u.periods.ExistsForMonth(ctx, companyID, year, month)
	if err != nil {
		return nil, apperror.Internal(err)
	}
	if exists {
		return nil, apperror.Conflict("Kỳ lương cho tháng này đã tồn tại")
	}

	if strings.TrimSpace(name) == "" {
		name = fmt.Sprintf("Lương tháng %02d/%d", month, year)
	}

	p := &domainpay.Period{
		CompanyID:   companyID,
		Year:        year,
		Month:       month,
		Name:        strings.TrimSpace(name),
		PeriodStart: start,
		PeriodEnd:   end,
		CreatedBy:   actorEmployeeID(actor),
	}
	if err := u.periods.Create(ctx, p); err != nil {
		return nil, apperror.Internal(err)
	}

	return u.periods.GetByID(ctx, p.ID)
}

// RequestCalculation đẩy việc tính lương sang worker.
//
// Không tính ngay trong request HTTP: kỳ lương của công ty vài trăm người
// mất vài giây tới vài chục giây, quá lâu cho một request. Và nếu người
// dùng đóng tab giữa chừng thì kỳ lương nằm lại ở trạng thái 'calculating'
// vĩnh viễn — vì không còn ai chạy tiếp để đổi nó.
func (u *Usecase) RequestCalculation(
	ctx context.Context,
	actor *domainauth.Actor,
	periodID uuid.UUID,
) error {
	p, err := u.periods.GetByID(ctx, periodID)
	if err != nil {
		return apperror.NotFound("kỳ lương")
	}
	if !p.Status.Editable() {
		return apperror.Conflict(
			"Kỳ lương đã khoá. Mở khoá về trạng thái nháp trước khi tính lại.")
	}

	if u.jobs == nil {
		// Không có hàng đợi (ví dụ trong test): chạy thẳng.
		_, err := u.Calculate(ctx, periodID)
		return err
	}

	requestID := ""
	if md, ok := auditMetaFrom(ctx); ok {
		requestID = md.RequestID
	}

	// Đổi trạng thái TRƯỚC khi đẩy việc: giao diện phải thấy ngay là đang
	// chạy, và trạng thái này cũng chặn người thứ hai bấm tính lại.
	p.Status = domainpay.StatusCalculating
	if err := u.periods.Update(ctx, p); err != nil {
		return apperror.Internal(err)
	}

	if err := u.jobs.PublishCalculate(ctx, periodID, requestID); err != nil {
		// Đẩy việc hỏng thì phải trả trạng thái về nháp, nếu không kỳ lương
		// kẹt ở 'calculating' mà không có ai chạy.
		p.Status = domainpay.StatusDraft
		_ = u.periods.Update(ctx, p)
		return apperror.Internal(err)
	}

	u.logAccess(ctx, actor, domainpay.AuditCalculate, "payroll_period", &periodID,
		map[string]any{"year": p.Year, "month": p.Month})

	return nil
}

// CalculateResult tóm tắt một lần chạy máy tính lương.
type CalculateResult struct {
	PeriodID  uuid.UUID
	Processed int
	Skipped   int
	TotalNet  domainpay.Money
}

// Calculate chạy máy tính lương cho cả kỳ.
//
// Chạy trong worker. Toàn bộ phép tính đi qua Calculate() thuần khiết ở
// calculator.go; hàm này chỉ lo việc gom dữ liệu vào và ghi kết quả ra.
//
// Chịu được chạy lại: ReplaceForPeriod xoá sạch phiếu cũ rồi ghi bộ mới
// trong một giao dịch, nên chạy hai lần cho ra đúng một bộ kết quả.
func (u *Usecase) Calculate(
	ctx context.Context,
	periodID uuid.UUID,
) (CalculateResult, error) {
	log := logger.FromContext(ctx)
	res := CalculateResult{PeriodID: periodID}

	p, err := u.periods.GetByID(ctx, periodID)
	if err != nil {
		return res, apperror.NotFound("kỳ lương")
	}
	if p.Status == domainpay.StatusLocked || p.Status.Final() {
		return res, apperror.Conflict("Kỳ lương đã khoá, không tính lại được")
	}

	// Tham số có hiệu lực TẠI KỲ đó, không phải tham số mới nhất. Tính lại
	// kỳ tháng trước phải cho ra đúng con số của tháng trước.
	settings, err := u.settings.Current(ctx, p.CompanyID, p.PeriodEnd)
	if err != nil {
		return res, apperror.New(apperror.KindUnprocessable,
			"Chưa cấu hình tham số tính lương cho công ty")
	}

	employees, err := u.employees.ListActiveIDs(ctx, nil)
	if err != nil {
		return res, apperror.Internal(err)
	}
	if len(employees) == 0 {
		return res, apperror.New(apperror.KindUnprocessable,
			"Không có nhân viên nào đang làm việc")
	}

	// Ba truy vấn cho CẢ kỳ lương, không phải ba truy vấn cho mỗi người.
	structures, err := u.structures.EffectiveOnMany(ctx, employees, p.PeriodEnd)
	if err != nil {
		return res, apperror.Internal(err)
	}
	workdays, err := u.attendance.WorkdaysInPeriod(ctx, employees, p.PeriodStart, p.PeriodEnd)
	if err != nil {
		return res, apperror.Internal(err)
	}

	slips := make([]*domainpay.Payslip, 0, len(employees))
	for _, empID := range employees {
		st := structures[empID]
		if st == nil {
			// Chưa cấu hình lương: BỎ QUA, không tính bừa bằng lương 0.
			//
			// Một phiếu lương 0 đồng trông như đã tính xong và rất dễ lọt
			// qua khâu rà soát; một người vắng mặt khỏi bảng lương thì kế
			// toán nhìn ra ngay.
			res.Skipped++
			log.Warn().Str("employee_id", empID.String()).
				Msg("bỏ qua: chưa cấu hình lương")
			continue
		}

		slip := Calculate(st, workdays[empID], settings)
		slips = append(slips, slip)
		res.TotalNet += slip.NetSalary
		res.Processed++
	}

	if err := u.payslips.ReplaceForPeriod(ctx, periodID, slips); err != nil {
		return res, apperror.Internal(err)
	}
	if err := u.periods.UpdateTotals(ctx, periodID); err != nil {
		return res, apperror.Internal(err)
	}

	now := u.clock.Now()
	p.Status = domainpay.StatusDraft
	p.CalculatedAt = &now
	if err := u.periods.Update(ctx, p); err != nil {
		return res, apperror.Internal(err)
	}

	log.Info().
		Str("period", fmt.Sprintf("%02d/%d", p.Month, p.Year)).
		Int("processed", res.Processed).
		Int("skipped", res.Skipped).
		Msg("đã chạy máy tính lương")

	return res, nil
}

// ChangeStatus chuyển trạng thái kỳ lương theo vòng đời đã định.
func (u *Usecase) ChangeStatus(
	ctx context.Context,
	actor *domainauth.Actor,
	periodID uuid.UUID,
	next domainpay.Status,
) (*domainpay.Period, error) {
	if !next.Valid() {
		return nil, apperror.Invalid("Trạng thái kỳ lương không hợp lệ", nil)
	}

	p, err := u.periods.GetByID(ctx, periodID)
	if err != nil {
		return nil, apperror.NotFound("kỳ lương")
	}
	if !p.Status.CanTransitionTo(next) {
		return nil, apperror.Invalid(
			"Không chuyển được từ \""+statusLabel(p.Status)+
				"\" sang \""+statusLabel(next)+"\"", nil)
	}

	// Không cho khoá kỳ chưa có phiếu lương nào: khoá một kỳ rỗng nghĩa là
	// chốt xong một tháng không trả lương cho ai, và sau khi khoá thì không
	// tính được nữa.
	if next == domainpay.StatusLocked && p.EmployeeCount == 0 {
		return nil, apperror.Conflict(
			"Kỳ lương chưa có phiếu nào. Chạy tính lương trước khi khoá.")
	}

	now := u.clock.Now()
	switch next {
	case domainpay.StatusLocked:
		p.LockedAt = &now
	case domainpay.StatusPaid:
		p.PaidAt = &now
	case domainpay.StatusDraft:
		// Mở khoá: xoá mốc khoá để lịch sử không nói dối.
		p.LockedAt = nil
	}
	p.Status = next

	if err := u.periods.Update(ctx, p); err != nil {
		return nil, apperror.Internal(err)
	}

	action := domainpay.AuditLockPeriod
	if next == domainpay.StatusPaid {
		action = domainpay.AuditMarkPaid
	}
	u.logAccess(ctx, actor, action, "payroll_period", &periodID,
		map[string]any{
			"from": string(p.Status),
			"to":   string(next),
			"year": p.Year, "month": p.Month,
		})

	return u.periods.GetByID(ctx, periodID)
}

func statusLabel(s domainpay.Status) string {
	return map[domainpay.Status]string{
		domainpay.StatusDraft:       "Nháp",
		domainpay.StatusCalculating: "Đang tính",
		domainpay.StatusLocked:      "Đã khoá",
		domainpay.StatusPaid:        "Đã trả",
		domainpay.StatusCancelled:   "Đã huỷ",
	}[s]
}

func actorEmployeeID(actor *domainauth.Actor) *uuid.UUID {
	if actor == nil {
		return nil
	}
	id := actor.EmployeeID
	return &id
}
