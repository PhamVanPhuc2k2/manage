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

type EmployeeRepository struct {
	db *postgres.DB
}

func NewEmployeeRepository(db *postgres.DB) *EmployeeRepository {
	return &EmployeeRepository{db: db}
}

const selectEmployee = `
SELECT e.id, e.company_id, e.employee_code, e.full_name, e.email,
       COALESCE(e.phone,''), e.date_of_birth, COALESCE(e.gender,''),
       COALESCE(e.address,''),
       e.department_id, e.position_id, e.manager_id,
       e.work_mode, e.status, e.joined_at, e.resigned_at,
       COALESCE(e.avatar_key,''),
       COALESCE(d.name,''), COALESCE(p.name,''), COALESCE(m.full_name,''),
       EXISTS(SELECT 1 FROM users u WHERE u.employee_id = e.id AND u.deleted_at IS NULL),
       e.deleted_at, e.created_at, e.updated_at
FROM employees e
LEFT JOIN departments d ON d.id = e.department_id
LEFT JOIN positions   p ON p.id = e.position_id
LEFT JOIN employees   m ON m.id = e.manager_id
`

func scanEmployeeRow(row pgx.Row) (*domainhr.Employee, error) {
	var e domainhr.Employee
	err := row.Scan(
		&e.ID, &e.CompanyID, &e.EmployeeCode, &e.FullName, &e.Email,
		&e.Phone, &e.DateOfBirth, &e.Gender, &e.Address,
		&e.DepartmentID, &e.PositionID, &e.ManagerID,
		&e.WorkMode, &e.Status, &e.JoinedAt, &e.ResignedAt,
		&e.AvatarKey,
		&e.DepartmentName, &e.PositionName, &e.ManagerName, &e.HasAccount,
		&e.DeletedAt, &e.CreatedAt, &e.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainhr.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("đọc nhân viên: %w", err)
	}
	return &e, nil
}

func (r *EmployeeRepository) Create(ctx context.Context, e *domainhr.Employee) error {
	const q = `
		INSERT INTO employees (
		    company_id, employee_code, full_name, email, phone, date_of_birth,
		    gender, address, department_id, position_id, manager_id,
		    work_mode, status, joined_at
		) VALUES (
		    $1, $2, $3, lower($4), NULLIF($5,''), $6,
		    NULLIF($7,''), NULLIF($8,''), $9, $10, $11,
		    $12, $13, $14
		)
		RETURNING id, created_at, updated_at`

	err := r.db.QueryRow(ctx, q,
		e.CompanyID, e.EmployeeCode, e.FullName, strings.TrimSpace(e.Email),
		e.Phone, e.DateOfBirth, e.Gender, e.Address,
		e.DepartmentID, e.PositionID, e.ManagerID,
		e.WorkMode, e.Status, e.JoinedAt,
	).Scan(&e.ID, &e.CreatedAt, &e.UpdatedAt)

	if err != nil {
		return fmt.Errorf("tạo nhân viên: %w", err)
	}
	return nil
}

func (r *EmployeeRepository) Update(ctx context.Context, e *domainhr.Employee) error {
	const q = `
		UPDATE employees SET
		    employee_code = $2, full_name = $3, email = lower($4),
		    phone = NULLIF($5,''), date_of_birth = $6, gender = NULLIF($7,''),
		    address = NULLIF($8,''), department_id = $9, position_id = $10,
		    manager_id = $11, work_mode = $12, status = $13,
		    joined_at = $14, resigned_at = $15
		WHERE id = $1 AND deleted_at IS NULL`

	tag, err := r.db.Exec(ctx, q,
		e.ID, e.EmployeeCode, e.FullName, strings.TrimSpace(e.Email),
		e.Phone, e.DateOfBirth, e.Gender, e.Address,
		e.DepartmentID, e.PositionID, e.ManagerID,
		e.WorkMode, e.Status, e.JoinedAt, e.ResignedAt,
	)
	if err != nil {
		return fmt.Errorf("cập nhật nhân viên: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainhr.ErrNotFound
	}
	return nil
}

// SoftDelete vô hiệu hoá nhân viên VÀ tài khoản của họ trong một giao dịch.
//
// Phải làm cả hai: xoá mềm nhân viên mà quên vô hiệu hoá tài khoản thì người
// đó vẫn đăng nhập được bình thường.
func (r *EmployeeRepository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("mở giao dịch: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx,
		`UPDATE employees SET deleted_at = NOW() WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("xoá nhân viên: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainhr.ErrNotFound
	}

	// Đặt CẢ deleted_at chứ không chỉ is_active.
	//
	// Chỉ số unique trên email là chỉ số một phần: UNIQUE (lower(email))
	// WHERE deleted_at IS NULL. Không đặt deleted_at thì hàng cũ vẫn giữ chỗ
	// email đó vĩnh viễn — nhân viên nghỉ rồi quay lại sẽ không bao giờ tạo
	// được tài khoản mới, và lỗi hiện ra là 500 chứ không nói rõ vì sao.
	//
	// Hàng vẫn nằm lại trong bảng (xoá mềm) nên user_roles và assigned_by
	// không bị gãy tham chiếu.
	if _, err := tx.Exec(ctx,
		`UPDATE users SET is_active = FALSE, deleted_at = NOW()
		 WHERE employee_id = $1 AND deleted_at IS NULL`, id); err != nil {
		return fmt.Errorf("vô hiệu hoá tài khoản: %w", err)
	}

	return tx.Commit(ctx)
}

func (r *EmployeeRepository) GetByID(ctx context.Context, id uuid.UUID) (*domainhr.Employee, error) {
	q := selectEmployee + ` WHERE e.id = $1 AND e.deleted_at IS NULL`
	return scanEmployeeRow(r.db.QueryRow(ctx, q, id))
}

// List lọc, tìm kiếm và phân trang.
//
// COUNT(*) OVER() lấy tổng số bản ghi trong cùng một truy vấn, khỏi chạy hai
// lần. Đổi lại PostgreSQL phải quét hết tập kết quả — với vài nghìn nhân viên
// thì không đáng kể.
func (r *EmployeeRepository) List(
	ctx context.Context,
	f domainhr.EmployeeFilter,
) ([]*domainhr.Employee, int, error) {
	sortColumn := map[string]string{
		"full_name":     "e.full_name",
		"employee_code": "e.employee_code",
		"joined_at":     "e.joined_at",
		"created_at":    "e.created_at",
	}[f.SortBy]
	if sortColumn == "" {
		sortColumn = "e.full_name"
	}
	direction := "ASC"
	if f.SortDesc {
		direction = "DESC"
	}

	// sortColumn và direction lấy từ bảng ánh xạ cố định ở trên, KHÔNG phải
	// từ chuỗi người dùng gửi lên — nếu không thì đây là lỗ hổng SQL injection.
	// Mọi giá trị khác đều đi qua tham số $n.
	q := selectEmployee + `
		WHERE e.deleted_at IS NULL
		  AND ($1::uuid IS NULL OR e.department_id = $1)
		  AND ($2::uuid IS NULL OR e.position_id = $2)
		  AND ($3::employee_status IS NULL OR e.status = $3)
		  AND ($4::work_mode IS NULL OR e.work_mode = $4)
		  AND ($5::uuid IS NULL OR e.id = $5)
		  AND ($6::uuid[] IS NULL OR e.department_id = ANY($6))
		  AND ($7::text IS NULL OR
		       f_unaccent(lower(e.full_name)) LIKE '%' || f_unaccent(lower($7)) || '%'
		    OR lower(e.employee_code)         LIKE '%' || lower($7) || '%'
		    OR lower(e.email)                 LIKE '%' || lower($7) || '%')
		ORDER BY ` + sortColumn + ` ` + direction + `
		LIMIT $8 OFFSET $9`

	// Đếm riêng để không phải thêm COUNT(*) OVER() vào danh sách cột
	// (nó sẽ phá cấu trúc scan dùng chung với GetByID).
	countQ := `
		SELECT COUNT(*) FROM employees e
		WHERE e.deleted_at IS NULL
		  AND ($1::uuid IS NULL OR e.department_id = $1)
		  AND ($2::uuid IS NULL OR e.position_id = $2)
		  AND ($3::employee_status IS NULL OR e.status = $3)
		  AND ($4::work_mode IS NULL OR e.work_mode = $4)
		  AND ($5::uuid IS NULL OR e.id = $5)
		  AND ($6::uuid[] IS NULL OR e.department_id = ANY($6))
		  AND ($7::text IS NULL OR
		       f_unaccent(lower(e.full_name)) LIKE '%' || f_unaccent(lower($7)) || '%'
		    OR lower(e.employee_code)         LIKE '%' || lower($7) || '%'
		    OR lower(e.email)                 LIKE '%' || lower($7) || '%')`

	var search any
	if s := strings.TrimSpace(f.Search); s != "" {
		search = s
	}
	var scopedDepts any
	if len(f.ScopedDepartmentIDs) > 0 {
		scopedDepts = f.ScopedDepartmentIDs
	}

	args := []any{
		f.DepartmentID, f.PositionID, f.Status, f.WorkMode,
		f.ScopedEmployeeID, scopedDepts, search,
	}

	var total int
	if err := r.db.QueryRow(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("đếm nhân viên: %w", err)
	}

	offset := (f.Page - 1) * f.PageSize
	rows, err := r.db.Query(ctx, q, append(args, f.PageSize, offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("liệt kê nhân viên: %w", err)
	}
	defer rows.Close()

	out := make([]*domainhr.Employee, 0, f.PageSize)
	for rows.Next() {
		var e domainhr.Employee
		if err := rows.Scan(
			&e.ID, &e.CompanyID, &e.EmployeeCode, &e.FullName, &e.Email,
			&e.Phone, &e.DateOfBirth, &e.Gender, &e.Address,
			&e.DepartmentID, &e.PositionID, &e.ManagerID,
			&e.WorkMode, &e.Status, &e.JoinedAt, &e.ResignedAt,
			&e.AvatarKey,
			&e.DepartmentName, &e.PositionName, &e.ManagerName, &e.HasAccount,
			&e.DeletedAt, &e.CreatedAt, &e.UpdatedAt,
		); err != nil {
			return nil, 0, err
		}
		out = append(out, &e)
	}
	return out, total, rows.Err()
}

// ListAncestorIDs đi ngược chuỗi cấp trên, dùng để chặn vòng lặp.
//
// Chuỗi manager_id cũng là một cây và cũng vòng lặp được y như phòng ban.
func (r *EmployeeRepository) ListAncestorIDs(ctx context.Context, id uuid.UUID) ([]uuid.UUID, error) {
	const q = `
		WITH RECURSIVE chain AS (
		    SELECT id, manager_id, 0 AS depth FROM employees
		    WHERE id = $1 AND deleted_at IS NULL

		    UNION ALL

		    SELECT e.id, e.manager_id, c.depth + 1
		    FROM employees e
		    JOIN chain c ON e.id = c.manager_id
		    WHERE e.deleted_at IS NULL AND c.depth < 20
		)
		SELECT id FROM chain`

	rows, err := r.db.Query(ctx, q, id)
	if err != nil {
		return nil, fmt.Errorf("truy vấn chuỗi cấp trên: %w", err)
	}
	defer rows.Close()

	var out []uuid.UUID
	for rows.Next() {
		var v uuid.UUID
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *EmployeeRepository) ExistsCode(
	ctx context.Context,
	companyID uuid.UUID,
	code string,
	excludeID *uuid.UUID,
) (bool, error) {
	const q = `
		SELECT EXISTS(
		    SELECT 1 FROM employees
		    WHERE company_id = $1 AND lower(employee_code) = lower($2)
		      AND deleted_at IS NULL AND ($3::uuid IS NULL OR id <> $3))`
	var exists bool
	err := r.db.QueryRow(ctx, q, companyID, code, excludeID).Scan(&exists)
	return exists, err
}

func (r *EmployeeRepository) ExistsEmail(
	ctx context.Context,
	email string,
	excludeID *uuid.UUID,
) (bool, error) {
	const q = `
		SELECT EXISTS(
		    SELECT 1 FROM employees
		    WHERE lower(email) = lower($1)
		      AND deleted_at IS NULL AND ($2::uuid IS NULL OR id <> $2))`
	var exists bool
	err := r.db.QueryRow(ctx, q, strings.TrimSpace(email), excludeID).Scan(&exists)
	return exists, err
}

func (r *EmployeeRepository) UpdateAvatarKey(ctx context.Context, id uuid.UUID, key string) error {
	const q = `UPDATE employees SET avatar_key = NULLIF($2,'') WHERE id = $1 AND deleted_at IS NULL`
	tag, err := r.db.Exec(ctx, q, id, key)
	if err != nil {
		return fmt.Errorf("cập nhật avatar: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainhr.ErrNotFound
	}
	return nil
}
