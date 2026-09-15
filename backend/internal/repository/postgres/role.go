package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	domainhr "github.com/PhamVanPhuc2k2/manage/internal/domain/hr"
	"github.com/PhamVanPhuc2k2/manage/pkg/postgres"
)

type RoleRepository struct {
	db *postgres.DB
}

func NewRoleRepository(db *postgres.DB) *RoleRepository {
	return &RoleRepository{db: db}
}

// List gom vai trò kèm danh sách quyền bằng array_agg, tránh N+1.
func (r *RoleRepository) List(ctx context.Context) ([]*domainhr.Role, error) {
	const q = `
		SELECT r.id, r.code, r.name, COALESCE(r.description,''),
		       r.scope::text, r.is_system,
		       COALESCE(array_agg(p.code) FILTER (WHERE p.code IS NOT NULL), '{}')
		FROM roles r
		LEFT JOIN role_permissions rp ON rp.role_id = r.id
		LEFT JOIN permissions p       ON p.id = rp.permission_id
		GROUP BY r.id
		ORDER BY r.code`

	rows, err := r.db.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("liệt kê vai trò: %w", err)
	}
	defer rows.Close()

	var out []*domainhr.Role
	for rows.Next() {
		var role domainhr.Role
		if err := rows.Scan(&role.ID, &role.Code, &role.Name, &role.Description,
			&role.Scope, &role.IsSystem, &role.Permissions); err != nil {
			return nil, err
		}
		out = append(out, &role)
	}
	return out, rows.Err()
}

func (r *RoleRepository) GetByCode(ctx context.Context, code string) (*domainhr.Role, error) {
	const q = `
		SELECT id, code, name, COALESCE(description,''), scope::text, is_system
		FROM roles WHERE code = $1`

	var role domainhr.Role
	err := r.db.QueryRow(ctx, q, code).Scan(&role.ID, &role.Code, &role.Name,
		&role.Description, &role.Scope, &role.IsSystem)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainhr.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("đọc vai trò: %w", err)
	}
	return &role, nil
}
