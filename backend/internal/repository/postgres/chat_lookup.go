package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/PhamVanPhuc2k2/manage/pkg/postgres"
)

// ChatLookup hiện thực chat.EmployeeLookup và notification.EmailLookup.
//
// Là kiểu riêng chứ không nối thêm method vào EmployeeLookup của module
// project: hai bên hỏi những câu khác nhau, và gộp lại thì mỗi lần một module
// cần thêm một phép tra, interface của module kia lại phình ra một method mà
// chính nó không dùng.
type ChatLookup struct {
	db *postgres.DB
}

func NewChatLookup(db *postgres.DB) *ChatLookup {
	return &ChatLookup{db: db}
}

func (l *ChatLookup) NamesOf(
	ctx context.Context,
	ids []uuid.UUID,
) (map[uuid.UUID]string, error) {
	out := make(map[uuid.UUID]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}

	const q = `SELECT id, full_name FROM employees WHERE id = ANY($1) AND deleted_at IS NULL`

	rows, err := l.db.Query(ctx, q, ids)
	if err != nil {
		return nil, fmt.Errorf("tra tên nhân viên: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id   uuid.UUID
			name string
		)
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}

// EmailsOf tra email, bỏ qua nhân viên không có email.
//
// Không báo lỗi khi thiếu: người không có email vẫn thấy thông báo trong ứng
// dụng, và làm hỏng cả lượt gửi vì một người thiếu email là đánh đổi sai.
func (l *ChatLookup) EmailsOf(
	ctx context.Context,
	ids []uuid.UUID,
) (map[uuid.UUID]string, error) {
	out := make(map[uuid.UUID]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}

	const q = `
		SELECT id, email FROM employees
		WHERE id = ANY($1) AND deleted_at IS NULL AND email IS NOT NULL AND email <> ''`

	rows, err := l.db.Query(ctx, q, ids)
	if err != nil {
		return nil, fmt.Errorf("tra email nhân viên: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id    uuid.UUID
			email string
		)
		if err := rows.Scan(&id, &email); err != nil {
			return nil, err
		}
		out[id] = email
	}
	return out, rows.Err()
}

// SameCompany kiểm tra MỌI id đều thuộc công ty này và còn làm việc.
//
// Đếm rồi so với độ dài danh sách, thay vì trả về danh sách hợp lệ: chỗ gọi
// chỉ cần biết có được phép hay không, và trả về danh sách đã lọc sẽ khiến
// việc thêm nhầm người ngoài công ty âm thầm thành công một phần.
func (l *ChatLookup) SameCompany(
	ctx context.Context,
	companyID uuid.UUID,
	ids []uuid.UUID,
) (bool, error) {
	if len(ids) == 0 {
		return true, nil
	}

	const q = `
		SELECT COUNT(DISTINCT id) FROM employees
		WHERE id = ANY($1) AND company_id = $2
		  AND deleted_at IS NULL AND status <> 'resigned'`

	var n int
	if err := l.db.QueryRow(ctx, q, ids, companyID).Scan(&n); err != nil {
		return false, fmt.Errorf("kiểm tra nhân viên cùng công ty: %w", err)
	}
	return n == len(ids), nil
}

func (l *ChatLookup) ByDepartment(
	ctx context.Context,
	departmentID uuid.UUID,
) ([]uuid.UUID, error) {
	const q = `
		SELECT id FROM employees
		WHERE department_id = $1 AND deleted_at IS NULL AND status <> 'resigned'`

	return l.scanIDs(ctx, q, departmentID)
}

// ByProject lấy thành viên dự án, kèm chủ nhiệm dự án.
//
// Chủ nhiệm không nhất thiết có trong bảng project_members, nhưng để họ ở
// ngoài nhóm chat của chính dự án mình phụ trách thì vô lý.
func (l *ChatLookup) ByProject(
	ctx context.Context,
	projectID uuid.UUID,
) ([]uuid.UUID, error) {
	const q = `
		SELECT pm.employee_id
		FROM project_members pm
		JOIN employees e ON e.id = pm.employee_id
		WHERE pm.project_id = $1 AND e.deleted_at IS NULL AND e.status <> 'resigned'
		UNION
		SELECT p.owner_id
		FROM projects p
		JOIN employees e ON e.id = p.owner_id
		WHERE p.id = $1 AND p.deleted_at IS NULL
		  AND e.deleted_at IS NULL AND e.status <> 'resigned'`

	return l.scanIDs(ctx, q, projectID)
}

func (l *ChatLookup) scanIDs(ctx context.Context, q string, arg any) ([]uuid.UUID, error) {
	rows, err := l.db.Query(ctx, q, arg)
	if err != nil {
		return nil, fmt.Errorf("liệt kê nhân viên: %w", err)
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
