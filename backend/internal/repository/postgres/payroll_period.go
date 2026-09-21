package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	domainpay "github.com/PhamVanPhuc2k2/manage/internal/domain/payroll"
	"github.com/PhamVanPhuc2k2/manage/pkg/postgres"
)

// =========================================================================
// KỲ LƯƠNG
// =========================================================================

type PayrollPeriodRepository struct {
	db *postgres.DB
}

func NewPayrollPeriodRepository(db *postgres.DB) *PayrollPeriodRepository {
	return &PayrollPeriodRepository{db: db}
}

const selectPeriod = `
SELECT id, company_id, year, month, name, period_start, period_end, status,
       total_gross, total_net, total_tax, total_insurance, employee_count,
       calculated_at, locked_at, paid_at, created_by, created_at, updated_at
FROM payroll_periods
`

func periodTargets(p *domainpay.Period) []any {
	return []any{
		&p.ID, &p.CompanyID, &p.Year, &p.Month, &p.Name,
		&p.PeriodStart, &p.PeriodEnd, &p.Status,
		&p.TotalGross, &p.TotalNet, &p.TotalTax, &p.TotalInsurance, &p.EmployeeCount,
		&p.CalculatedAt, &p.LockedAt, &p.PaidAt,
		&p.CreatedBy, &p.CreatedAt, &p.UpdatedAt,
	}
}

func (r *PayrollPeriodRepository) Create(ctx context.Context, p *domainpay.Period) error {
	const q = `
		INSERT INTO payroll_periods
		  (company_id, year, month, name, period_start, period_end, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING id, status, created_at, updated_at`

	err := r.db.QueryRow(ctx, q, p.CompanyID, p.Year, p.Month, p.Name,
		p.PeriodStart, p.PeriodEnd, p.CreatedBy).
		Scan(&p.ID, &p.Status, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return fmt.Errorf("tạo kỳ lương: %w", err)
	}
	return nil
}

func (r *PayrollPeriodRepository) Update(ctx context.Context, p *domainpay.Period) error {
	const q = `
		UPDATE payroll_periods SET
		  name = $2, status = $3,
		  calculated_at = $4, locked_at = $5, paid_at = $6
		WHERE id = $1`

	tag, err := r.db.Exec(ctx, q, p.ID, p.Name, p.Status,
		p.CalculatedAt, p.LockedAt, p.PaidAt)
	if err != nil {
		return fmt.Errorf("cập nhật kỳ lương: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainpay.ErrNotFound
	}
	return nil
}

func (r *PayrollPeriodRepository) GetByID(
	ctx context.Context,
	id uuid.UUID,
) (*domainpay.Period, error) {
	var p domainpay.Period
	err := r.db.QueryRow(ctx, selectPeriod+` WHERE id = $1`, id).Scan(periodTargets(&p)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainpay.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("đọc kỳ lương: %w", err)
	}
	return &p, nil
}

func (r *PayrollPeriodRepository) List(
	ctx context.Context,
	companyID uuid.UUID,
	limit int,
) ([]*domainpay.Period, error) {
	q := selectPeriod + `
		WHERE company_id = $1 ORDER BY year DESC, month DESC LIMIT $2`

	rows, err := r.db.Query(ctx, q, companyID, limit)
	if err != nil {
		return nil, fmt.Errorf("liệt kê kỳ lương: %w", err)
	}
	defer rows.Close()

	out := make([]*domainpay.Period, 0, limit)
	for rows.Next() {
		var p domainpay.Period
		if err := rows.Scan(periodTargets(&p)...); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

func (r *PayrollPeriodRepository) ExistsForMonth(
	ctx context.Context,
	companyID uuid.UUID,
	year, month int,
) (bool, error) {
	const q = `
		SELECT EXISTS (
		  SELECT 1 FROM payroll_periods
		  WHERE company_id = $1 AND year = $2 AND month = $3
		    AND status <> 'cancelled')`

	var exists bool
	if err := r.db.QueryRow(ctx, q, companyID, year, month).Scan(&exists); err != nil {
		return false, fmt.Errorf("kiểm tra kỳ lương đã tồn tại: %w", err)
	}
	return exists, nil
}

// UpdateTotals tính lại các số tổng của kỳ từ chính bảng phiếu lương.
//
// Tính từ database chứ không cộng dồn trong Go: nếu ai đó sửa một phiếu
// lương bằng tay (kỳ còn nháp), con số tổng phải theo kịp mà không cần chạy
// lại cả máy tính lương.
func (r *PayrollPeriodRepository) UpdateTotals(ctx context.Context, periodID uuid.UUID) error {
	const q = `
		UPDATE payroll_periods p SET
		  total_gross     = t.gross,
		  total_net       = t.net,
		  total_tax       = t.tax,
		  total_insurance = t.insurance,
		  employee_count  = t.cnt
		FROM (
		  SELECT COALESCE(SUM(gross_salary),0)       AS gross,
		         COALESCE(SUM(net_salary),0)         AS net,
		         COALESCE(SUM(income_tax),0)         AS tax,
		         COALESCE(SUM(insurance_employee),0) AS insurance,
		         COUNT(*)                            AS cnt
		  FROM payslips WHERE period_id = $1
		) t
		WHERE p.id = $1`

	if _, err := r.db.Exec(ctx, q, periodID); err != nil {
		return fmt.Errorf("cập nhật tổng kỳ lương: %w", err)
	}
	return nil
}

// =========================================================================
// NHẬT KÝ TRUY CẬP
// =========================================================================

type AuditRepository struct {
	db *postgres.DB
}

func NewAuditRepository(db *postgres.DB) *AuditRepository {
	return &AuditRepository{db: db}
}

func (r *AuditRepository) Log(ctx context.Context, e *domainpay.AuditEntry) error {
	var detail []byte
	if len(e.Detail) > 0 {
		var err error
		if detail, err = json.Marshal(e.Detail); err != nil {
			return fmt.Errorf("mã hoá chi tiết nhật ký: %w", err)
		}
	}

	const q = `
		INSERT INTO audit_logs
		  (actor_id, action, resource, resource_id, detail, ip, request_id)
		VALUES ($1,$2,$3,$4,$5,NULLIF($6,''),NULLIF($7,''))
		RETURNING id, created_at`

	err := r.db.QueryRow(ctx, q, e.ActorID, e.Action, e.Resource, e.ResourceID,
		detail, e.IP, e.RequestID).Scan(&e.ID, &e.CreatedAt)
	if err != nil {
		return fmt.Errorf("ghi nhật ký truy cập: %w", err)
	}
	return nil
}

func (r *AuditRepository) List(
	ctx context.Context,
	resource string,
	resourceID *uuid.UUID,
	limit int,
) ([]*domainpay.AuditEntry, error) {
	const q = `
		SELECT a.id, a.actor_id, a.action, a.resource, a.resource_id,
		       a.detail, COALESCE(a.ip,''), COALESCE(a.request_id,''), a.created_at,
		       COALESCE(e.full_name,'')
		FROM audit_logs a
		LEFT JOIN employees e ON e.id = a.actor_id
		WHERE ($1::text IS NULL OR a.resource = $1)
		  AND ($2::uuid IS NULL OR a.resource_id = $2)
		ORDER BY a.created_at DESC
		LIMIT $3`

	var res any
	if resource != "" {
		res = resource
	}

	rows, err := r.db.Query(ctx, q, res, resourceID, limit)
	if err != nil {
		return nil, fmt.Errorf("đọc nhật ký truy cập: %w", err)
	}
	defer rows.Close()

	out := make([]*domainpay.AuditEntry, 0, limit)
	for rows.Next() {
		var (
			e   domainpay.AuditEntry
			raw []byte
		)
		if err := rows.Scan(&e.ID, &e.ActorID, &e.Action, &e.Resource, &e.ResourceID,
			&raw, &e.IP, &e.RequestID, &e.CreatedAt, &e.ActorName); err != nil {
			return nil, err
		}
		if len(raw) > 0 {
			// Chi tiết hỏng không được làm hỏng cả phép đọc nhật ký: nhật ký
			// là thứ người ta tìm tới khi đang điều tra sự cố.
			_ = json.Unmarshal(raw, &e.Detail)
		}
		out = append(out, &e)
	}
	return out, rows.Err()
}
