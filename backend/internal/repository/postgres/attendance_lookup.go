package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	domainpay "github.com/PhamVanPhuc2k2/manage/internal/domain/payroll"
	"github.com/PhamVanPhuc2k2/manage/pkg/postgres"
)

// AttendanceLookup hiện thực cổng cùng tên của module lương.
//
// Đọc THẲNG bảng attendance_days thay vì gọi qua usecase/attendance: hai
// tầng nghiệp vụ import chéo nhau là đường nhanh nhất tới phụ thuộc vòng.
// Module lương chỉ khai báo interface hẹp nó cần, và composition root nối
// bản hiện thực này vào.
type AttendanceLookup struct {
	db *postgres.DB
}

func NewAttendanceLookup(db *postgres.DB) *AttendanceLookup {
	return &AttendanceLookup{db: db}
}

// WorkdaysInPeriod đếm ngày công của nhiều người trong một khoảng, MỘT lượt.
//
// Máy tính lương chạy qua toàn bộ nhân viên; hỏi từng người một sẽ là N
// truy vấn cho một việc mà một câu GROUP BY làm xong.
func (l *AttendanceLookup) WorkdaysInPeriod(
	ctx context.Context,
	employeeIDs []uuid.UUID,
	from, to time.Time,
) (map[uuid.UUID]domainpay.Workdays, error) {
	out := make(map[uuid.UUID]domainpay.Workdays, len(employeeIDs))
	if len(employeeIDs) == 0 {
		return out, nil
	}

	// Ngày lễ và cuối tuần KHÔNG tính vào ngày công thực tế, nhưng cũng
	// không tính là vắng — người lao động vẫn hưởng lương những ngày đó qua
	// cơ chế lương tháng. Chỉ đếm 'present' và 'leave'.
	const q = `
		SELECT employee_id,
		       COUNT(*) FILTER (WHERE status = 'present')::float8,
		       COUNT(*) FILTER (WHERE status = 'leave')::float8,
		       COUNT(*) FILTER (WHERE status = 'absent')::float8
		FROM attendance_days
		WHERE employee_id = ANY($1)
		  AND work_date BETWEEN $2::date AND $3::date
		GROUP BY employee_id`

	rows, err := l.db.Query(ctx, q, employeeIDs, from, to)
	if err != nil {
		return nil, fmt.Errorf("đọc ngày công cho kỳ lương: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id uuid.UUID
			w  domainpay.Workdays
		)
		if err := rows.Scan(&id, &w.Present, &w.Leave, &w.Absent); err != nil {
			return nil, err
		}
		out[id] = w
	}
	return out, rows.Err()
}
