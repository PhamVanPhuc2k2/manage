package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	domainpay "github.com/PhamVanPhuc2k2/manage/internal/domain/payroll"
	"github.com/PhamVanPhuc2k2/manage/pkg/postgres"
)

type PayslipRepository struct {
	db *postgres.DB
}

func NewPayslipRepository(db *postgres.DB) *PayslipRepository {
	return &PayslipRepository{db: db}
}

const selectPayslip = `
SELECT ps.id, ps.period_id, ps.employee_id,
       ps.standard_workdays, ps.actual_workdays, ps.leave_days, ps.absent_days,
       ps.base_salary, ps.allowances, ps.bonuses, ps.gross_salary,
       ps.insurance_base, ps.insurance_employee, ps.insurance_employer,
       ps.taxable_income, ps.personal_deduction, ps.dependent_deduction,
       ps.assessable_income, ps.income_tax,
       ps.other_deductions, ps.net_salary, ps.dependents, COALESCE(ps.note,''),
       ps.created_at, ps.updated_at,
       COALESCE(e.full_name,''), COALESCE(e.employee_code,''),
       COALESCE(d.name,''), COALESCE(pos.name,''),
       COALESCE(p.name,''), p.year, p.month
FROM payslips ps
LEFT JOIN employees      e   ON e.id = ps.employee_id
LEFT JOIN departments    d   ON d.id = e.department_id
LEFT JOIN positions      pos ON pos.id = e.position_id
LEFT JOIN payroll_periods p  ON p.id = ps.period_id
`

func payslipTargets(s *domainpay.Payslip) []any {
	return []any{
		&s.ID, &s.PeriodID, &s.EmployeeID,
		&s.StandardWorkdays, &s.ActualWorkdays, &s.LeaveDays, &s.AbsentDays,
		&s.BaseSalary, &s.Allowances, &s.Bonuses, &s.GrossSalary,
		&s.InsuranceBase, &s.InsuranceEmployee, &s.InsuranceEmployer,
		&s.TaxableIncome, &s.PersonalDeduction, &s.DependentDeduction,
		&s.AssessableIncome, &s.IncomeTax,
		&s.OtherDeductions, &s.NetSalary, &s.Dependents, &s.Note,
		&s.CreatedAt, &s.UpdatedAt,
		&s.EmployeeName, &s.EmployeeCode, &s.DepartmentName, &s.PositionName,
		&s.PeriodName, &s.PeriodYear, &s.PeriodMonth,
	}
}

// ReplaceForPeriod xoá sạch phiếu cũ của kỳ rồi ghi bộ mới, trong MỘT giao dịch.
//
// Chạy lại máy tính lương phải cho ra kết quả sạch. Xoá rồi chèn ở hai giao
// dịch riêng sẽ để lại một khoảng thời gian kỳ lương rỗng, và nếu bước chèn
// hỏng giữa chừng thì mất trắng — đúng lúc kế toán đang chờ số.
func (r *PayslipRepository) ReplaceForPeriod(
	ctx context.Context,
	periodID uuid.UUID,
	slips []*domainpay.Payslip,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("mở giao dịch: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// payslip_items tự dọn theo nhờ ON DELETE CASCADE.
	if _, err := tx.Exec(ctx,
		`DELETE FROM payslips WHERE period_id = $1`, periodID); err != nil {
		return fmt.Errorf("xoá phiếu lương cũ: %w", err)
	}

	for _, s := range slips {
		var id uuid.UUID
		err := tx.QueryRow(ctx, `
			INSERT INTO payslips
			  (period_id, employee_id, standard_workdays, actual_workdays,
			   leave_days, absent_days, base_salary, allowances, bonuses,
			   gross_salary, insurance_base, insurance_employee, insurance_employer,
			   taxable_income, personal_deduction, dependent_deduction,
			   assessable_income, income_tax, other_deductions, net_salary,
			   dependents, note)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,
			        $17,$18,$19,$20,$21,NULLIF($22,''))
			RETURNING id`,
			periodID, s.EmployeeID, s.StandardWorkdays, s.ActualWorkdays,
			s.LeaveDays, s.AbsentDays, s.BaseSalary, s.Allowances, s.Bonuses,
			s.GrossSalary, s.InsuranceBase, s.InsuranceEmployee, s.InsuranceEmployer,
			s.TaxableIncome, s.PersonalDeduction, s.DependentDeduction,
			s.AssessableIncome, s.IncomeTax, s.OtherDeductions, s.NetSalary,
			s.Dependents, s.Note).Scan(&id)
		if err != nil {
			return fmt.Errorf("ghi phiếu lương: %w", err)
		}
		s.ID = id

		for i, it := range s.Items {
			if _, err := tx.Exec(ctx, `
				INSERT INTO payslip_items
				  (payslip_id, kind, code, name, amount, taxable, sort_order)
				VALUES ($1,$2,$3,$4,$5,$6,$7)`,
				id, it.Kind, it.Code, it.Name, it.Amount, it.Taxable, i); err != nil {
				return fmt.Errorf("ghi dòng phiếu lương %s: %w", it.Code, err)
			}
		}
	}

	return tx.Commit(ctx)
}

func (r *PayslipRepository) loadItems(
	ctx context.Context,
	payslipIDs []uuid.UUID,
) (map[uuid.UUID][]*domainpay.PayslipItem, error) {
	out := make(map[uuid.UUID][]*domainpay.PayslipItem, len(payslipIDs))
	if len(payslipIDs) == 0 {
		return out, nil
	}

	const q = `
		SELECT id, payslip_id, kind, code, name, amount, taxable, sort_order, created_at
		FROM payslip_items WHERE payslip_id = ANY($1) ORDER BY payslip_id, sort_order`

	rows, err := r.db.Query(ctx, q, payslipIDs)
	if err != nil {
		return nil, fmt.Errorf("đọc dòng phiếu lương: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var it domainpay.PayslipItem
		if err := rows.Scan(&it.ID, &it.PayslipID, &it.Kind, &it.Code, &it.Name,
			&it.Amount, &it.Taxable, &it.SortOrder, &it.CreatedAt); err != nil {
			return nil, err
		}
		out[it.PayslipID] = append(out[it.PayslipID], &it)
	}
	return out, rows.Err()
}

func (r *PayslipRepository) one(ctx context.Context, q string, args ...any) (
	*domainpay.Payslip, error,
) {
	var s domainpay.Payslip
	err := r.db.QueryRow(ctx, q, args...).Scan(payslipTargets(&s)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainpay.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("đọc phiếu lương: %w", err)
	}

	items, err := r.loadItems(ctx, []uuid.UUID{s.ID})
	if err != nil {
		return nil, err
	}
	s.Items = items[s.ID]

	// Số tài khoản lấy từ cấu hình lương có hiệu lực, không lưu vào phiếu.
	//
	// Số tài khoản có thể đổi sau khi trả lương; phiếu cũ nên hiện số hiện
	// tại chứ không phải số đã lỗi thời. Và không lưu lại nghĩa là một bản
	// sao ít đi — dữ liệu nhạy cảm càng ít chỗ càng tốt.
	const bankQ = `
		SELECT COALESCE(bank_account,''), COALESCE(bank_name,'')
		FROM salary_structures
		WHERE employee_id = $1
		ORDER BY effective_from DESC LIMIT 1`

	var account, bank string
	if err := r.db.QueryRow(ctx, bankQ, s.EmployeeID).Scan(&account, &bank); err == nil {
		s.BankAccountMasked = maskAccount(account)
		s.BankName = bank
	}

	return &s, nil
}

// maskAccount chỉ giữ 4 số cuối.
//
// Che ở TẦNG REPOSITORY, không ở handler: số đầy đủ không bao giờ đi vào
// entity, nên không có đường nào để nó lọt ra JSON do sơ suất sau này.
func maskAccount(v string) string {
	if v == "" {
		return ""
	}
	if len(v) <= 4 {
		return "****"
	}
	return "****" + v[len(v)-4:]
}

func (r *PayslipRepository) GetByID(
	ctx context.Context,
	id uuid.UUID,
) (*domainpay.Payslip, error) {
	return r.one(ctx, selectPayslip+` WHERE ps.id = $1`, id)
}

func (r *PayslipRepository) GetForEmployee(
	ctx context.Context,
	periodID, employeeID uuid.UUID,
) (*domainpay.Payslip, error) {
	return r.one(ctx,
		selectPayslip+` WHERE ps.period_id = $1 AND ps.employee_id = $2`,
		periodID, employeeID)
}

func (r *PayslipRepository) List(
	ctx context.Context,
	f domainpay.PayslipFilter,
) ([]*domainpay.Payslip, int, error) {
	scoped := f.ScopedEmployeeIDs
	if scoped == nil {
		scoped = []uuid.UUID{}
	}

	const where = `
		WHERE ($1::uuid IS NULL OR ps.period_id = $1)
		  AND ($2::uuid IS NULL OR ps.employee_id = $2)
		  AND ($3::uuid IS NULL OR e.department_id = $3)
		  AND (NOT $4::bool OR ps.employee_id = ANY($5::uuid[]))`

	args := []any{f.PeriodID, f.EmployeeID, f.DepartmentID, f.RestrictScope, scoped}

	var total int
	countQ := `
		SELECT COUNT(*) FROM payslips ps
		LEFT JOIN employees e ON e.id = ps.employee_id` + where
	if err := r.db.QueryRow(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("đếm phiếu lương: %w", err)
	}

	q := selectPayslip + where + ` ORDER BY e.full_name LIMIT $6 OFFSET $7`
	offset := (f.Page - 1) * f.PageSize

	rows, err := r.db.Query(ctx, q, append(args, f.PageSize, offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("liệt kê phiếu lương: %w", err)
	}
	defer rows.Close()

	out := make([]*domainpay.Payslip, 0, f.PageSize)
	for rows.Next() {
		var s domainpay.Payslip
		if err := rows.Scan(payslipTargets(&s)...); err != nil {
			return nil, 0, err
		}
		out = append(out, &s)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	// Danh sách KHÔNG nạp dòng chi tiết: bảng lương hiển thị con số tổng,
	// và nạp chi tiết cho 200 phiếu chỉ để không hiển thị là lãng phí.
	return out, total, nil
}

func (r *PayslipRepository) ListForEmployee(
	ctx context.Context,
	employeeID uuid.UUID,
	limit int,
) ([]*domainpay.Payslip, error) {
	q := selectPayslip + `
		WHERE ps.employee_id = $1 AND p.status IN ('locked','paid')
		ORDER BY p.year DESC, p.month DESC
		LIMIT $2`

	rows, err := r.db.Query(ctx, q, employeeID, limit)
	if err != nil {
		return nil, fmt.Errorf("liệt kê phiếu lương cá nhân: %w", err)
	}
	defer rows.Close()

	out := make([]*domainpay.Payslip, 0, limit)
	for rows.Next() {
		var s domainpay.Payslip
		if err := rows.Scan(payslipTargets(&s)...); err != nil {
			return nil, err
		}
		out = append(out, &s)
	}
	return out, rows.Err()
}

func (r *PayslipRepository) Update(ctx context.Context, s *domainpay.Payslip) error {
	// Chỉ cho sửa khi kỳ còn NHÁP. Điều kiện nằm trong chính câu lệnh chứ
	// không kiểm tra ở tầng trên: đây là hàng rào cuối cùng bảo vệ số liệu
	// lương đã chốt, và nó phải đúng kể cả khi có đường gọi nào đó quên
	// kiểm tra.
	const q = `
		UPDATE payslips ps SET
		  base_salary = $2, allowances = $3, bonuses = $4, gross_salary = $5,
		  insurance_employee = $6, taxable_income = $7, assessable_income = $8,
		  income_tax = $9, other_deductions = $10, net_salary = $11,
		  note = NULLIF($12,'')
		FROM payroll_periods p
		WHERE ps.id = $1 AND p.id = ps.period_id AND p.status = 'draft'`

	tag, err := r.db.Exec(ctx, q, s.ID, s.BaseSalary, s.Allowances, s.Bonuses,
		s.GrossSalary, s.InsuranceEmployee, s.TaxableIncome, s.AssessableIncome,
		s.IncomeTax, s.OtherDeductions, s.NetSalary, s.Note)
	if err != nil {
		return fmt.Errorf("cập nhật phiếu lương: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Không có dòng nào đổi = phiếu không tồn tại HOẶC kỳ đã khoá.
		// Trả ErrLocked vì đó là nguyên nhân gần như chắc chắn, và thông báo
		// "kỳ đã khoá" hữu ích hơn nhiều so với "không tìm thấy".
		return domainpay.ErrLocked
	}
	return nil
}

// =========================================================================
// BÁO CÁO CHI PHÍ NHÂN SỰ
// =========================================================================

func (r *PayslipRepository) CostByDepartment(
	ctx context.Context,
	periodID uuid.UUID,
) ([]*domainpay.CostRow, error) {
	const q = `
		SELECT COALESCE(d.id::text, 'none'), COALESCE(d.name, 'Chưa có phòng ban'),
		       COUNT(*),
		       COALESCE(SUM(ps.gross_salary),0),
		       COALESCE(SUM(ps.net_salary),0),
		       COALESCE(SUM(ps.income_tax),0),
		       COALESCE(SUM(ps.insurance_employee),0),
		       COALESCE(SUM(ps.insurance_employer),0)
		FROM payslips ps
		LEFT JOIN employees   e ON e.id = ps.employee_id
		LEFT JOIN departments d ON d.id = e.department_id
		WHERE ps.period_id = $1
		GROUP BY d.id, d.name
		ORDER BY SUM(ps.gross_salary) DESC`

	return r.costRows(ctx, q, periodID)
}

func (r *PayslipRepository) CostByMonth(
	ctx context.Context,
	companyID uuid.UUID,
	year int,
) ([]*domainpay.CostRow, error) {
	const q = `
		SELECT to_char(make_date(p.year, p.month, 1), 'YYYY-MM'),
		       'Tháng ' || p.month::text,
		       COUNT(*),
		       COALESCE(SUM(ps.gross_salary),0),
		       COALESCE(SUM(ps.net_salary),0),
		       COALESCE(SUM(ps.income_tax),0),
		       COALESCE(SUM(ps.insurance_employee),0),
		       COALESCE(SUM(ps.insurance_employer),0)
		FROM payslips ps
		JOIN payroll_periods p ON p.id = ps.period_id
		WHERE p.company_id = $1 AND p.year = $2 AND p.status <> 'cancelled'
		GROUP BY p.year, p.month
		ORDER BY p.month`

	return r.costRows(ctx, q, companyID, year)
}

func (r *PayslipRepository) costRows(ctx context.Context, q string, args ...any) (
	[]*domainpay.CostRow, error,
) {
	rows, err := r.db.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("thống kê chi phí nhân sự: %w", err)
	}
	defer rows.Close()

	out := make([]*domainpay.CostRow, 0)
	for rows.Next() {
		var c domainpay.CostRow
		if err := rows.Scan(&c.Key, &c.Label, &c.EmployeeCount,
			&c.TotalGross, &c.TotalNet, &c.TotalTax,
			&c.InsuranceEmployee, &c.InsuranceEmployer); err != nil {
			return nil, err
		}
		out = append(out, &c)
	}
	return out, rows.Err()
}
