package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	domainproject "github.com/PhamVanPhuc2k2/manage/internal/domain/project"
	"github.com/PhamVanPhuc2k2/manage/pkg/postgres"
)

type ProjectMemberRepository struct {
	db *postgres.DB
}

func NewProjectMemberRepository(db *postgres.DB) *ProjectMemberRepository {
	return &ProjectMemberRepository{db: db}
}

const selectProjectMember = `
SELECT pm.project_id, pm.employee_id, pm.role, pm.added_by, pm.added_at,
       COALESCE(e.full_name,''), COALESCE(e.employee_code,''), COALESCE(e.email,''),
       COALESCE(pos.name,''), COALESCE(d.name,''), COALESCE(e.avatar_key,'')
FROM project_members pm
JOIN employees e ON e.id = pm.employee_id
LEFT JOIN positions   pos ON pos.id = e.position_id
LEFT JOIN departments d   ON d.id   = e.department_id
`

func scanMember(row pgx.Row) (*domainproject.Member, error) {
	var m domainproject.Member
	err := row.Scan(&m.ProjectID, &m.EmployeeID, &m.Role, &m.AddedBy, &m.AddedAt,
		&m.EmployeeName, &m.EmployeeCode, &m.Email,
		&m.PositionName, &m.DepartmentName, &m.AvatarKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainproject.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("đọc thành viên dự án: %w", err)
	}
	return &m, nil
}

// Add thêm thành viên. Thêm lại người đã có thì CẬP NHẬT vai trò.
//
// Chọn upsert thay vì báo lỗi trùng: giao diện "thêm thành viên" và "đổi vai
// trò" là hai nút khác nhau nhưng người dùng hay nhầm, và kết quả họ mong
// đợi trong cả hai trường hợp đều là "người này có vai trò như tôi vừa chọn".
func (r *ProjectMemberRepository) Add(ctx context.Context, m *domainproject.Member) error {
	const q = `
		INSERT INTO project_members (project_id, employee_id, role, added_by)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (project_id, employee_id)
		DO UPDATE SET role = EXCLUDED.role
		RETURNING added_at`

	err := r.db.QueryRow(ctx, q, m.ProjectID, m.EmployeeID, m.Role, m.AddedBy).
		Scan(&m.AddedAt)
	if err != nil {
		return fmt.Errorf("thêm thành viên dự án: %w", err)
	}
	return nil
}

func (r *ProjectMemberRepository) UpdateRole(
	ctx context.Context,
	projectID, employeeID uuid.UUID,
	role domainproject.Role,
) error {
	const q = `
		UPDATE project_members SET role = $3
		WHERE project_id = $1 AND employee_id = $2`

	tag, err := r.db.Exec(ctx, q, projectID, employeeID, role)
	if err != nil {
		return fmt.Errorf("đổi vai trò thành viên: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainproject.ErrNotFound
	}
	return nil
}

func (r *ProjectMemberRepository) Remove(ctx context.Context, projectID, employeeID uuid.UUID) error {
	const q = `DELETE FROM project_members WHERE project_id = $1 AND employee_id = $2`

	tag, err := r.db.Exec(ctx, q, projectID, employeeID)
	if err != nil {
		return fmt.Errorf("gỡ thành viên dự án: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainproject.ErrNotFound
	}
	return nil
}

func (r *ProjectMemberRepository) List(
	ctx context.Context,
	projectID uuid.UUID,
) ([]*domainproject.Member, error) {
	// Sắp theo vai trò trước: chủ dự án luôn đứng đầu danh sách.
	q := selectProjectMember + `
		WHERE pm.project_id = $1 AND e.deleted_at IS NULL
		ORDER BY
		  CASE pm.role WHEN 'owner' THEN 0 WHEN 'member' THEN 1 ELSE 2 END,
		  e.full_name`

	rows, err := r.db.Query(ctx, q, projectID)
	if err != nil {
		return nil, fmt.Errorf("liệt kê thành viên dự án: %w", err)
	}
	defer rows.Close()

	out := make([]*domainproject.Member, 0)
	for rows.Next() {
		var m domainproject.Member
		if err := rows.Scan(&m.ProjectID, &m.EmployeeID, &m.Role, &m.AddedBy, &m.AddedAt,
			&m.EmployeeName, &m.EmployeeCode, &m.Email,
			&m.PositionName, &m.DepartmentName, &m.AvatarKey); err != nil {
			return nil, err
		}
		out = append(out, &m)
	}
	return out, rows.Err()
}

func (r *ProjectMemberRepository) Get(
	ctx context.Context,
	projectID, employeeID uuid.UUID,
) (*domainproject.Member, error) {
	q := selectProjectMember + ` WHERE pm.project_id = $1 AND pm.employee_id = $2`
	return scanMember(r.db.QueryRow(ctx, q, projectID, employeeID))
}

func (r *ProjectMemberRepository) CountByRole(
	ctx context.Context,
	projectID uuid.UUID,
	role domainproject.Role,
) (int, error) {
	const q = `SELECT COUNT(*) FROM project_members WHERE project_id = $1 AND role = $2`

	var n int
	if err := r.db.QueryRow(ctx, q, projectID, role).Scan(&n); err != nil {
		return 0, fmt.Errorf("đếm thành viên theo vai trò: %w", err)
	}
	return n, nil
}
