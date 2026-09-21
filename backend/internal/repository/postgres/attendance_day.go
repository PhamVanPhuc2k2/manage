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
// PHIÊN LÀM VIỆC
// =========================================================================

type AttendanceSessionRepository struct {
	db *postgres.DB
}

func NewAttendanceSessionRepository(db *postgres.DB) *AttendanceSessionRepository {
	return &AttendanceSessionRepository{db: db}
}

const selectSession = `
SELECT s.id, s.employee_id, s.work_date, s.started_at, s.ended_at,
       s.active_minutes, s.source, COALESCE(s.note,''),
       s.created_at, s.updated_at, COALESCE(e.full_name,'')
FROM attendance_sessions s
LEFT JOIN employees e ON e.id = s.employee_id
`

func (r *AttendanceSessionRepository) collect(ctx context.Context, q string, args ...any) (
	[]*domainatt.Session, error,
) {
	rows, err := r.db.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("liệt kê phiên làm việc: %w", err)
	}
	defer rows.Close()

	out := make([]*domainatt.Session, 0)
	for rows.Next() {
		var s domainatt.Session
		if err := rows.Scan(&s.ID, &s.EmployeeID, &s.WorkDate, &s.StartedAt,
			&s.EndedAt, &s.ActiveMinutes, &s.Source, &s.Note,
			&s.CreatedAt, &s.UpdatedAt, &s.EmployeeName); err != nil {
			return nil, err
		}
		out = append(out, &s)
	}
	return out, rows.Err()
}

func (r *AttendanceSessionRepository) Create(ctx context.Context, s *domainatt.Session) error {
	const q = `
		INSERT INTO attendance_sessions
		  (employee_id, work_date, started_at, ended_at, active_minutes, source, note)
		VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''))
		RETURNING id, created_at, updated_at`

	err := r.db.QueryRow(ctx, q, s.EmployeeID, s.WorkDate, s.StartedAt, s.EndedAt,
		s.ActiveMinutes, s.Source, s.Note).Scan(&s.ID, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return fmt.Errorf("tạo phiên làm việc: %w", err)
	}
	return nil
}

// ExtendLatest nới dài phiên gần nhất nếu khoảng hở còn trong ngưỡng.
//
// Toàn bộ "tìm phiên phù hợp" và "cập nhật nó" nằm trong MỘT câu lệnh.
//
// Tách thành hai bước (SELECT rồi UPDATE) sẽ hỏng khi có hai worker cùng
// quét: cả hai cùng thấy "phiên cuối đã quá hạn", cả hai cùng tạo phiên mới,
// và một ngày làm việc liền mạch bị cắt đôi mà không ai biết vì sao.
func (r *AttendanceSessionRepository) ExtendLatest(
	ctx context.Context,
	employeeID uuid.UUID,
	until time.Time,
	addActiveMinutes int,
	maxGap time.Duration,
) (bool, error) {
	const q = `
		UPDATE attendance_sessions
		SET ended_at = GREATEST(ended_at, $2),
		    active_minutes = active_minutes + $3
		WHERE id = (
		    SELECT id FROM attendance_sessions
		    WHERE employee_id = $1
		      AND source = 'presence'
		      AND ended_at >= $2::timestamptz - $4::interval
		      AND started_at <= $2
		    ORDER BY ended_at DESC
		    LIMIT 1
		    FOR UPDATE
		)`

	tag, err := r.db.Exec(ctx, q, employeeID, until, addActiveMinutes,
		fmt.Sprintf("%d seconds", int(maxGap.Seconds())))
	if err != nil {
		return false, fmt.Errorf("nới dài phiên làm việc: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func (r *AttendanceSessionRepository) ListByDay(
	ctx context.Context,
	employeeID uuid.UUID,
	day time.Time,
) ([]*domainatt.Session, error) {
	q := selectSession + `
		WHERE s.employee_id = $1 AND s.work_date = $2::date
		ORDER BY s.started_at`
	return r.collect(ctx, q, employeeID, day)
}

func (r *AttendanceSessionRepository) ListBetween(
	ctx context.Context,
	employeeID uuid.UUID,
	from, to time.Time,
) ([]*domainatt.Session, error) {
	q := selectSession + `
		WHERE s.employee_id = $1 AND s.work_date BETWEEN $2::date AND $3::date
		ORDER BY s.started_at`
	return r.collect(ctx, q, employeeID, from, to)
}

func (r *AttendanceSessionRepository) AggregateDay(
	ctx context.Context,
	employeeID uuid.UUID,
	day time.Time,
) (int, int, *time.Time, *time.Time, error) {
	// Tổng thời lượng tính bằng SUM của từng phiên, KHÔNG phải hiệu giữa mốc
	// đầu và mốc cuối: người làm sáng rồi nghỉ trưa rồi làm chiều có khoảng
	// trống ở giữa, và lấy hiệu hai đầu sẽ tính luôn cả giờ nghỉ trưa.
	const q = `
		SELECT
		  COALESCE(SUM(EXTRACT(EPOCH FROM (ended_at - started_at)) / 60), 0)::int,
		  COALESCE(SUM(active_minutes), 0)::int,
		  MIN(started_at), MAX(ended_at)
		FROM attendance_sessions
		WHERE employee_id = $1 AND work_date = $2::date`

	var (
		online, active int
		first, last    *time.Time
	)
	err := r.db.QueryRow(ctx, q, employeeID, day).Scan(&online, &active, &first, &last)
	if err != nil {
		return 0, 0, nil, nil, fmt.Errorf("tổng hợp phiên trong ngày: %w", err)
	}
	return online, active, first, last, nil
}

func (r *AttendanceSessionRepository) EmployeesWithSessions(
	ctx context.Context,
	day time.Time,
) ([]uuid.UUID, error) {
	const q = `
		SELECT DISTINCT employee_id FROM attendance_sessions
		WHERE work_date = $1::date`

	rows, err := r.db.Query(ctx, q, day)
	if err != nil {
		return nil, fmt.Errorf("liệt kê nhân viên có phiên: %w", err)
	}
	defer rows.Close()

	out := make([]uuid.UUID, 0)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// =========================================================================
// TỔNG HỢP NGÀY
// =========================================================================

type AttendanceDayRepository struct {
	db *postgres.DB
}

func NewAttendanceDayRepository(db *postgres.DB) *AttendanceDayRepository {
	return &AttendanceDayRepository{db: db}
}

const selectDay = `
SELECT d.id, d.employee_id, d.work_date, d.online_minutes, d.active_minutes,
       d.first_seen_at, d.last_seen_at, d.late_minutes, d.early_leave_minutes,
       d.shortfall_minutes, d.status, d.is_locked, d.created_at, d.updated_at,
       COALESCE(e.full_name,''), COALESCE(dep.name,'')
FROM attendance_days d
LEFT JOIN employees   e   ON e.id = d.employee_id
LEFT JOIN departments dep ON dep.id = e.department_id
`

func scanDayRow(row pgx.Row) (*domainatt.Day, error) {
	var d domainatt.Day
	err := row.Scan(&d.ID, &d.EmployeeID, &d.WorkDate, &d.OnlineMinutes,
		&d.ActiveMinutes, &d.FirstSeenAt, &d.LastSeenAt, &d.LateMinutes,
		&d.EarlyLeaveMinutes, &d.ShortfallMinutes, &d.Status, &d.IsLocked,
		&d.CreatedAt, &d.UpdatedAt, &d.EmployeeName, &d.DepartmentName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainatt.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("đọc bảng công ngày: %w", err)
	}
	return &d, nil
}

// Upsert ghi tổng hợp ngày, KHÔNG ghi đè dòng đã khoá.
//
// Điều kiện WHERE NOT is_locked nằm trong chính câu lệnh chứ không kiểm tra
// ở tầng trên: job tổng hợp chạy tự động mỗi ngày, và chỉ cần một lần chạy
// lại trên kỳ đã chốt là số liệu lương đã trả bị thay đổi sau lưng.
func (r *AttendanceDayRepository) Upsert(ctx context.Context, d *domainatt.Day) error {
	const q = `
		INSERT INTO attendance_days
		  (employee_id, work_date, online_minutes, active_minutes,
		   first_seen_at, last_seen_at, late_minutes, early_leave_minutes,
		   shortfall_minutes, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (employee_id, work_date) DO UPDATE SET
		  online_minutes      = EXCLUDED.online_minutes,
		  active_minutes      = EXCLUDED.active_minutes,
		  first_seen_at       = EXCLUDED.first_seen_at,
		  last_seen_at        = EXCLUDED.last_seen_at,
		  late_minutes        = EXCLUDED.late_minutes,
		  early_leave_minutes = EXCLUDED.early_leave_minutes,
		  shortfall_minutes   = EXCLUDED.shortfall_minutes,
		  status              = EXCLUDED.status
		WHERE NOT attendance_days.is_locked
		RETURNING id`

	err := r.db.QueryRow(ctx, q, d.EmployeeID, d.WorkDate, d.OnlineMinutes,
		d.ActiveMinutes, d.FirstSeenAt, d.LastSeenAt, d.LateMinutes,
		d.EarlyLeaveMinutes, d.ShortfallMinutes, d.Status).Scan(&d.ID)

	// Không có dòng trả về = dòng đã khoá và ON CONFLICT bị chặn bởi WHERE.
	// Đó là kết quả ĐÚNG, không phải lỗi.
	if errors.Is(err, pgx.ErrNoRows) {
		return domainatt.ErrLocked
	}
	if err != nil {
		return fmt.Errorf("ghi bảng công ngày: %w", err)
	}
	return nil
}

func (r *AttendanceDayRepository) GetByDate(
	ctx context.Context,
	employeeID uuid.UUID,
	day time.Time,
) (*domainatt.Day, error) {
	q := selectDay + ` WHERE d.employee_id = $1 AND d.work_date = $2::date`
	return scanDayRow(r.db.QueryRow(ctx, q, employeeID, day))
}

// dayWhere là mệnh đề lọc dùng chung cho List và MonthSummary.
const dayWhere = `
	WHERE d.work_date BETWEEN $1::date AND $2::date
	  AND ($3::uuid IS NULL OR d.employee_id = $3)
	  AND ($4::uuid IS NULL OR e.department_id = $4)
	  AND (NOT $5::bool OR d.employee_id = ANY($6::uuid[]))`

func dayArgs(f domainatt.DayFilter) []any {
	scoped := f.ScopedEmployeeIDs
	if scoped == nil {
		scoped = []uuid.UUID{}
	}
	return []any{f.From, f.To, f.EmployeeID, f.DepartmentID, f.RestrictScope, scoped}
}

func (r *AttendanceDayRepository) List(
	ctx context.Context,
	f domainatt.DayFilter,
) ([]*domainatt.Day, error) {
	q := selectDay + dayWhere + ` ORDER BY d.work_date DESC, e.full_name`

	rows, err := r.db.Query(ctx, q, dayArgs(f)...)
	if err != nil {
		return nil, fmt.Errorf("liệt kê bảng công: %w", err)
	}
	defer rows.Close()

	out := make([]*domainatt.Day, 0)
	for rows.Next() {
		var d domainatt.Day
		if err := rows.Scan(&d.ID, &d.EmployeeID, &d.WorkDate, &d.OnlineMinutes,
			&d.ActiveMinutes, &d.FirstSeenAt, &d.LastSeenAt, &d.LateMinutes,
			&d.EarlyLeaveMinutes, &d.ShortfallMinutes, &d.Status, &d.IsLocked,
			&d.CreatedAt, &d.UpdatedAt, &d.EmployeeName, &d.DepartmentName); err != nil {
			return nil, err
		}
		out = append(out, &d)
	}
	return out, rows.Err()
}

func (r *AttendanceDayRepository) MonthSummary(
	ctx context.Context,
	f domainatt.DayFilter,
) ([]*domainatt.MonthSummary, error) {
	q := `
		SELECT d.employee_id, COALESCE(e.full_name,''),
		       EXTRACT(YEAR  FROM d.work_date)::int,
		       EXTRACT(MONTH FROM d.work_date)::int,
		       COUNT(*) FILTER (WHERE d.status <> 'weekend' AND d.status <> 'holiday'),
		       COUNT(*) FILTER (WHERE d.status = 'present'),
		       COUNT(*) FILTER (WHERE d.status = 'absent'),
		       COUNT(*) FILTER (WHERE d.status = 'leave'),
		       COALESCE(SUM(d.online_minutes),0)::int,
		       COALESCE(SUM(d.active_minutes),0)::int,
		       COALESCE(SUM(d.late_minutes),0)::int,
		       COUNT(*) FILTER (WHERE d.late_minutes > 0),
		       COALESCE(SUM(d.shortfall_minutes),0)::int
		FROM attendance_days d
		LEFT JOIN employees e ON e.id = d.employee_id` + dayWhere + `
		GROUP BY d.employee_id, e.full_name,
		         EXTRACT(YEAR FROM d.work_date), EXTRACT(MONTH FROM d.work_date)
		ORDER BY e.full_name`

	rows, err := r.db.Query(ctx, q, dayArgs(f)...)
	if err != nil {
		return nil, fmt.Errorf("tổng hợp công theo tháng: %w", err)
	}
	defer rows.Close()

	out := make([]*domainatt.MonthSummary, 0)
	for rows.Next() {
		var m domainatt.MonthSummary
		if err := rows.Scan(&m.EmployeeID, &m.EmployeeName, &m.Year, &m.Month,
			&m.WorkdayCount, &m.PresentDays, &m.AbsentDays, &m.LeaveDays,
			&m.OnlineMinutes, &m.ActiveMinutes, &m.LateMinutes, &m.LateDays,
			&m.ShortfallMins); err != nil {
			return nil, err
		}
		out = append(out, &m)
	}
	return out, rows.Err()
}

func (r *AttendanceDayRepository) SetLocked(
	ctx context.Context,
	from, to time.Time,
	locked bool,
) (int64, error) {
	const q = `
		UPDATE attendance_days SET is_locked = $3
		WHERE work_date BETWEEN $1::date AND $2::date`

	tag, err := r.db.Exec(ctx, q, from, to, locked)
	if err != nil {
		return 0, fmt.Errorf("khoá kỳ công: %w", err)
	}
	return tag.RowsAffected(), nil
}
