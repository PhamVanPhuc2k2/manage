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
// YÊU CẦU ĐIỀU CHỈNH CÔNG
// =========================================================================

type AdjustmentRepository struct {
	db *postgres.DB
}

func NewAdjustmentRepository(db *postgres.DB) *AdjustmentRepository {
	return &AdjustmentRepository{db: db}
}

const selectAdjustment = `
SELECT a.id, a.employee_id, a.work_date, a.requested_start, a.requested_end,
       a.reason, a.status, a.approver_id, a.decided_at,
       COALESCE(a.decision_note,''), a.created_at, a.updated_at,
       COALESCE(e.full_name,''), COALESCE(ap.full_name,'')
FROM attendance_adjustments a
LEFT JOIN employees e  ON e.id  = a.employee_id
LEFT JOIN employees ap ON ap.id = a.approver_id
`

func scanAdjustment(row pgx.Row) (*domainatt.Adjustment, error) {
	var a domainatt.Adjustment
	err := row.Scan(&a.ID, &a.EmployeeID, &a.WorkDate, &a.RequestedStart,
		&a.RequestedEnd, &a.Reason, &a.Status, &a.ApproverID, &a.DecidedAt,
		&a.DecisionNote, &a.CreatedAt, &a.UpdatedAt, &a.EmployeeName, &a.ApproverName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainatt.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("đọc yêu cầu điều chỉnh: %w", err)
	}
	return &a, nil
}

func (r *AdjustmentRepository) Create(ctx context.Context, a *domainatt.Adjustment) error {
	const q = `
		INSERT INTO attendance_adjustments
		  (employee_id, work_date, requested_start, requested_end, reason)
		VALUES ($1,$2,$3,$4,$5)
		RETURNING id, status, created_at, updated_at`

	err := r.db.QueryRow(ctx, q, a.EmployeeID, a.WorkDate, a.RequestedStart,
		a.RequestedEnd, a.Reason).
		Scan(&a.ID, &a.Status, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return fmt.Errorf("tạo yêu cầu điều chỉnh: %w", err)
	}
	return nil
}

func (r *AdjustmentRepository) Update(ctx context.Context, a *domainatt.Adjustment) error {
	const q = `
		UPDATE attendance_adjustments
		SET status = $2, approver_id = $3, decided_at = $4,
		    decision_note = NULLIF($5,'')
		WHERE id = $1`

	tag, err := r.db.Exec(ctx, q, a.ID, a.Status, a.ApproverID, a.DecidedAt, a.DecisionNote)
	if err != nil {
		return fmt.Errorf("cập nhật yêu cầu điều chỉnh: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainatt.ErrNotFound
	}
	return nil
}

func (r *AdjustmentRepository) GetByID(ctx context.Context, id uuid.UUID) (
	*domainatt.Adjustment, error,
) {
	return scanAdjustment(r.db.QueryRow(ctx, selectAdjustment+` WHERE a.id = $1`, id))
}

func (r *AdjustmentRepository) List(
	ctx context.Context,
	employeeIDs []uuid.UUID,
	restrict bool,
	status *domainatt.ApprovalStatus,
) ([]*domainatt.Adjustment, error) {
	ids := employeeIDs
	if ids == nil {
		ids = []uuid.UUID{}
	}

	q := selectAdjustment + `
		WHERE (NOT $1::bool OR a.employee_id = ANY($2::uuid[]))
		  AND ($3::approval_status IS NULL OR a.status = $3)
		ORDER BY a.created_at DESC
		LIMIT 200`

	rows, err := r.db.Query(ctx, q, restrict, ids, status)
	if err != nil {
		return nil, fmt.Errorf("liệt kê yêu cầu điều chỉnh: %w", err)
	}
	defer rows.Close()

	out := make([]*domainatt.Adjustment, 0)
	for rows.Next() {
		var a domainatt.Adjustment
		if err := rows.Scan(&a.ID, &a.EmployeeID, &a.WorkDate, &a.RequestedStart,
			&a.RequestedEnd, &a.Reason, &a.Status, &a.ApproverID, &a.DecidedAt,
			&a.DecisionNote, &a.CreatedAt, &a.UpdatedAt,
			&a.EmployeeName, &a.ApproverName); err != nil {
			return nil, err
		}
		out = append(out, &a)
	}
	return out, rows.Err()
}

// =========================================================================
// ĐƠN NGHỈ PHÉP
// =========================================================================

type LeaveRepository struct {
	db *postgres.DB
}

func NewLeaveRepository(db *postgres.DB) *LeaveRepository {
	return &LeaveRepository{db: db}
}

const selectLeave = `
SELECT l.id, l.employee_id, l.leave_type, l.start_date, l.end_date,
       l.day_part, l.days, COALESCE(l.reason,''), l.status,
       l.approver_id, l.decided_at, COALESCE(l.decision_note,''),
       l.created_at, l.updated_at,
       COALESCE(e.full_name,''), COALESCE(ap.full_name,'')
FROM leave_requests l
LEFT JOIN employees e  ON e.id  = l.employee_id
LEFT JOIN employees ap ON ap.id = l.approver_id
`

func scanLeaveTargets(l *domainatt.LeaveRequest) []any {
	return []any{
		&l.ID, &l.EmployeeID, &l.Type, &l.StartDate, &l.EndDate,
		&l.DayPart, &l.Days, &l.Reason, &l.Status,
		&l.ApproverID, &l.DecidedAt, &l.DecisionNote,
		&l.CreatedAt, &l.UpdatedAt, &l.EmployeeName, &l.ApproverName,
	}
}

func (r *LeaveRepository) Create(ctx context.Context, l *domainatt.LeaveRequest) error {
	const q = `
		INSERT INTO leave_requests
		  (employee_id, leave_type, start_date, end_date, day_part, days, reason)
		VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''))
		RETURNING id, status, created_at, updated_at`

	err := r.db.QueryRow(ctx, q, l.EmployeeID, l.Type, l.StartDate, l.EndDate,
		l.DayPart, l.Days, l.Reason).
		Scan(&l.ID, &l.Status, &l.CreatedAt, &l.UpdatedAt)
	if err != nil {
		return fmt.Errorf("tạo đơn nghỉ phép: %w", err)
	}
	return nil
}

func (r *LeaveRepository) Update(ctx context.Context, l *domainatt.LeaveRequest) error {
	const q = `
		UPDATE leave_requests
		SET status = $2, approver_id = $3, decided_at = $4,
		    decision_note = NULLIF($5,'')
		WHERE id = $1`

	tag, err := r.db.Exec(ctx, q, l.ID, l.Status, l.ApproverID, l.DecidedAt, l.DecisionNote)
	if err != nil {
		return fmt.Errorf("cập nhật đơn nghỉ phép: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainatt.ErrNotFound
	}
	return nil
}

func (r *LeaveRepository) GetByID(ctx context.Context, id uuid.UUID) (
	*domainatt.LeaveRequest, error,
) {
	var l domainatt.LeaveRequest
	err := r.db.QueryRow(ctx, selectLeave+` WHERE l.id = $1`, id).
		Scan(scanLeaveTargets(&l)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainatt.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("đọc đơn nghỉ phép: %w", err)
	}
	return &l, nil
}

func (r *LeaveRepository) List(
	ctx context.Context,
	f domainatt.LeaveFilter,
) ([]*domainatt.LeaveRequest, int, error) {
	scoped := f.ScopedEmployeeIDs
	if scoped == nil {
		scoped = []uuid.UUID{}
	}

	const where = `
		WHERE ($1::uuid IS NULL OR l.employee_id = $1)
		  AND ($2::approval_status IS NULL OR l.status = $2)
		  AND ($3::leave_type IS NULL OR l.leave_type = $3)
		  AND ($4::date IS NULL OR l.end_date   >= $4)
		  AND ($5::date IS NULL OR l.start_date <= $5)
		  AND (NOT $6::bool OR l.employee_id = ANY($7::uuid[]))`

	args := []any{f.EmployeeID, f.Status, f.Type, f.From, f.To, f.RestrictScope, scoped}

	var total int
	countQ := `SELECT COUNT(*) FROM leave_requests l` + where
	if err := r.db.QueryRow(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("đếm đơn nghỉ phép: %w", err)
	}

	q := selectLeave + where + ` ORDER BY l.created_at DESC LIMIT $8 OFFSET $9`
	offset := (f.Page - 1) * f.PageSize

	rows, err := r.db.Query(ctx, q, append(args, f.PageSize, offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("liệt kê đơn nghỉ phép: %w", err)
	}
	defer rows.Close()

	out := make([]*domainatt.LeaveRequest, 0, f.PageSize)
	for rows.Next() {
		var l domainatt.LeaveRequest
		if err := rows.Scan(scanLeaveTargets(&l)...); err != nil {
			return nil, 0, err
		}
		out = append(out, &l)
	}
	return out, total, rows.Err()
}

func (r *LeaveRepository) Overlapping(
	ctx context.Context,
	employeeID uuid.UUID,
	from, to time.Time,
	excludeID *uuid.UUID,
) ([]*domainatt.LeaveRequest, error) {
	// Chỉ xét đơn đang chờ và đã duyệt. Đơn bị từ chối hay đã huỷ không
	// chiếm chỗ, và chặn vì chúng sẽ khiến người dùng không gửi lại được sau
	// khi sửa.
	q := selectLeave + `
		WHERE l.employee_id = $1
		  AND l.status IN ('pending','approved')
		  AND l.start_date <= $3 AND l.end_date >= $2
		  AND ($4::uuid IS NULL OR l.id <> $4)`

	rows, err := r.db.Query(ctx, q, employeeID, from, to, excludeID)
	if err != nil {
		return nil, fmt.Errorf("kiểm tra đơn trùng ngày: %w", err)
	}
	defer rows.Close()

	out := make([]*domainatt.LeaveRequest, 0)
	for rows.Next() {
		var l domainatt.LeaveRequest
		if err := rows.Scan(scanLeaveTargets(&l)...); err != nil {
			return nil, err
		}
		out = append(out, &l)
	}
	return out, rows.Err()
}

func (r *LeaveRepository) ApprovedOn(
	ctx context.Context,
	day time.Time,
) (map[uuid.UUID]*domainatt.LeaveRequest, error) {
	q := selectLeave + `
		WHERE l.status = 'approved'
		  AND l.start_date <= $1::date AND l.end_date >= $1::date`

	rows, err := r.db.Query(ctx, q, day)
	if err != nil {
		return nil, fmt.Errorf("liệt kê đơn nghỉ đã duyệt: %w", err)
	}
	defer rows.Close()

	out := make(map[uuid.UUID]*domainatt.LeaveRequest)
	for rows.Next() {
		var l domainatt.LeaveRequest
		if err := rows.Scan(scanLeaveTargets(&l)...); err != nil {
			return nil, err
		}
		out[l.EmployeeID] = &l
	}
	return out, rows.Err()
}

// =========================================================================
// QUỸ NGÀY PHÉP
// =========================================================================

type BalanceRepository struct {
	db *postgres.DB
}

func NewBalanceRepository(db *postgres.DB) *BalanceRepository {
	return &BalanceRepository{db: db}
}

func (r *BalanceRepository) Get(
	ctx context.Context,
	employeeID uuid.UUID,
	year int,
) (*domainatt.Balance, error) {
	const q = `
		SELECT b.employee_id, b.year, b.entitled_days, b.carried_over_days,
		       b.used_days, b.created_at, b.updated_at, COALESCE(e.full_name,'')
		FROM leave_balances b
		LEFT JOIN employees e ON e.id = b.employee_id
		WHERE b.employee_id = $1 AND b.year = $2`

	var b domainatt.Balance
	err := r.db.QueryRow(ctx, q, employeeID, year).Scan(&b.EmployeeID, &b.Year,
		&b.EntitledDays, &b.CarriedOverDays, &b.UsedDays,
		&b.CreatedAt, &b.UpdatedAt, &b.EmployeeName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainatt.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("đọc quỹ phép: %w", err)
	}
	return &b, nil
}

func (r *BalanceRepository) Upsert(ctx context.Context, b *domainatt.Balance) error {
	const q = `
		INSERT INTO leave_balances
		  (employee_id, year, entitled_days, carried_over_days, used_days)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (employee_id, year) DO UPDATE SET
		  entitled_days     = EXCLUDED.entitled_days,
		  carried_over_days = EXCLUDED.carried_over_days
		RETURNING used_days, created_at, updated_at`

	// used_days KHÔNG nằm trong DO UPDATE: nó là số đã tiêu, chỉ đổi qua
	// AddUsed khi có đơn được duyệt. Cho phép ghi đè ở đây nghĩa là một lần
	// sửa quỹ phép năm sẽ xoá sạch lịch sử đã dùng.
	err := r.db.QueryRow(ctx, q, b.EmployeeID, b.Year, b.EntitledDays,
		b.CarriedOverDays, b.UsedDays).
		Scan(&b.UsedDays, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		return fmt.Errorf("ghi quỹ phép: %w", err)
	}
	return nil
}

func (r *BalanceRepository) AddUsed(
	ctx context.Context,
	employeeID uuid.UUID,
	year int,
	delta float64,
) error {
	// Tạo dòng quỹ nếu chưa có, rồi cộng dồn. Cộng dồn ngay trong SQL nên
	// hai đơn duyệt cùng lúc không ghi đè nhau.
	const q = `
		INSERT INTO leave_balances (employee_id, year, used_days)
		VALUES ($1, $2, GREATEST($3, 0))
		ON CONFLICT (employee_id, year) DO UPDATE SET
		  used_days = GREATEST(leave_balances.used_days + $3, 0)`

	if _, err := r.db.Exec(ctx, q, employeeID, year, delta); err != nil {
		return fmt.Errorf("cập nhật số ngày phép đã dùng: %w", err)
	}
	return nil
}

func (r *BalanceRepository) List(
	ctx context.Context,
	year int,
	employeeIDs []uuid.UUID,
	restrict bool,
) ([]*domainatt.Balance, error) {
	ids := employeeIDs
	if ids == nil {
		ids = []uuid.UUID{}
	}

	const q = `
		SELECT b.employee_id, b.year, b.entitled_days, b.carried_over_days,
		       b.used_days, b.created_at, b.updated_at, COALESCE(e.full_name,'')
		FROM leave_balances b
		LEFT JOIN employees e ON e.id = b.employee_id
		WHERE b.year = $1
		  AND (NOT $2::bool OR b.employee_id = ANY($3::uuid[]))
		ORDER BY e.full_name`

	rows, err := r.db.Query(ctx, q, year, restrict, ids)
	if err != nil {
		return nil, fmt.Errorf("liệt kê quỹ phép: %w", err)
	}
	defer rows.Close()

	out := make([]*domainatt.Balance, 0)
	for rows.Next() {
		var b domainatt.Balance
		if err := rows.Scan(&b.EmployeeID, &b.Year, &b.EntitledDays,
			&b.CarriedOverDays, &b.UsedDays, &b.CreatedAt, &b.UpdatedAt,
			&b.EmployeeName); err != nil {
			return nil, err
		}
		out = append(out, &b)
	}
	return out, rows.Err()
}
