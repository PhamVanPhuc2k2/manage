package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	domainhr "github.com/PhamVanPhuc2k2/manage/internal/domain/hr"
	"github.com/PhamVanPhuc2k2/manage/pkg/postgres"
)

type DepartmentRepository struct {
	db *postgres.DB
}

func NewDepartmentRepository(db *postgres.DB) *DepartmentRepository {
	return &DepartmentRepository{db: db}
}

const selectDepartment = `
SELECT d.id, d.company_id, d.parent_id, d.code, d.name,
       COALESCE(d.description,''), d.manager_id,
       COALESCE(m.full_name,''),
       (SELECT COUNT(*) FROM employees e
         WHERE e.department_id = d.id AND e.deleted_at IS NULL),
       d.created_at, d.updated_at
FROM departments d
LEFT JOIN employees m ON m.id = d.manager_id
`

func scanDepartment(row pgx.Row) (*domainhr.Department, error) {
	var d domainhr.Department
	err := row.Scan(&d.ID, &d.CompanyID, &d.ParentID, &d.Code, &d.Name,
		&d.Description, &d.ManagerID, &d.ManagerName, &d.EmployeeCount,
		&d.CreatedAt, &d.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainhr.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("đọc phòng ban: %w", err)
	}
	return &d, nil
}

func (r *DepartmentRepository) Create(ctx context.Context, d *domainhr.Department) error {
	const q = `
		INSERT INTO departments (company_id, parent_id, code, name, description, manager_id)
		VALUES ($1, $2, $3, $4, NULLIF($5,''), $6)
		RETURNING id, created_at, updated_at`

	err := r.db.QueryRow(ctx, q, d.CompanyID, d.ParentID, d.Code, d.Name,
		d.Description, d.ManagerID).Scan(&d.ID, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return fmt.Errorf("tạo phòng ban: %w", err)
	}
	return nil
}

func (r *DepartmentRepository) Update(ctx context.Context, d *domainhr.Department) error {
	const q = `
		UPDATE departments
		SET parent_id = $2, code = $3, name = $4,
		    description = NULLIF($5,''), manager_id = $6
		WHERE id = $1 AND deleted_at IS NULL`

	tag, err := r.db.Exec(ctx, q, d.ID, d.ParentID, d.Code, d.Name, d.Description, d.ManagerID)
	if err != nil {
		return fmt.Errorf("cập nhật phòng ban: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainhr.ErrNotFound
	}
	return nil
}

func (r *DepartmentRepository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	const q = `UPDATE departments SET deleted_at = NOW() WHERE id = $1 AND deleted_at IS NULL`
	tag, err := r.db.Exec(ctx, q, id)
	if err != nil {
		return fmt.Errorf("xoá phòng ban: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainhr.ErrNotFound
	}
	return nil
}

func (r *DepartmentRepository) GetByID(ctx context.Context, id uuid.UUID) (*domainhr.Department, error) {
	q := selectDepartment + ` WHERE d.id = $1 AND d.deleted_at IS NULL`
	return scanDepartment(r.db.QueryRow(ctx, q, id))
}

func (r *DepartmentRepository) List(ctx context.Context, companyID uuid.UUID) ([]*domainhr.Department, error) {
	q := selectDepartment + ` WHERE d.company_id = $1 AND d.deleted_at IS NULL ORDER BY d.name`

	rows, err := r.db.Query(ctx, q, companyID)
	if err != nil {
		return nil, fmt.Errorf("liệt kê phòng ban: %w", err)
	}
	defer rows.Close()

	var out []*domainhr.Department
	for rows.Next() {
		var d domainhr.Department
		if err := rows.Scan(&d.ID, &d.CompanyID, &d.ParentID, &d.Code, &d.Name,
			&d.Description, &d.ManagerID, &d.ManagerName, &d.EmployeeCount,
			&d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, &d)
	}
	return out, rows.Err()
}

// ListSubtreeIDs trả về id của phòng ban và mọi phòng con, đệ quy.
//
// Van depth < 10 phòng trường hợp dữ liệu đã lỡ có vòng lặp — không có nó
// thì truy vấn chạy vô hạn và treo cả connection pool.
func (r *DepartmentRepository) ListSubtreeIDs(ctx context.Context, rootID uuid.UUID) ([]uuid.UUID, error) {
	const q = `
		WITH RECURSIVE subtree AS (
		    SELECT id, 0 AS depth FROM departments
		    WHERE id = $1 AND deleted_at IS NULL

		    UNION ALL

		    SELECT d.id, s.depth + 1
		    FROM departments d
		    JOIN subtree s ON d.parent_id = s.id
		    WHERE d.deleted_at IS NULL AND s.depth < 10
		)
		SELECT id FROM subtree`

	return r.queryUUIDs(ctx, q, rootID)
}

// ListAncestorIDs đi ngược lên cây, dùng để chặn vòng lặp khi đổi cha.
func (r *DepartmentRepository) ListAncestorIDs(ctx context.Context, id uuid.UUID) ([]uuid.UUID, error) {
	const q = `
		WITH RECURSIVE ancestors AS (
		    SELECT id, parent_id, 0 AS depth FROM departments
		    WHERE id = $1 AND deleted_at IS NULL

		    UNION ALL

		    SELECT d.id, d.parent_id, a.depth + 1
		    FROM departments d
		    JOIN ancestors a ON d.id = a.parent_id
		    WHERE d.deleted_at IS NULL AND a.depth < 10
		)
		SELECT id FROM ancestors`

	return r.queryUUIDs(ctx, q, id)
}

func (r *DepartmentRepository) queryUUIDs(ctx context.Context, q string, args ...any) ([]uuid.UUID, error) {
	rows, err := r.db.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("truy vấn cây phòng ban: %w", err)
	}
	defer rows.Close()

	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (r *DepartmentRepository) CountEmployees(ctx context.Context, id uuid.UUID) (int, error) {
	const q = `SELECT COUNT(*) FROM employees WHERE department_id = $1 AND deleted_at IS NULL`
	var n int
	err := r.db.QueryRow(ctx, q, id).Scan(&n)
	return n, err
}

func (r *DepartmentRepository) ExistsCode(
	ctx context.Context,
	companyID uuid.UUID,
	code string,
	excludeID *uuid.UUID,
) (bool, error) {
	const q = `
		SELECT EXISTS(
		    SELECT 1 FROM departments
		    WHERE company_id = $1 AND lower(code) = lower($2)
		      AND deleted_at IS NULL
		      AND ($3::uuid IS NULL OR id <> $3)
		)`
	var exists bool
	err := r.db.QueryRow(ctx, q, companyID, code, excludeID).Scan(&exists)
	return exists, err
}
