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

type TaskRepository struct {
	db *postgres.DB
}

func NewTaskRepository(db *postgres.DB) *TaskRepository {
	return &TaskRepository{db: db}
}

const selectTask = `
SELECT t.id, t.project_id, t.seq, t.parent_task_id,
       t.title, COALESCE(t.description,''), t.status, t.priority,
       t.assignee_id, t.reporter_id, t.due_date, t.estimate_hours, t.sort_order,
       t.started_at, t.completed_at,
       COALESCE(p.code,''), COALESCE(p.name,''),
       COALESCE(a.full_name,''), COALESCE(a.avatar_key,''), COALESCE(rp.full_name,''),
       (SELECT COUNT(*) FROM task_comments c
         WHERE c.task_id = t.id AND c.deleted_at IS NULL),
       (SELECT COUNT(*) FROM task_attachments at WHERE at.task_id = t.id),
       (SELECT COUNT(*) FROM tasks s
         WHERE s.parent_task_id = t.id AND s.deleted_at IS NULL),
       (SELECT COUNT(*) FROM tasks s
         WHERE s.parent_task_id = t.id AND s.deleted_at IS NULL AND s.status = 'done'),
       (SELECT COALESCE(SUM(tl.spent_minutes),0) FROM task_timelogs tl WHERE tl.task_id = t.id),
       t.deleted_at, t.created_at, t.updated_at
FROM tasks t
JOIN projects p ON p.id = t.project_id
LEFT JOIN employees a  ON a.id  = t.assignee_id
LEFT JOIN employees rp ON rp.id = t.reporter_id
`

// taskScanTargets gom danh sách con trỏ quét một dòng task.
//
// Viết một lần và dùng lại ở GetByID, List, ListBoard, ListSubtasks: bốn chỗ
// này phải quét CÙNG một danh sách cột theo CÙNG thứ tự. Tách ra hàm riêng
// nghĩa là thêm cột vào selectTask chỉ phải sửa đúng hai chỗ, và trình biên
// dịch bắt được nếu lệch số lượng.
func taskScanTargets(t *domainproject.Task) []any {
	return []any{
		&t.ID, &t.ProjectID, &t.Seq, &t.ParentTaskID,
		&t.Title, &t.Description, &t.Status, &t.Priority,
		&t.AssigneeID, &t.ReporterID, &t.DueDate, &t.EstimateHours, &t.SortOrder,
		&t.StartedAt, &t.CompletedAt,
		&t.ProjectCode, &t.ProjectName,
		&t.AssigneeName, &t.AssigneeAvatar, &t.ReporterName,
		&t.CommentCount, &t.AttachmentCount, &t.SubtaskCount, &t.DoneSubtasks,
		&t.SpentMinutes,
		&t.DeletedAt, &t.CreatedAt, &t.UpdatedAt,
	}
}

func scanTaskRow(row pgx.Row) (*domainproject.Task, error) {
	var t domainproject.Task
	err := row.Scan(taskScanTargets(&t)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainproject.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("đọc công việc: %w", err)
	}
	return &t, nil
}

func (r *TaskRepository) collect(ctx context.Context, q string, args ...any) ([]*domainproject.Task, error) {
	rows, err := r.db.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("liệt kê công việc: %w", err)
	}
	defer rows.Close()

	out := make([]*domainproject.Task, 0)
	for rows.Next() {
		var t domainproject.Task
		if err := rows.Scan(taskScanTargets(&t)...); err != nil {
			return nil, err
		}
		out = append(out, &t)
	}
	return out, rows.Err()
}

func (r *TaskRepository) Create(ctx context.Context, t *domainproject.Task) error {
	const q = `
		INSERT INTO tasks
		  (project_id, seq, parent_task_id, title, description, status, priority,
		   assignee_id, reporter_id, due_date, estimate_hours, sort_order,
		   started_at, completed_at)
		VALUES ($1,$2,$3,$4,NULLIF($5,''),$6,$7,$8,$9,$10,$11,$12,$13,$14)
		RETURNING id, created_at, updated_at`

	err := r.db.QueryRow(ctx, q,
		t.ProjectID, t.Seq, t.ParentTaskID, t.Title, t.Description,
		t.Status, t.Priority, t.AssigneeID, t.ReporterID, t.DueDate,
		t.EstimateHours, t.SortOrder, t.StartedAt, t.CompletedAt).
		Scan(&t.ID, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return fmt.Errorf("tạo công việc: %w", err)
	}
	return nil
}

func (r *TaskRepository) Update(ctx context.Context, t *domainproject.Task) error {
	const q = `
		UPDATE tasks
		SET title = $2, description = NULLIF($3,''), status = $4, priority = $5,
		    assignee_id = $6, due_date = $7, estimate_hours = $8,
		    parent_task_id = $9, started_at = $10, completed_at = $11
		WHERE id = $1 AND deleted_at IS NULL`

	tag, err := r.db.Exec(ctx, q, t.ID, t.Title, t.Description, t.Status, t.Priority,
		t.AssigneeID, t.DueDate, t.EstimateHours, t.ParentTaskID,
		t.StartedAt, t.CompletedAt)
	if err != nil {
		return fmt.Errorf("cập nhật công việc: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainproject.ErrNotFound
	}
	return nil
}

// SoftDelete xoá mềm task và mọi task con của nó.
//
// Xoá cả con trong CÙNG một câu lệnh: làm hai bước thì task cha có thể biến
// mất trong khi task con vẫn hiện trên bảng Kanban, mồ côi và không ai sửa
// được. Khoá ngoại ON DELETE CASCADE không giúp gì ở đây vì đây là xoá mềm.
func (r *TaskRepository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	const q = `
		UPDATE tasks SET deleted_at = NOW()
		WHERE (id = $1 OR parent_task_id = $1) AND deleted_at IS NULL`

	tag, err := r.db.Exec(ctx, q, id)
	if err != nil {
		return fmt.Errorf("xoá công việc: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainproject.ErrNotFound
	}
	return nil
}

func (r *TaskRepository) GetByID(ctx context.Context, id uuid.UUID) (*domainproject.Task, error) {
	q := selectTask + ` WHERE t.id = $1 AND t.deleted_at IS NULL`
	return scanTaskRow(r.db.QueryRow(ctx, q, id))
}

func (r *TaskRepository) List(
	ctx context.Context,
	f domainproject.TaskFilter,
) ([]*domainproject.Task, int, error) {
	sortColumn := map[string]string{
		"created_at": "t.created_at",
		"due_date":   "t.due_date",
		"title":      "t.title",
		"seq":        "t.seq",
		// Sắp theo độ ưu tiên cần thứ tự nghiệp vụ (urgent trước low), không
		// phải thứ tự bảng chữ cái mà enum của PostgreSQL trả về.
		"priority": `CASE t.priority
		                WHEN 'urgent' THEN 0 WHEN 'high' THEN 1
		                WHEN 'medium' THEN 2 ELSE 3 END`,
	}[f.SortBy]
	if sortColumn == "" {
		sortColumn = "t.created_at"
	}
	direction := "ASC"
	if f.SortDesc {
		direction = "DESC"
	}

	const where = `
		WHERE t.deleted_at IS NULL AND p.deleted_at IS NULL
		  AND ($1::uuid IS NULL OR t.project_id = $1)
		  AND ($2::task_status IS NULL OR t.status = $2)
		  AND ($3::task_priority IS NULL OR t.priority = $3)
		  AND ($4::uuid IS NULL OR t.assignee_id = $4)
		  AND ($5::uuid IS NULL OR t.parent_task_id = $5)
		  AND (NOT $6::bool OR t.parent_task_id IS NULL)
		  AND ($7::timestamptz IS NULL OR t.due_date < $7)
		  AND (NOT $8::bool OR t.status <> 'done')
		  AND (NOT $9::bool OR t.project_id = ANY($10::uuid[]))
		  AND ($11::text IS NULL OR
		       f_unaccent(lower(t.title)) LIKE '%' || f_unaccent(lower($11)) || '%')`

	var search any
	if s := strings.TrimSpace(f.Search); s != "" {
		search = s
	}

	// Lát cắt rỗng và nil phải cho ra cùng một mảng rỗng khi RestrictProjects
	// bật — nghĩa là "không thấy dự án nào", chứ không phải "không giới hạn".
	visible := f.VisibleProjectIDs
	if visible == nil {
		visible = []uuid.UUID{}
	}

	args := []any{
		f.ProjectID, f.Status, f.Priority, f.AssigneeID, f.ParentTaskID,
		f.OnlyRoots, f.DueBefore, f.Unfinished,
		f.RestrictProjects, visible, search,
	}

	var total int
	countQ := `SELECT COUNT(*) FROM tasks t JOIN projects p ON p.id = t.project_id` + where
	if err := r.db.QueryRow(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("đếm công việc: %w", err)
	}

	q := selectTask + where +
		` ORDER BY ` + sortColumn + ` ` + direction + ` LIMIT $12 OFFSET $13`

	offset := (f.Page - 1) * f.PageSize
	items, err := r.collect(ctx, q, append(args, f.PageSize, offset)...)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (r *TaskRepository) ListBoard(
	ctx context.Context,
	projectID uuid.UUID,
) ([]*domainproject.Task, error) {
	q := selectTask + `
		WHERE t.project_id = $1 AND t.deleted_at IS NULL
		  AND t.parent_task_id IS NULL
		ORDER BY t.status, t.sort_order, t.created_at`
	return r.collect(ctx, q, projectID)
}

func (r *TaskRepository) ListSubtasks(
	ctx context.Context,
	parentID uuid.UUID,
) ([]*domainproject.Task, error) {
	q := selectTask + `
		WHERE t.parent_task_id = $1 AND t.deleted_at IS NULL
		ORDER BY t.sort_order, t.created_at`
	return r.collect(ctx, q, parentID)
}

func (r *TaskRepository) CountSubtasks(ctx context.Context, parentID uuid.UUID) (int, error) {
	const q = `SELECT COUNT(*) FROM tasks WHERE parent_task_id = $1 AND deleted_at IS NULL`

	var n int
	if err := r.db.QueryRow(ctx, q, parentID).Scan(&n); err != nil {
		return 0, fmt.Errorf("đếm công việc con: %w", err)
	}
	return n, nil
}

func (r *TaskRepository) SortOrderOf(ctx context.Context, taskID uuid.UUID) (float64, error) {
	const q = `SELECT sort_order FROM tasks WHERE id = $1 AND deleted_at IS NULL`

	var v float64
	err := r.db.QueryRow(ctx, q, taskID).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, domainproject.ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("đọc vị trí công việc: %w", err)
	}
	return v, nil
}

func (r *TaskRepository) Reorder(
	ctx context.Context,
	taskID uuid.UUID,
	status domainproject.TaskStatus,
	sortOrder float64,
) error {
	// Cập nhật mốc thời gian ngay trong câu lệnh, theo đúng trạng thái đích.
	//
	// started_at chỉ ghi LẦN ĐẦU vào in_progress (COALESCE giữ giá trị cũ) —
	// kéo task ra rồi kéo lại không được làm mất mốc bắt đầu thật.
	// $2 được ép kiểu TƯỜNG MINH ở mọi chỗ xuất hiện.
	//
	// Thiếu ép kiểu, PostgreSQL nhìn thấy `$2 = 'todo'` trong CASE và suy ra
	// $2 là `text`, rồi `status = $2` trở thành so sánh task_status với text
	// — không có toán tử nào như vậy, câu lệnh hỏng lúc chạy. Suy luận kiểu
	// chỉ đi theo một chiều và lần xuất hiện đầu tiên không phải lúc nào
	// cũng thắng.
	const q = `
		UPDATE tasks
		SET status = $2::task_status,
		    sort_order = $3,
		    started_at = CASE
		        WHEN $2::task_status = 'todo' THEN NULL
		        WHEN started_at IS NULL THEN NOW()
		        ELSE started_at END,
		    completed_at = CASE WHEN $2::task_status = 'done' THEN COALESCE(completed_at, NOW())
		                        ELSE NULL END
		WHERE id = $1 AND deleted_at IS NULL`

	tag, err := r.db.Exec(ctx, q, taskID, status, sortOrder)
	if err != nil {
		return fmt.Errorf("sắp xếp lại công việc: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainproject.ErrNotFound
	}
	return nil
}

// RenumberColumn đánh số lại cả cột theo bước 1000.
//
// Bước rộng để sau đó còn chỗ chèn: giữa 1000 và 2000 có thể chia đôi khoảng
// 50 lần trước khi float64 hết khả năng phân biệt.
func (r *TaskRepository) RenumberColumn(
	ctx context.Context,
	projectID uuid.UUID,
	status domainproject.TaskStatus,
) error {
	const q = `
		UPDATE tasks t
		SET sort_order = ranked.rn * 1000
		FROM (
		    SELECT id, ROW_NUMBER() OVER (ORDER BY sort_order, created_at) AS rn
		    FROM tasks
		    WHERE project_id = $1 AND status = $2
		      AND parent_task_id IS NULL AND deleted_at IS NULL
		) ranked
		WHERE t.id = ranked.id`

	if _, err := r.db.Exec(ctx, q, projectID, status); err != nil {
		return fmt.Errorf("đánh số lại cột Kanban: %w", err)
	}
	return nil
}

func (r *TaskRepository) MaxSortOrder(
	ctx context.Context,
	projectID uuid.UUID,
	status domainproject.TaskStatus,
) (float64, error) {
	const q = `
		SELECT COALESCE(MAX(sort_order), 0) FROM tasks
		WHERE project_id = $1 AND status = $2
		  AND parent_task_id IS NULL AND deleted_at IS NULL`

	var v float64
	if err := r.db.QueryRow(ctx, q, projectID, status).Scan(&v); err != nil {
		return 0, fmt.Errorf("đọc vị trí lớn nhất: %w", err)
	}
	return v, nil
}
