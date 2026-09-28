package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	domainhr "github.com/PhamVanPhuc2k2/manage/internal/domain/hr"
	"github.com/PhamVanPhuc2k2/manage/pkg/postgres"
)

// EmployeeImportRepository lưu các lượt nhập nhân viên hàng loạt.
type EmployeeImportRepository struct {
	db *postgres.DB
}

func NewEmployeeImportRepository(db *postgres.DB) *EmployeeImportRepository {
	return &EmployeeImportRepository{db: db}
}

func (r *EmployeeImportRepository) Create(ctx context.Context, imp *domainhr.EmployeeImport) error {
	rows, err := json.Marshal(imp.Rows)
	if err != nil {
		return fmt.Errorf("mã hoá các dòng: %w", err)
	}
	actor, err := json.Marshal(imp.Actor)
	if err != nil {
		return fmt.Errorf("mã hoá actor: %w", err)
	}
	const q = `
		INSERT INTO employee_imports
		    (company_id, file_name, status, create_accounts, total_rows, rows, created_by, actor)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at`
	return r.db.QueryRow(ctx, q,
		imp.CompanyID, imp.FileName, imp.Status, imp.CreateAccounts,
		len(imp.Rows), rows, imp.CreatedBy, actor,
	).Scan(&imp.ID, &imp.CreatedAt)
}

// Hai cột đếm tính ngay trong SQL để trang danh sách khỏi phải kéo cả
// mảng results về chỉ để đếm.
const selectEmployeeImport = `
SELECT i.id, i.company_id, i.file_name, i.status, i.create_accounts, i.total_rows,
       i.created_by, COALESCE(e.full_name, ''), i.actor, COALESCE(i.error, ''),
       i.created_at, i.started_at, i.finished_at,
       (SELECT COUNT(*) FROM jsonb_array_elements(i.results) x WHERE COALESCE(x->>'error','') = ''),
       (SELECT COUNT(*) FROM jsonb_array_elements(i.results) x WHERE COALESCE(x->>'error','') <> '')
`

func scanEmployeeImport(row pgx.Row, extra ...any) (*domainhr.EmployeeImport, error) {
	var (
		imp   domainhr.EmployeeImport
		actor []byte
	)
	dest := []any{
		&imp.ID, &imp.CompanyID, &imp.FileName, &imp.Status, &imp.CreateAccounts, &imp.TotalRows,
		&imp.CreatedBy, &imp.CreatorName, &actor, &imp.Error,
		&imp.CreatedAt, &imp.StartedAt, &imp.FinishedAt,
		&imp.Succeeded, &imp.Failed,
	}
	err := row.Scan(append(dest, extra...)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainhr.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("đọc lượt nhập: %w", err)
	}
	if err := json.Unmarshal(actor, &imp.Actor); err != nil {
		return nil, fmt.Errorf("giải mã actor: %w", err)
	}
	return &imp, nil
}

func (r *EmployeeImportRepository) GetByID(ctx context.Context, id uuid.UUID) (*domainhr.EmployeeImport, error) {
	q := selectEmployeeImport + `, i.rows, i.results
		FROM employee_imports i
		LEFT JOIN employees e ON e.id = i.created_by
		WHERE i.id = $1`
	var rows, results []byte
	imp, err := scanEmployeeImport(r.db.QueryRow(ctx, q, id), &rows, &results)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(rows, &imp.Rows); err != nil {
		return nil, fmt.Errorf("giải mã các dòng: %w", err)
	}
	if err := json.Unmarshal(results, &imp.Results); err != nil {
		return nil, fmt.Errorf("giải mã kết quả: %w", err)
	}
	return imp, nil
}

func (r *EmployeeImportRepository) List(
	ctx context.Context,
	companyID uuid.UUID,
	createdBy *uuid.UUID,
	limit int,
) ([]*domainhr.EmployeeImport, error) {
	q := selectEmployeeImport + `
		FROM employee_imports i
		LEFT JOIN employees e ON e.id = i.created_by
		WHERE i.company_id = $1 AND ($2::uuid IS NULL OR i.created_by = $2)
		ORDER BY i.created_at DESC
		LIMIT $3`
	rows, err := r.db.Query(ctx, q, companyID, createdBy, limit)
	if err != nil {
		return nil, fmt.Errorf("liệt kê lượt nhập: %w", err)
	}
	defer rows.Close()

	var out []*domainhr.EmployeeImport
	for rows.Next() {
		imp, err := scanEmployeeImport(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, imp)
	}
	return out, rows.Err()
}

func (r *EmployeeImportRepository) MarkProcessing(ctx context.Context, id uuid.UUID) error {
	// COALESCE giữ nguyên started_at khi message được giao lại: thời điểm
	// bắt đầu là lần chạy ĐẦU, không phải lần chạy lại.
	const q = `
		UPDATE employee_imports
		SET status = 'processing', started_at = COALESCE(started_at, NOW())
		WHERE id = $1`
	_, err := r.db.Exec(ctx, q, id)
	return err
}

func (r *EmployeeImportRepository) AppendResult(
	ctx context.Context,
	id uuid.UUID,
	res domainhr.ImportRowResult,
) error {
	b, err := json.Marshal(res)
	if err != nil {
		return fmt.Errorf("mã hoá kết quả: %w", err)
	}
	const q = `UPDATE employee_imports SET results = results || jsonb_build_array($2::jsonb) WHERE id = $1`
	_, err = r.db.Exec(ctx, q, id, b)
	return err
}

func (r *EmployeeImportRepository) Finish(
	ctx context.Context,
	id uuid.UUID,
	status domainhr.ImportStatus,
	errMsg string,
) error {
	const q = `
		UPDATE employee_imports
		SET status = $2, error = NULLIF($3, ''), finished_at = NOW()
		WHERE id = $1`
	_, err := r.db.Exec(ctx, q, id, status, errMsg)
	return err
}
