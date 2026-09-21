package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	domainpay "github.com/PhamVanPhuc2k2/manage/internal/domain/payroll"
	"github.com/PhamVanPhuc2k2/manage/pkg/postgres"
)

// =========================================================================
// THAM SỐ TÍNH LƯƠNG
// =========================================================================

type PayrollSettingsRepository struct {
	db *postgres.DB
}

func NewPayrollSettingsRepository(db *postgres.DB) *PayrollSettingsRepository {
	return &PayrollSettingsRepository{db: db}
}

func (r *PayrollSettingsRepository) Current(
	ctx context.Context,
	companyID uuid.UUID,
	at time.Time,
) (*domainpay.Settings, error) {
	// Lấy bản có hiệu lực TẠI THỜI ĐIỂM at, không phải bản mới nhất.
	//
	// Tính lại kỳ lương tháng trước phải dùng tham số của tháng trước; dùng
	// tham số vừa sửa hôm qua sẽ cho ra con số khác với phiếu đã phát.
	const q = `
		SELECT id, company_id, personal_deduction, dependent_deduction,
		       social_rate, health_rate, unemployment_rate,
		       employer_social_rate, employer_health_rate, employer_unemployment_rate,
		       COALESCE(social_cap, 0), COALESCE(unemployment_cap, 0),
		       standard_workdays, effective_from, created_at, updated_at
		FROM payroll_settings
		WHERE company_id = $1 AND effective_from <= $2::date
		ORDER BY effective_from DESC
		LIMIT 1`

	var s domainpay.Settings
	err := r.db.QueryRow(ctx, q, companyID, at).Scan(
		&s.ID, &s.CompanyID, &s.PersonalDeduction, &s.DependentDeduction,
		&s.SocialRate, &s.HealthRate, &s.UnemploymentRate,
		&s.EmployerSocialRate, &s.EmployerHealthRate, &s.EmployerUnemploymentRate,
		&s.SocialCap, &s.UnemploymentCap,
		&s.StandardWorkdays, &s.EffectiveFrom, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainpay.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("đọc tham số tính lương: %w", err)
	}

	brackets, err := r.brackets(ctx, s.ID)
	if err != nil {
		return nil, err
	}
	s.Brackets = brackets
	return &s, nil
}

func (r *PayrollSettingsRepository) brackets(
	ctx context.Context,
	settingsID uuid.UUID,
) ([]domainpay.TaxBracket, error) {
	const q = `
		SELECT id, ordinal, from_amount, to_amount, rate
		FROM tax_brackets WHERE settings_id = $1 ORDER BY ordinal`

	rows, err := r.db.Query(ctx, q, settingsID)
	if err != nil {
		return nil, fmt.Errorf("đọc biểu thuế: %w", err)
	}
	defer rows.Close()

	out := make([]domainpay.TaxBracket, 0, 8)
	for rows.Next() {
		var b domainpay.TaxBracket
		if err := rows.Scan(&b.ID, &b.Ordinal, &b.From, &b.To, &b.Rate); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// SaveVersion ghi tham số có hiệu lực từ một ngày, tạo bản mới nếu chưa có.
//
// ON CONFLICT theo (company_id, effective_from): sửa nhiều lần trong CÙNG
// một ngày thì cập nhật đúng bản đó thay vì sinh ra một dãy bản trùng ngày.
// Sang ngày khác là một bản mới, và các kỳ lương cũ vẫn thấy bản cũ.
func (r *PayrollSettingsRepository) SaveVersion(
	ctx context.Context,
	s *domainpay.Settings,
) (uuid.UUID, error) {
	const q = `
		INSERT INTO payroll_settings
		  (company_id, effective_from, personal_deduction, dependent_deduction,
		   social_rate, health_rate, unemployment_rate,
		   employer_social_rate, employer_health_rate, employer_unemployment_rate,
		   social_cap, unemployment_cap, standard_workdays)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULLIF($11,0::bigint),
		        NULLIF($12,0::bigint),$13)
		ON CONFLICT (company_id, effective_from) DO UPDATE SET
		  personal_deduction = EXCLUDED.personal_deduction,
		  dependent_deduction = EXCLUDED.dependent_deduction,
		  social_rate = EXCLUDED.social_rate,
		  health_rate = EXCLUDED.health_rate,
		  unemployment_rate = EXCLUDED.unemployment_rate,
		  employer_social_rate = EXCLUDED.employer_social_rate,
		  employer_health_rate = EXCLUDED.employer_health_rate,
		  employer_unemployment_rate = EXCLUDED.employer_unemployment_rate,
		  social_cap = EXCLUDED.social_cap,
		  unemployment_cap = EXCLUDED.unemployment_cap,
		  standard_workdays = EXCLUDED.standard_workdays
		RETURNING id`

	var id uuid.UUID
	err := r.db.QueryRow(ctx, q, s.CompanyID, s.EffectiveFrom,
		s.PersonalDeduction, s.DependentDeduction,
		s.SocialRate, s.HealthRate, s.UnemploymentRate,
		s.EmployerSocialRate, s.EmployerHealthRate, s.EmployerUnemploymentRate,
		s.SocialCap, s.UnemploymentCap, s.StandardWorkdays).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("ghi tham số tính lương: %w", err)
	}
	return id, nil
}

// ReplaceBrackets thay toàn bộ biểu thuế trong MỘT giao dịch.
//
// Thay cả bộ chứ không sửa từng bậc: biểu thuế chỉ có nghĩa khi đủ và liền
// mạch, và một lần sửa dở dang sẽ để lại khoảng thu nhập không bậc nào phủ —
// thuế của khoảng đó im lặng thành 0.
func (r *PayrollSettingsRepository) ReplaceBrackets(
	ctx context.Context,
	settingsID uuid.UUID,
	brackets []domainpay.TaxBracket,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("mở giao dịch: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`DELETE FROM tax_brackets WHERE settings_id = $1`, settingsID); err != nil {
		return fmt.Errorf("xoá biểu thuế cũ: %w", err)
	}

	for i, b := range brackets {
		if _, err := tx.Exec(ctx, `
			INSERT INTO tax_brackets (settings_id, ordinal, from_amount, to_amount, rate)
			VALUES ($1,$2,$3,$4,$5)`,
			settingsID, i+1, b.From, b.To, b.Rate); err != nil {
			return fmt.Errorf("ghi bậc thuế %d: %w", i+1, err)
		}
	}

	return tx.Commit(ctx)
}

// =========================================================================
// CẤU HÌNH LƯƠNG THEO NHÂN VIÊN
// =========================================================================

type SalaryStructureRepository struct {
	db *postgres.DB
}

func NewSalaryStructureRepository(db *postgres.DB) *SalaryStructureRepository {
	return &SalaryStructureRepository{db: db}
}

const selectStructure = `
SELECT s.id, s.employee_id, s.base_salary, s.insurance_salary, s.dependents,
       COALESCE(s.bank_account,''), COALESCE(s.bank_name,''),
       s.effective_from, s.effective_to, COALESCE(s.note,''),
       s.created_by, s.created_at, s.updated_at,
       COALESCE(e.full_name,''), COALESCE(e.employee_code,''), COALESCE(d.name,'')
FROM salary_structures s
LEFT JOIN employees   e ON e.id = s.employee_id
LEFT JOIN departments d ON d.id = e.department_id
`

func structureTargets(s *domainpay.Structure) []any {
	return []any{
		&s.ID, &s.EmployeeID, &s.BaseSalary, &s.InsuranceSalary, &s.Dependents,
		&s.BankAccount, &s.BankName,
		&s.EffectiveFrom, &s.EffectiveTo, &s.Note,
		&s.CreatedBy, &s.CreatedAt, &s.UpdatedAt,
		&s.EmployeeName, &s.EmployeeCode, &s.DepartmentName,
	}
}

func (r *SalaryStructureRepository) Create(ctx context.Context, s *domainpay.Structure) error {
	const q = `
		INSERT INTO salary_structures
		  (employee_id, base_salary, insurance_salary, dependents,
		   bank_account, bank_name, effective_from, effective_to, note, created_by)
		VALUES ($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,''),$7,$8,NULLIF($9,''),$10)
		RETURNING id, created_at, updated_at`

	err := r.db.QueryRow(ctx, q, s.EmployeeID, s.BaseSalary, s.InsuranceSalary,
		s.Dependents, s.BankAccount, s.BankName, s.EffectiveFrom, s.EffectiveTo,
		s.Note, s.CreatedBy).Scan(&s.ID, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return fmt.Errorf("tạo cấu hình lương: %w", err)
	}
	return nil
}

func (r *SalaryStructureRepository) Update(ctx context.Context, s *domainpay.Structure) error {
	const q = `
		UPDATE salary_structures SET
		  base_salary = $2, insurance_salary = $3, dependents = $4,
		  bank_account = NULLIF($5,''), bank_name = NULLIF($6,''),
		  effective_from = $7, effective_to = $8, note = NULLIF($9,'')
		WHERE id = $1`

	tag, err := r.db.Exec(ctx, q, s.ID, s.BaseSalary, s.InsuranceSalary,
		s.Dependents, s.BankAccount, s.BankName,
		s.EffectiveFrom, s.EffectiveTo, s.Note)
	if err != nil {
		return fmt.Errorf("cập nhật cấu hình lương: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainpay.ErrNotFound
	}
	return nil
}

func (r *SalaryStructureRepository) GetByID(
	ctx context.Context,
	id uuid.UUID,
) (*domainpay.Structure, error) {
	var s domainpay.Structure
	err := r.db.QueryRow(ctx, selectStructure+` WHERE s.id = $1`, id).
		Scan(structureTargets(&s)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainpay.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("đọc cấu hình lương: %w", err)
	}

	items, err := r.components(ctx, []uuid.UUID{s.ID})
	if err != nil {
		return nil, err
	}
	s.Components = items[s.ID]
	return &s, nil
}

func (r *SalaryStructureRepository) EffectiveOn(
	ctx context.Context,
	employeeID uuid.UUID,
	on time.Time,
) (*domainpay.Structure, error) {
	q := selectStructure + `
		WHERE s.employee_id = $1
		  AND s.effective_from <= $2::date
		  AND (s.effective_to IS NULL OR s.effective_to >= $2::date)
		ORDER BY s.effective_from DESC
		LIMIT 1`

	var s domainpay.Structure
	err := r.db.QueryRow(ctx, q, employeeID, on).Scan(structureTargets(&s)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainpay.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("đọc cấu hình lương hiệu lực: %w", err)
	}

	items, err := r.components(ctx, []uuid.UUID{s.ID})
	if err != nil {
		return nil, err
	}
	s.Components = items[s.ID]
	return &s, nil
}

// EffectiveOnMany lấy cấu hình lương của nhiều người trong MỘT lượt.
//
// DISTINCT ON là cách của PostgreSQL để lấy "một dòng mỗi nhóm": nó giữ
// dòng ĐẦU TIÊN của mỗi employee_id theo thứ tự ORDER BY. Viết bằng
// window function hay subquery tương quan đều chạy được nhưng dài hơn và
// chậm hơn đáng kể trên bảng lớn.
func (r *SalaryStructureRepository) EffectiveOnMany(
	ctx context.Context,
	employeeIDs []uuid.UUID,
	on time.Time,
) (map[uuid.UUID]*domainpay.Structure, error) {
	out := make(map[uuid.UUID]*domainpay.Structure, len(employeeIDs))
	if len(employeeIDs) == 0 {
		return out, nil
	}

	q := `
		SELECT DISTINCT ON (s.employee_id)
		       s.id, s.employee_id, s.base_salary, s.insurance_salary, s.dependents,
		       COALESCE(s.bank_account,''), COALESCE(s.bank_name,''),
		       s.effective_from, s.effective_to, COALESCE(s.note,''),
		       s.created_by, s.created_at, s.updated_at,
		       COALESCE(e.full_name,''), COALESCE(e.employee_code,''), COALESCE(d.name,'')
		FROM salary_structures s
		LEFT JOIN employees   e ON e.id = s.employee_id
		LEFT JOIN departments d ON d.id = e.department_id
		WHERE s.employee_id = ANY($1)
		  AND s.effective_from <= $2::date
		  AND (s.effective_to IS NULL OR s.effective_to >= $2::date)
		ORDER BY s.employee_id, s.effective_from DESC`

	rows, err := r.db.Query(ctx, q, employeeIDs, on)
	if err != nil {
		return nil, fmt.Errorf("đọc cấu hình lương hàng loạt: %w", err)
	}
	defer rows.Close()

	ids := make([]uuid.UUID, 0, len(employeeIDs))
	for rows.Next() {
		var s domainpay.Structure
		if err := rows.Scan(structureTargets(&s)...); err != nil {
			return nil, err
		}
		out[s.EmployeeID] = &s
		ids = append(ids, s.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Nạp thành phần lương cho TẤT CẢ trong một truy vấn nữa — tổng cộng
	// hai truy vấn cho cả kỳ lương, thay vì 2N.
	items, err := r.components(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, s := range out {
		s.Components = items[s.ID]
	}
	return out, nil
}

func (r *SalaryStructureRepository) components(
	ctx context.Context,
	structureIDs []uuid.UUID,
) (map[uuid.UUID][]*domainpay.Component, error) {
	out := make(map[uuid.UUID][]*domainpay.Component, len(structureIDs))
	if len(structureIDs) == 0 {
		return out, nil
	}

	const q = `
		SELECT id, structure_id, kind, code, name, amount, taxable, prorated, created_at
		FROM salary_components
		WHERE structure_id = ANY($1)
		ORDER BY kind, code`

	rows, err := r.db.Query(ctx, q, structureIDs)
	if err != nil {
		return nil, fmt.Errorf("đọc thành phần lương: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var c domainpay.Component
		if err := rows.Scan(&c.ID, &c.StructureID, &c.Kind, &c.Code, &c.Name,
			&c.Amount, &c.Taxable, &c.Prorated, &c.CreatedAt); err != nil {
			return nil, err
		}
		out[c.StructureID] = append(out[c.StructureID], &c)
	}
	return out, rows.Err()
}

func (r *SalaryStructureRepository) History(
	ctx context.Context,
	employeeID uuid.UUID,
) ([]*domainpay.Structure, error) {
	q := selectStructure + `
		WHERE s.employee_id = $1 ORDER BY s.effective_from DESC`

	rows, err := r.db.Query(ctx, q, employeeID)
	if err != nil {
		return nil, fmt.Errorf("đọc lịch sử lương: %w", err)
	}
	defer rows.Close()

	out := make([]*domainpay.Structure, 0)
	ids := make([]uuid.UUID, 0)
	for rows.Next() {
		var s domainpay.Structure
		if err := rows.Scan(structureTargets(&s)...); err != nil {
			return nil, err
		}
		out = append(out, &s)
		ids = append(ids, s.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	items, err := r.components(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, s := range out {
		s.Components = items[s.ID]
	}
	return out, nil
}

// CloseOpenEnded đóng khoảng hiệu lực của cấu hình đang để mở.
//
// Gọi trước khi thêm cấu hình mới. Hai dòng cùng để mở sẽ khiến EffectiveOn
// trả về kết quả phụ thuộc thứ tự sắp xếp — tức là không xác định, và lương
// của người đó sẽ nhảy qua lại giữa hai mức mà không ai hiểu vì sao.
func (r *SalaryStructureRepository) CloseOpenEnded(
	ctx context.Context,
	employeeID uuid.UUID,
	before time.Time,
) error {
	const q = `
		UPDATE salary_structures
		SET effective_to = ($2::date - INTERVAL '1 day')::date
		WHERE employee_id = $1 AND effective_to IS NULL AND effective_from < $2::date`

	if _, err := r.db.Exec(ctx, q, employeeID, before); err != nil {
		return fmt.Errorf("đóng cấu hình lương cũ: %w", err)
	}
	return nil
}

func (r *SalaryStructureRepository) ReplaceComponents(
	ctx context.Context,
	structureID uuid.UUID,
	items []*domainpay.Component,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("mở giao dịch: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`DELETE FROM salary_components WHERE structure_id = $1`, structureID); err != nil {
		return fmt.Errorf("xoá thành phần lương cũ: %w", err)
	}

	for _, c := range items {
		if _, err := tx.Exec(ctx, `
			INSERT INTO salary_components
			  (structure_id, kind, code, name, amount, taxable, prorated)
			VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			structureID, c.Kind, c.Code, c.Name, c.Amount, c.Taxable, c.Prorated); err != nil {
			return fmt.Errorf("ghi thành phần lương %s: %w", c.Code, err)
		}
	}

	return tx.Commit(ctx)
}
