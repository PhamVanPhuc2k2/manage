package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	domainatt "github.com/PhamVanPhuc2k2/manage/internal/domain/attendance"
	"github.com/PhamVanPhuc2k2/manage/pkg/postgres"
)

// =========================================================================
// KHUNG GIỜ LÀM VIỆC
// =========================================================================

type ScheduleRepository struct {
	db *postgres.DB
}

func NewScheduleRepository(db *postgres.DB) *ScheduleRepository {
	return &ScheduleRepository{db: db}
}

const selectSchedule = `
SELECT s.id, s.company_id, s.department_id, s.employee_id, s.name,
       s.work_start::text, s.work_end::text, s.workdays,
       s.break_minutes, s.grace_minutes,
       s.effective_from, s.effective_to, s.created_at, s.updated_at
FROM work_schedules s
`

func scanSchedule(row pgx.Row) (*domainatt.Schedule, error) {
	var s domainatt.Schedule
	err := row.Scan(&s.ID, &s.CompanyID, &s.DepartmentID, &s.EmployeeID, &s.Name,
		&s.WorkStart, &s.WorkEnd, &s.Workdays, &s.BreakMinutes, &s.GraceMinutes,
		&s.EffectiveFrom, &s.EffectiveTo, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainatt.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("đọc khung giờ làm việc: %w", err)
	}
	return &s, nil
}

func (r *ScheduleRepository) collect(ctx context.Context, q string, args ...any) (
	[]*domainatt.Schedule, error,
) {
	rows, err := r.db.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("liệt kê khung giờ làm việc: %w", err)
	}
	defer rows.Close()

	out := make([]*domainatt.Schedule, 0)
	for rows.Next() {
		var s domainatt.Schedule
		if err := rows.Scan(&s.ID, &s.CompanyID, &s.DepartmentID, &s.EmployeeID, &s.Name,
			&s.WorkStart, &s.WorkEnd, &s.Workdays, &s.BreakMinutes, &s.GraceMinutes,
			&s.EffectiveFrom, &s.EffectiveTo, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, &s)
	}
	return out, rows.Err()
}

func (r *ScheduleRepository) Create(ctx context.Context, s *domainatt.Schedule) error {
	const q = `
		INSERT INTO work_schedules
		  (company_id, department_id, employee_id, name, work_start, work_end,
		   workdays, break_minutes, grace_minutes, effective_from, effective_to)
		VALUES ($1,$2,$3,$4,$5::time,$6::time,$7,$8,$9,$10,$11)
		RETURNING id, created_at, updated_at`

	err := r.db.QueryRow(ctx, q, s.CompanyID, s.DepartmentID, s.EmployeeID, s.Name,
		s.WorkStart, s.WorkEnd, s.Workdays, s.BreakMinutes, s.GraceMinutes,
		s.EffectiveFrom, s.EffectiveTo).Scan(&s.ID, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return fmt.Errorf("tạo khung giờ làm việc: %w", err)
	}
	return nil
}

func (r *ScheduleRepository) Update(ctx context.Context, s *domainatt.Schedule) error {
	const q = `
		UPDATE work_schedules
		SET name = $2, work_start = $3::time, work_end = $4::time, workdays = $5,
		    break_minutes = $6, grace_minutes = $7,
		    effective_from = $8, effective_to = $9
		WHERE id = $1`

	tag, err := r.db.Exec(ctx, q, s.ID, s.Name, s.WorkStart, s.WorkEnd, s.Workdays,
		s.BreakMinutes, s.GraceMinutes, s.EffectiveFrom, s.EffectiveTo)
	if err != nil {
		return fmt.Errorf("cập nhật khung giờ làm việc: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainatt.ErrNotFound
	}
	return nil
}

func (r *ScheduleRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM work_schedules WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("xoá khung giờ làm việc: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainatt.ErrNotFound
	}
	return nil
}

func (r *ScheduleRepository) GetByID(ctx context.Context, id uuid.UUID) (
	*domainatt.Schedule, error,
) {
	return scanSchedule(r.db.QueryRow(ctx, selectSchedule+` WHERE s.id = $1`, id))
}

func (r *ScheduleRepository) List(ctx context.Context, companyID uuid.UUID) (
	[]*domainatt.Schedule, error,
) {
	q := selectSchedule + `
		WHERE s.company_id = $1
		ORDER BY
		  CASE WHEN s.employee_id IS NOT NULL THEN 0
		       WHEN s.department_id IS NOT NULL THEN 1 ELSE 2 END,
		  s.name`
	return r.collect(ctx, q, companyID)
}

// Applicable trả về mọi khung giờ có thể áp dụng cho một nhân viên vào một
// ngày: của chính họ, của phòng họ, và của công ty.
func (r *ScheduleRepository) Applicable(
	ctx context.Context,
	employeeID uuid.UUID,
	on time.Time,
) ([]*domainatt.Schedule, error) {
	q := selectSchedule + `
		JOIN employees e ON e.id = $1
		WHERE s.company_id = e.company_id
		  AND s.effective_from <= $2::date
		  AND (s.effective_to IS NULL OR s.effective_to >= $2::date)
		  AND (
		        (s.employee_id IS NULL AND s.department_id IS NULL)
		     OR  s.employee_id = e.id
		     OR (s.department_id IS NOT NULL AND s.department_id = e.department_id)
		  )`
	return r.collect(ctx, q, employeeID, on)
}

// =========================================================================
// NGÀY LỄ
// =========================================================================

type HolidayRepository struct {
	db *postgres.DB
}

func NewHolidayRepository(db *postgres.DB) *HolidayRepository {
	return &HolidayRepository{db: db}
}

func (r *HolidayRepository) Create(ctx context.Context, h *domainatt.Holiday) error {
	const q = `
		INSERT INTO holidays (company_id, holiday_date, name, is_paid)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (company_id, holiday_date)
		DO UPDATE SET name = EXCLUDED.name, is_paid = EXCLUDED.is_paid
		RETURNING id, created_at`

	err := r.db.QueryRow(ctx, q, h.CompanyID, h.Date, h.Name, h.IsPaid).
		Scan(&h.ID, &h.CreatedAt)
	if err != nil {
		return fmt.Errorf("tạo ngày lễ: %w", err)
	}
	return nil
}

func (r *HolidayRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM holidays WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("xoá ngày lễ: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainatt.ErrNotFound
	}
	return nil
}

func (r *HolidayRepository) ListBetween(
	ctx context.Context,
	companyID uuid.UUID,
	from, to time.Time,
) ([]*domainatt.Holiday, error) {
	const q = `
		SELECT id, company_id, holiday_date, name, is_paid, created_at
		FROM holidays
		WHERE company_id = $1 AND holiday_date BETWEEN $2 AND $3
		ORDER BY holiday_date`

	rows, err := r.db.Query(ctx, q, companyID, from, to)
	if err != nil {
		return nil, fmt.Errorf("liệt kê ngày lễ: %w", err)
	}
	defer rows.Close()

	out := make([]*domainatt.Holiday, 0)
	for rows.Next() {
		var h domainatt.Holiday
		if err := rows.Scan(&h.ID, &h.CompanyID, &h.Date, &h.Name,
			&h.IsPaid, &h.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &h)
	}
	return out, rows.Err()
}
