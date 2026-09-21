package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	domainproject "github.com/PhamVanPhuc2k2/manage/internal/domain/project"
	"github.com/PhamVanPhuc2k2/manage/pkg/postgres"
)

type ProjectRepository struct {
	db *postgres.DB
}

func NewProjectRepository(db *postgres.DB) *ProjectRepository {
	return &ProjectRepository{db: db}
}

// Đếm task bằng subquery thay vì LEFT JOIN + GROUP BY.
//
// Với GROUP BY, mỗi cột thêm vào danh sách SELECT lại phải thêm vào GROUP BY,
// và chỉ cần một dự án có 500 task là bộ nhóm phình ra 500 dòng trước khi
// gộp lại. Subquery giữ mỗi dự án đúng một dòng từ đầu tới cuối.
const selectProject = `
SELECT p.id, p.company_id, p.code, p.name, COALESCE(p.description,''),
       p.status, p.owner_id, p.department_id,
       p.start_date, p.due_date, p.completed_at,
       COALESCE(o.full_name,''), COALESCE(d.name,''),
       (SELECT COUNT(*) FROM project_members pm WHERE pm.project_id = p.id),
       (SELECT COUNT(*) FROM tasks t WHERE t.project_id = p.id AND t.deleted_at IS NULL),
       (SELECT COUNT(*) FROM tasks t WHERE t.project_id = p.id AND t.deleted_at IS NULL
                                       AND t.status = 'done'),
       p.deleted_at, p.created_at, p.updated_at
FROM projects p
LEFT JOIN employees   o ON o.id = p.owner_id
LEFT JOIN departments d ON d.id = p.department_id
`

func scanProjectRow(row pgx.Row) (*domainproject.Project, error) {
	var p domainproject.Project
	err := row.Scan(&p.ID, &p.CompanyID, &p.Code, &p.Name, &p.Description,
		&p.Status, &p.OwnerID, &p.DepartmentID,
		&p.StartDate, &p.DueDate, &p.CompletedAt,
		&p.OwnerName, &p.DepartmentName,
		&p.MemberCount, &p.TaskCount, &p.DoneTaskCount,
		&p.DeletedAt, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainproject.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("đọc dự án: %w", err)
	}
	return &p, nil
}

func (r *ProjectRepository) Create(ctx context.Context, p *domainproject.Project) error {
	const q = `
		INSERT INTO projects
		  (company_id, code, name, description, status, owner_id,
		   department_id, start_date, due_date)
		VALUES ($1, $2, $3, NULLIF($4,''), $5, $6, $7, $8, $9)
		RETURNING id, created_at, updated_at`

	err := r.db.QueryRow(ctx, q, p.CompanyID, p.Code, p.Name, p.Description,
		p.Status, p.OwnerID, p.DepartmentID, p.StartDate, p.DueDate).
		Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return fmt.Errorf("tạo dự án: %w", err)
	}
	return nil
}

func (r *ProjectRepository) Update(ctx context.Context, p *domainproject.Project) error {
	const q = `
		UPDATE projects
		SET code = $2, name = $3, description = NULLIF($4,''), status = $5,
		    owner_id = $6, department_id = $7, start_date = $8, due_date = $9,
		    completed_at = $10
		WHERE id = $1 AND deleted_at IS NULL`

	tag, err := r.db.Exec(ctx, q, p.ID, p.Code, p.Name, p.Description, p.Status,
		p.OwnerID, p.DepartmentID, p.StartDate, p.DueDate, p.CompletedAt)
	if err != nil {
		return fmt.Errorf("cập nhật dự án: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainproject.ErrNotFound
	}
	return nil
}

func (r *ProjectRepository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	const q = `UPDATE projects SET deleted_at = NOW() WHERE id = $1 AND deleted_at IS NULL`
	tag, err := r.db.Exec(ctx, q, id)
	if err != nil {
		return fmt.Errorf("xoá dự án: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainproject.ErrNotFound
	}
	return nil
}

func (r *ProjectRepository) GetByID(
	ctx context.Context,
	id uuid.UUID,
) (*domainproject.Project, error) {
	q := selectProject + ` WHERE p.id = $1 AND p.deleted_at IS NULL`
	return scanProjectRow(r.db.QueryRow(ctx, q, id))
}

func (r *ProjectRepository) List(
	ctx context.Context,
	f domainproject.ProjectFilter,
) ([]*domainproject.Project, int, error) {
	sortColumn := map[string]string{
		"name":       "p.name",
		"code":       "p.code",
		"due_date":   "p.due_date",
		"created_at": "p.created_at",
		"status":     "p.status",
	}[f.SortBy]
	if sortColumn == "" {
		sortColumn = "p.created_at"
	}
	direction := "ASC"
	if f.SortDesc {
		direction = "DESC"
	}

	// sortColumn và direction lấy từ bảng ánh xạ cố định ở trên, KHÔNG phải
	// từ chuỗi người dùng gửi lên. Mọi giá trị khác đi qua tham số $n.
	const where = `
		WHERE p.deleted_at IS NULL
		  AND ($1::project_status IS NULL OR p.status = $1)
		  AND ($2::uuid IS NULL OR p.owner_id = $2)
		  AND ($3::uuid IS NULL OR p.department_id = $3)
		  AND ($4::uuid IS NULL OR EXISTS (
		        SELECT 1 FROM project_members pm
		        WHERE pm.project_id = p.id AND pm.employee_id = $4))
		  AND ($5::text IS NULL OR
		       f_unaccent(lower(p.name)) LIKE '%' || f_unaccent(lower($5)) || '%'
		    OR lower(p.code)             LIKE '%' || lower($5) || '%')`

	var search any
	if s := strings.TrimSpace(f.Search); s != "" {
		search = s
	}
	args := []any{f.Status, f.OwnerID, f.DepartmentID, f.MemberEmployeeID, search}

	var total int
	countQ := `SELECT COUNT(*) FROM projects p` + where
	if err := r.db.QueryRow(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("đếm dự án: %w", err)
	}

	q := selectProject + where +
		` ORDER BY ` + sortColumn + ` ` + direction + ` LIMIT $6 OFFSET $7`

	offset := (f.Page - 1) * f.PageSize
	rows, err := r.db.Query(ctx, q, append(args, f.PageSize, offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("liệt kê dự án: %w", err)
	}
	defer rows.Close()

	out := make([]*domainproject.Project, 0, f.PageSize)
	for rows.Next() {
		var p domainproject.Project
		if err := rows.Scan(&p.ID, &p.CompanyID, &p.Code, &p.Name, &p.Description,
			&p.Status, &p.OwnerID, &p.DepartmentID,
			&p.StartDate, &p.DueDate, &p.CompletedAt,
			&p.OwnerName, &p.DepartmentName,
			&p.MemberCount, &p.TaskCount, &p.DoneTaskCount,
			&p.DeletedAt, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, &p)
	}
	return out, total, rows.Err()
}

func (r *ProjectRepository) ExistsCode(
	ctx context.Context,
	companyID uuid.UUID,
	code string,
	excludeID *uuid.UUID,
) (bool, error) {
	const q = `
		SELECT EXISTS (
		  SELECT 1 FROM projects
		  WHERE company_id = $1 AND lower(code) = lower($2)
		    AND deleted_at IS NULL
		    AND ($3::uuid IS NULL OR id <> $3))`

	var exists bool
	if err := r.db.QueryRow(ctx, q, companyID, code, excludeID).Scan(&exists); err != nil {
		return false, fmt.Errorf("kiểm tra mã dự án: %w", err)
	}
	return exists, nil
}

func (r *ProjectRepository) ListIDsForMember(
	ctx context.Context,
	employeeID uuid.UUID,
) ([]uuid.UUID, error) {
	// Gồm cả dự án mình làm chủ nhưng chưa kịp có dòng trong project_members
	// — về nguyên tắc usecase luôn thêm chủ dự án làm thành viên, nhưng dữ
	// liệu sửa tay hoặc migration cũ có thể để sót.
	const q = `
		SELECT p.id FROM projects p
		WHERE p.deleted_at IS NULL
		  AND (p.owner_id = $1
		    OR EXISTS (SELECT 1 FROM project_members pm
		               WHERE pm.project_id = p.id AND pm.employee_id = $1))`

	rows, err := r.db.Query(ctx, q, employeeID)
	if err != nil {
		return nil, fmt.Errorf("liệt kê dự án của nhân viên: %w", err)
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

// NextTaskSeq tăng bộ đếm của dự án và trả về giá trị mới.
//
// UPDATE ... RETURNING khoá dòng projects trong suốt giao dịch, nên hai
// người tạo task cùng lúc buộc phải xếp hàng và không thể nhận cùng một số.
func (r *ProjectRepository) NextTaskSeq(ctx context.Context, projectID uuid.UUID) (int, error) {
	const q = `
		UPDATE projects SET task_seq = task_seq + 1
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING task_seq`

	var seq int
	err := r.db.QueryRow(ctx, q, projectID).Scan(&seq)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, domainproject.ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("cấp số thứ tự task: %w", err)
	}
	return seq, nil
}
