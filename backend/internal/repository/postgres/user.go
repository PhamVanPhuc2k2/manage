package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	domainhr "github.com/PhamVanPhuc2k2/manage/internal/domain/hr"
	"github.com/PhamVanPhuc2k2/manage/pkg/postgres"
)

type UserRepository struct {
	db *postgres.DB
}

func NewUserRepository(db *postgres.DB) *UserRepository {
	return &UserRepository{db: db}
}

// selectUser JOIN sang employees để một truy vấn có đủ thông tin quyết định
// tài khoản có được đăng nhập không — tránh N+1 và tránh quên kiểm tra.
const selectUser = `
SELECT u.id, u.employee_id, u.email, u.password_hash, u.is_active,
       u.must_change_password, u.last_login_at, u.failed_attempts,
       u.locked_until, u.deleted_at, u.created_at, u.updated_at,
       e.full_name, e.status, e.deleted_at, e.department_id
FROM users u
JOIN employees e ON e.id = u.employee_id
`

func scanUser(row pgx.Row) (*domainhr.User, error) {
	var u domainhr.User
	err := row.Scan(
		&u.ID, &u.EmployeeID, &u.Email, &u.PasswordHash, &u.IsActive,
		&u.MustChangePassword, &u.LastLoginAt, &u.FailedAttempts,
		&u.LockedUntil, &u.DeletedAt, &u.CreatedAt, &u.UpdatedAt,
		&u.EmployeeName, &u.EmployeeStatus, &u.EmployeeDeletedAt, &u.DepartmentID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainhr.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("đọc tài khoản: %w", err)
	}
	return &u, nil
}

func (r *UserRepository) Create(ctx context.Context, u *domainhr.User) error {
	const q = `
		INSERT INTO users (employee_id, email, password_hash, is_active, must_change_password)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at, updated_at`

	err := r.db.QueryRow(ctx, q,
		u.EmployeeID, strings.ToLower(u.Email), u.PasswordHash, u.IsActive, u.MustChangePassword,
	).Scan(&u.ID, &u.CreatedAt, &u.UpdatedAt)

	if err != nil {
		return fmt.Errorf("tạo tài khoản: %w", err)
	}
	return nil
}

func (r *UserRepository) FindByEmail(ctx context.Context, email string) (*domainhr.User, error) {
	q := selectUser + ` WHERE lower(u.email) = lower($1) AND u.deleted_at IS NULL`
	return scanUser(r.db.QueryRow(ctx, q, strings.TrimSpace(email)))
}

func (r *UserRepository) FindByID(ctx context.Context, id uuid.UUID) (*domainhr.User, error) {
	q := selectUser + ` WHERE u.id = $1 AND u.deleted_at IS NULL`
	return scanUser(r.db.QueryRow(ctx, q, id))
}

func (r *UserRepository) FindByEmployeeID(ctx context.Context, employeeID uuid.UUID) (*domainhr.User, error) {
	q := selectUser + ` WHERE u.employee_id = $1 AND u.deleted_at IS NULL`
	return scanUser(r.db.QueryRow(ctx, q, employeeID))
}

func (r *UserRepository) UpdatePasswordHash(ctx context.Context, id uuid.UUID, hash string) error {
	const q = `UPDATE users SET password_hash = $2, must_change_password = FALSE WHERE id = $1`
	_, err := r.db.Exec(ctx, q, id, hash)
	return err
}

func (r *UserRepository) SetMustChangePassword(ctx context.Context, id uuid.UUID, must bool) error {
	const q = `UPDATE users SET must_change_password = $2 WHERE id = $1`
	_, err := r.db.Exec(ctx, q, id, must)
	return err
}

func (r *UserRepository) UpdateLastLogin(ctx context.Context, id uuid.UUID) error {
	const q = `UPDATE users SET last_login_at = NOW() WHERE id = $1`
	_, err := r.db.Exec(ctx, q, id)
	return err
}

func (r *UserRepository) IncrementFailedAttempts(ctx context.Context, id uuid.UUID) error {
	const q = `UPDATE users SET failed_attempts = failed_attempts + 1 WHERE id = $1`
	_, err := r.db.Exec(ctx, q, id)
	return err
}

func (r *UserRepository) ResetFailedAttempts(ctx context.Context, id uuid.UUID) error {
	const q = `UPDATE users SET failed_attempts = 0, locked_until = NULL WHERE id = $1`
	_, err := r.db.Exec(ctx, q, id)
	return err
}

func (r *UserRepository) SetActive(ctx context.Context, id uuid.UUID, active bool) error {
	const q = `UPDATE users SET is_active = $2 WHERE id = $1`
	_, err := r.db.Exec(ctx, q, id, active)
	return err
}

func (r *UserRepository) ListRoleCodes(ctx context.Context, userID uuid.UUID) ([]string, error) {
	const q = `
		SELECT r.code FROM user_roles ur
		JOIN roles r ON r.id = ur.role_id
		WHERE ur.user_id = $1
		ORDER BY r.code`

	rows, err := r.db.Query(ctx, q, userID)
	if err != nil {
		return nil, fmt.Errorf("đọc vai trò: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, err
		}
		out = append(out, code)
	}
	return out, rows.Err()
}

func (r *UserRepository) AssignRole(ctx context.Context, userID, roleID, assignedBy uuid.UUID) error {
	const q = `
		INSERT INTO user_roles (user_id, role_id, assigned_by)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, role_id) DO NOTHING`
	_, err := r.db.Exec(ctx, q, userID, roleID, assignedBy)
	return err
}

func (r *UserRepository) RemoveRole(ctx context.Context, userID, roleID uuid.UUID) error {
	const q = `DELETE FROM user_roles WHERE user_id = $1 AND role_id = $2`
	_, err := r.db.Exec(ctx, q, userID, roleID)
	return err
}
