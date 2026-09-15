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

type PositionRepository struct {
	db *postgres.DB
}

func NewPositionRepository(db *postgres.DB) *PositionRepository {
	return &PositionRepository{db: db}
}

const selectPosition = `
SELECT id, company_id, code, name, salary_min, salary_max, created_at, updated_at
FROM positions
`

func scanPosition(row pgx.Row) (*domainhr.Position, error) {
	var p domainhr.Position
	err := row.Scan(&p.ID, &p.CompanyID, &p.Code, &p.Name,
		&p.SalaryMin, &p.SalaryMax, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainhr.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("đọc chức vụ: %w", err)
	}
	return &p, nil
}

func (r *PositionRepository) Create(ctx context.Context, p *domainhr.Position) error {
	const q = `
		INSERT INTO positions (company_id, code, name, salary_min, salary_max)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at, updated_at`

	err := r.db.QueryRow(ctx, q, p.CompanyID, p.Code, p.Name, p.SalaryMin, p.SalaryMax).
		Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return fmt.Errorf("tạo chức vụ: %w", err)
	}
	return nil
}

func (r *PositionRepository) Update(ctx context.Context, p *domainhr.Position) error {
	const q = `
		UPDATE positions SET code = $2, name = $3, salary_min = $4, salary_max = $5
		WHERE id = $1 AND deleted_at IS NULL`

	tag, err := r.db.Exec(ctx, q, p.ID, p.Code, p.Name, p.SalaryMin, p.SalaryMax)
	if err != nil {
		return fmt.Errorf("cập nhật chức vụ: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainhr.ErrNotFound
	}
	return nil
}

func (r *PositionRepository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	const q = `UPDATE positions SET deleted_at = NOW() WHERE id = $1 AND deleted_at IS NULL`
	tag, err := r.db.Exec(ctx, q, id)
	if err != nil {
		return fmt.Errorf("xoá chức vụ: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainhr.ErrNotFound
	}
	return nil
}

func (r *PositionRepository) GetByID(ctx context.Context, id uuid.UUID) (*domainhr.Position, error) {
	return scanPosition(r.db.QueryRow(ctx, selectPosition+` WHERE id = $1 AND deleted_at IS NULL`, id))
}

func (r *PositionRepository) List(ctx context.Context, companyID uuid.UUID) ([]*domainhr.Position, error) {
	q := selectPosition + ` WHERE company_id = $1 AND deleted_at IS NULL ORDER BY name`

	rows, err := r.db.Query(ctx, q, companyID)
	if err != nil {
		return nil, fmt.Errorf("liệt kê chức vụ: %w", err)
	}
	defer rows.Close()

	var out []*domainhr.Position
	for rows.Next() {
		var p domainhr.Position
		if err := rows.Scan(&p.ID, &p.CompanyID, &p.Code, &p.Name,
			&p.SalaryMin, &p.SalaryMax, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

func (r *PositionRepository) CountEmployees(ctx context.Context, id uuid.UUID) (int, error) {
	const q = `SELECT COUNT(*) FROM employees WHERE position_id = $1 AND deleted_at IS NULL`
	var n int
	err := r.db.QueryRow(ctx, q, id).Scan(&n)
	return n, err
}

func (r *PositionRepository) ExistsCode(
	ctx context.Context,
	companyID uuid.UUID,
	code string,
	excludeID *uuid.UUID,
) (bool, error) {
	const q = `
		SELECT EXISTS(
		    SELECT 1 FROM positions
		    WHERE company_id = $1 AND lower(code) = lower($2)
		      AND deleted_at IS NULL
		      AND ($3::uuid IS NULL OR id <> $3)
		)`
	var exists bool
	err := r.db.QueryRow(ctx, q, companyID, code, excludeID).Scan(&exists)
	return exists, err
}
