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

type ProjectReportRepository struct {
	db *postgres.DB
}

func NewProjectReportRepository(db *postgres.DB) *ProjectReportRepository {
	return &ProjectReportRepository{db: db}
}

// ProjectProgress tổng hợp tiến độ một dự án trong MỘT truy vấn.
//
// Gộp mọi phép đếm vào một lượt bằng FILTER thay vì gọi năm câu COUNT riêng:
// bảng tasks chỉ phải quét một lần, và năm con số chắc chắn nhất quán với
// nhau vì cùng đọc một ảnh chụp dữ liệu.
func (r *ProjectReportRepository) ProjectProgress(
	ctx context.Context,
	projectID uuid.UUID,
) (*domainproject.ProjectProgress, error) {
	const q = `
		SELECT p.id, p.code, p.name, p.status, p.due_date,
		       COUNT(t.id),
		       COUNT(t.id) FILTER (WHERE t.status = 'todo'),
		       COUNT(t.id) FILTER (WHERE t.status = 'in_progress'),
		       COUNT(t.id) FILTER (WHERE t.status = 'review'),
		       COUNT(t.id) FILTER (WHERE t.status = 'done'),
		       COUNT(t.id) FILTER (
		           WHERE t.status <> 'done' AND t.due_date IS NOT NULL AND t.due_date < NOW()),
		       COALESCE(SUM(t.estimate_hours), 0),
		       COALESCE((SELECT SUM(tl.spent_minutes)
		                 FROM task_timelogs tl
		                 JOIN tasks tt ON tt.id = tl.task_id
		                 WHERE tt.project_id = p.id AND tt.deleted_at IS NULL), 0)
		FROM projects p
		LEFT JOIN tasks t ON t.project_id = p.id AND t.deleted_at IS NULL
		WHERE p.id = $1 AND p.deleted_at IS NULL
		GROUP BY p.id, p.code, p.name, p.status, p.due_date`

	var (
		out                            domainproject.ProjectProgress
		todo, inProgress, review, done int
	)
	err := r.db.QueryRow(ctx, q, projectID).Scan(
		&out.ProjectID, &out.ProjectCode, &out.ProjectName, &out.Status, &out.DueDate,
		&out.TotalTasks, &todo, &inProgress, &review, &done,
		&out.OverdueTasks, &out.EstimateHrs, &out.SpentMinutes)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainproject.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("đọc tiến độ dự án: %w", err)
	}

	out.ByStatus = map[domainproject.TaskStatus]int{
		domainproject.TaskTodo:       todo,
		domainproject.TaskInProgress: inProgress,
		domainproject.TaskReview:     review,
		domainproject.TaskDone:       done,
	}
	return &out, nil
}

// Workload thống kê khối lượng việc theo từng nhân viên.
//
// restrict = false nghĩa là không giới hạn dự án (người xem có phạm vi toàn
// công ty). restrict = true với projectIDs rỗng cho ra danh sách rỗng —
// đúng nghĩa "không thấy dự án nào", khác hẳn với "xem tất cả".
func (r *ProjectReportRepository) Workload(
	ctx context.Context,
	projectIDs []uuid.UUID,
	restrict bool,
) ([]*domainproject.Workload, error) {
	const q = `
		SELECT e.id, e.full_name, e.department_id,
		       COUNT(t.id) FILTER (WHERE t.status <> 'done'),
		       COUNT(t.id) FILTER (
		           WHERE t.status <> 'done' AND t.due_date IS NOT NULL AND t.due_date < NOW()),
		       COUNT(t.id) FILTER (WHERE t.status = 'done'),
		       COALESCE(SUM(t.estimate_hours) FILTER (WHERE t.status <> 'done'), 0),
		       COALESCE((SELECT SUM(tl.spent_minutes)
		                 FROM task_timelogs tl
		                 JOIN tasks tt ON tt.id = tl.task_id
		                 WHERE tl.employee_id = e.id AND tt.deleted_at IS NULL
		                   AND (NOT $1::bool OR tt.project_id = ANY($2::uuid[]))), 0)
		FROM employees e
		JOIN tasks t    ON t.assignee_id = e.id AND t.deleted_at IS NULL
		JOIN projects p ON p.id = t.project_id AND p.deleted_at IS NULL
		WHERE e.deleted_at IS NULL
		  AND (NOT $1::bool OR t.project_id = ANY($2::uuid[]))
		GROUP BY e.id, e.full_name, e.department_id
		ORDER BY COUNT(t.id) FILTER (WHERE t.status <> 'done') DESC, e.full_name`

	ids := projectIDs
	if ids == nil {
		ids = []uuid.UUID{}
	}

	rows, err := r.db.Query(ctx, q, restrict, ids)
	if err != nil {
		return nil, fmt.Errorf("thống kê khối lượng việc: %w", err)
	}
	defer rows.Close()

	out := make([]*domainproject.Workload, 0)
	for rows.Next() {
		var w domainproject.Workload
		if err := rows.Scan(&w.EmployeeID, &w.EmployeeName, &w.DepartmentID,
			&w.OpenTasks, &w.OverdueTasks, &w.DoneTasks,
			&w.EstimateHrs, &w.SpentMinutes); err != nil {
			return nil, err
		}
		out = append(out, &w)
	}
	return out, rows.Err()
}
