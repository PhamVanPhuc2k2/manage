package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	domainatt "github.com/PhamVanPhuc2k2/manage/internal/domain/attendance"
	"github.com/PhamVanPhuc2k2/manage/pkg/postgres"
)

// AttendanceExportRepository lưu các lượt xuất báo cáo chấm công.
type AttendanceExportRepository struct {
	db *postgres.DB
}

func NewAttendanceExportRepository(db *postgres.DB) *AttendanceExportRepository {
	return &AttendanceExportRepository{db: db}
}

func (r *AttendanceExportRepository) Create(ctx context.Context, e *domainatt.Export) error {
	actor, err := json.Marshal(e.Actor)
	if err != nil {
		return fmt.Errorf("mã hoá actor: %w", err)
	}
	const q = `
		INSERT INTO attendance_exports (year, month, department_id, status, requested_by, actor)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at`
	return r.db.QueryRow(ctx, q,
		e.Year, e.Month, e.DepartmentID, e.Status, e.RequestedBy, actor,
	).Scan(&e.ID, &e.CreatedAt)
}

// Không bao giờ SELECT cột file ở đây: danh sách mười lượt xuất không được
// kéo theo mười tệp Excel.
const selectAttendanceExport = `
SELECT x.id, x.year, x.month, x.department_id, COALESCE(d.name, ''), x.status,
       x.requested_by, COALESCE(e.full_name, ''), x.actor,
       COALESCE(x.file_name, ''), x.file IS NOT NULL, COALESCE(x.employee_count, 0),
       COALESCE(x.error, ''), x.created_at, x.finished_at
FROM attendance_exports x
LEFT JOIN departments d ON d.id = x.department_id
LEFT JOIN employees   e ON e.id = x.requested_by
`

func scanAttendanceExport(row pgx.Row) (*domainatt.Export, error) {
	var (
		e     domainatt.Export
		actor []byte
	)
	err := row.Scan(
		&e.ID, &e.Year, &e.Month, &e.DepartmentID, &e.DepartmentName, &e.Status,
		&e.RequestedBy, &e.RequesterName, &actor,
		&e.FileName, &e.HasFile, &e.EmployeeCount,
		&e.Error, &e.CreatedAt, &e.FinishedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainatt.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("đọc lượt xuất: %w", err)
	}
	if err := json.Unmarshal(actor, &e.Actor); err != nil {
		return nil, fmt.Errorf("giải mã actor: %w", err)
	}
	return &e, nil
}

func (r *AttendanceExportRepository) GetByID(ctx context.Context, id uuid.UUID) (*domainatt.Export, error) {
	return scanAttendanceExport(r.db.QueryRow(ctx, selectAttendanceExport+` WHERE x.id = $1`, id))
}

func (r *AttendanceExportRepository) GetFile(ctx context.Context, id uuid.UUID) (string, []byte, error) {
	var (
		name string
		data []byte
	)
	err := r.db.QueryRow(ctx,
		`SELECT COALESCE(file_name, ''), file FROM attendance_exports
		 WHERE id = $1 AND file IS NOT NULL`, id).Scan(&name, &data)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, domainatt.ErrNotFound
	}
	if err != nil {
		return "", nil, fmt.Errorf("đọc tệp xuất: %w", err)
	}
	return name, data, nil
}

func (r *AttendanceExportRepository) List(
	ctx context.Context,
	requestedBy *uuid.UUID,
	limit int,
) ([]*domainatt.Export, error) {
	rows, err := r.db.Query(ctx, selectAttendanceExport+`
		WHERE ($1::uuid IS NULL OR x.requested_by = $1)
		ORDER BY x.created_at DESC
		LIMIT $2`, requestedBy, limit)
	if err != nil {
		return nil, fmt.Errorf("liệt kê lượt xuất: %w", err)
	}
	defer rows.Close()

	var out []*domainatt.Export
	for rows.Next() {
		e, err := scanAttendanceExport(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *AttendanceExportRepository) MarkProcessing(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx,
		`UPDATE attendance_exports SET status = 'processing' WHERE id = $1`, id)
	return err
}

func (r *AttendanceExportRepository) Complete(
	ctx context.Context,
	id uuid.UUID,
	fileName string,
	data []byte,
	employeeCount int,
) error {
	_, err := r.db.Exec(ctx, `
		UPDATE attendance_exports
		SET status = 'done', file_name = $2, file = $3, employee_count = $4,
		    error = NULL, finished_at = NOW()
		WHERE id = $1`, id, fileName, data, employeeCount)
	return err
}

func (r *AttendanceExportRepository) Fail(ctx context.Context, id uuid.UUID, msg string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE attendance_exports
		SET status = 'failed', error = $2, finished_at = NOW()
		WHERE id = $1`, id, msg)
	return err
}

func (r *AttendanceExportRepository) PurgeFiles(ctx context.Context, before time.Time) (int64, error) {
	tag, err := r.db.Exec(ctx,
		`UPDATE attendance_exports SET file = NULL
		 WHERE file IS NOT NULL AND created_at < $1`, before)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
