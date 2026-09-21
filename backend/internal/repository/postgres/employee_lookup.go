package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/PhamVanPhuc2k2/manage/pkg/postgres"
)

// EmployeeLookup hiện thực cổng cùng tên của module project.
//
// Là một kiểu RIÊNG chứ không phải thêm method vào EmployeeRepository: hai
// bên có người dùng khác nhau và nhu cầu khác nhau. Gộp lại thì mỗi lần
// module project cần thêm một phép tra cứu, interface của module hr lại
// phình ra một method mà chính nó không dùng.
type EmployeeLookup struct {
	db *postgres.DB
}

func NewEmployeeLookup(db *postgres.DB) *EmployeeLookup {
	return &EmployeeLookup{db: db}
}

// Exists cho biết nhân viên có tồn tại VÀ còn làm việc không.
//
// Gộp hai điều kiện vào một câu trả lời là có chủ ý: mọi chỗ gọi đều cần cả
// hai. Giao việc cho người đã nghỉ là lỗi nghiệp vụ, và tách thành hai hàm
// chỉ tạo cơ hội cho ai đó quên gọi hàm thứ hai.
func (l *EmployeeLookup) Exists(ctx context.Context, id uuid.UUID) (bool, error) {
	const q = `
		SELECT EXISTS (
		  SELECT 1 FROM employees
		  WHERE id = $1 AND deleted_at IS NULL AND status <> 'resigned')`

	var ok bool
	if err := l.db.QueryRow(ctx, q, id).Scan(&ok); err != nil {
		return false, fmt.Errorf("kiểm tra nhân viên: %w", err)
	}
	return ok, nil
}

// NamesOf tra tên nhiều nhân viên trong MỘT truy vấn.
//
// Tồn tại để chỗ gọi không phải lặp qua danh sách và hỏi từng người một —
// đó là lỗi N+1 kinh điển, với một bình luận có 5 người được nhắc tên thì
// thành 5 vòng round-trip tới database.
func (l *EmployeeLookup) NamesOf(
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

func (l *EmployeeLookup) DepartmentOf(ctx context.Context, id uuid.UUID) (*uuid.UUID, error) {
	const q = `SELECT department_id FROM employees WHERE id = $1 AND deleted_at IS NULL`

	var deptID *uuid.UUID
	if err := l.db.QueryRow(ctx, q, id).Scan(&deptID); err != nil {
		return nil, fmt.Errorf("đọc phòng ban của nhân viên: %w", err)
	}
	return deptID, nil
}

// ListActiveIDs trả về id mọi nhân viên còn làm việc.
//
// Job tổng hợp công cuối ngày cần nó để đánh dấu người VẮNG. Người vắng
// không có phiên làm việc nào, nên không thể suy ra từ bảng phiên — phải
// đối chiếu với danh sách đầy đủ.
func (l *EmployeeLookup) ListActiveIDs(
	ctx context.Context,
	departmentIDs []uuid.UUID,
) ([]uuid.UUID, error) {
	ids := departmentIDs
	if ids == nil {
		ids = []uuid.UUID{}
	}

	const q = `
		SELECT id FROM employees
		WHERE deleted_at IS NULL AND status <> 'resigned'
		  AND (cardinality($1::uuid[]) = 0 OR department_id = ANY($1))`

	rows, err := l.db.Query(ctx, q, ids)
	if err != nil {
		return nil, fmt.Errorf("liệt kê nhân viên đang làm việc: %w", err)
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
