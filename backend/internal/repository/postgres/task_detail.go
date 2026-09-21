// Bình luận, tệp đính kèm, nhật ký thay đổi và ghi nhận thời gian của task.
//
// Bốn bảng này gom vào một file vì chúng đều là dữ liệu phụ thuộc hoàn toàn
// vào một task, có vòng đời gắn với task đó, và mỗi repository chỉ vài chục
// dòng — tách thành bốn file sẽ khó theo dõi hơn là dễ.
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

// =========================================================================
// BÌNH LUẬN
// =========================================================================

type TaskCommentRepository struct {
	db *postgres.DB
}

func NewTaskCommentRepository(db *postgres.DB) *TaskCommentRepository {
	return &TaskCommentRepository{db: db}
}

const selectComment = `
SELECT c.id, c.task_id, c.author_id, c.content, c.mentioned_ids, c.edited_at,
       COALESCE(a.full_name,''), COALESCE(a.avatar_key,''),
       c.deleted_at, c.created_at, c.updated_at
FROM task_comments c
LEFT JOIN employees a ON a.id = c.author_id
`

func (r *TaskCommentRepository) Create(ctx context.Context, c *domainproject.Comment) error {
	const q = `
		INSERT INTO task_comments (task_id, author_id, content, mentioned_ids)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at, updated_at`

	// mentioned_ids không được là nil: cột khai báo NOT NULL DEFAULT '{}',
	// nhưng truyền nil qua tham số sẽ ghi NULL chứ không kích hoạt DEFAULT.
	ids := c.MentionedIDs
	if ids == nil {
		ids = []uuid.UUID{}
	}

	err := r.db.QueryRow(ctx, q, c.TaskID, c.AuthorID, c.Content, ids).
		Scan(&c.ID, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return fmt.Errorf("tạo bình luận: %w", err)
	}
	return nil
}

func (r *TaskCommentRepository) Update(ctx context.Context, c *domainproject.Comment) error {
	const q = `
		UPDATE task_comments
		SET content = $2, mentioned_ids = $3, edited_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL`

	ids := c.MentionedIDs
	if ids == nil {
		ids = []uuid.UUID{}
	}

	tag, err := r.db.Exec(ctx, q, c.ID, c.Content, ids)
	if err != nil {
		return fmt.Errorf("sửa bình luận: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainproject.ErrNotFound
	}
	return nil
}

func (r *TaskCommentRepository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	const q = `UPDATE task_comments SET deleted_at = NOW() WHERE id = $1 AND deleted_at IS NULL`

	tag, err := r.db.Exec(ctx, q, id)
	if err != nil {
		return fmt.Errorf("xoá bình luận: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainproject.ErrNotFound
	}
	return nil
}

func (r *TaskCommentRepository) GetByID(
	ctx context.Context,
	id uuid.UUID,
) (*domainproject.Comment, error) {
	q := selectComment + ` WHERE c.id = $1 AND c.deleted_at IS NULL`

	var c domainproject.Comment
	err := r.db.QueryRow(ctx, q, id).Scan(
		&c.ID, &c.TaskID, &c.AuthorID, &c.Content, &c.MentionedIDs, &c.EditedAt,
		&c.AuthorName, &c.AuthorAvatar, &c.DeletedAt, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainproject.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("đọc bình luận: %w", err)
	}
	return &c, nil
}

func (r *TaskCommentRepository) ListByTask(
	ctx context.Context,
	taskID uuid.UUID,
) ([]*domainproject.Comment, error) {
	q := selectComment + `
		WHERE c.task_id = $1 AND c.deleted_at IS NULL
		ORDER BY c.created_at`

	rows, err := r.db.Query(ctx, q, taskID)
	if err != nil {
		return nil, fmt.Errorf("liệt kê bình luận: %w", err)
	}
	defer rows.Close()

	out := make([]*domainproject.Comment, 0)
	for rows.Next() {
		var c domainproject.Comment
		if err := rows.Scan(
			&c.ID, &c.TaskID, &c.AuthorID, &c.Content, &c.MentionedIDs, &c.EditedAt,
			&c.AuthorName, &c.AuthorAvatar, &c.DeletedAt, &c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, &c)
	}
	return out, rows.Err()
}

func (r *TaskCommentRepository) CountByTask(ctx context.Context, taskID uuid.UUID) (int, error) {
	const q = `SELECT COUNT(*) FROM task_comments WHERE task_id = $1 AND deleted_at IS NULL`

	var n int
	if err := r.db.QueryRow(ctx, q, taskID).Scan(&n); err != nil {
		return 0, fmt.Errorf("đếm bình luận: %w", err)
	}
	return n, nil
}

// =========================================================================
// TỆP ĐÍNH KÈM
// =========================================================================

type TaskAttachmentRepository struct {
	db *postgres.DB
}

func NewTaskAttachmentRepository(db *postgres.DB) *TaskAttachmentRepository {
	return &TaskAttachmentRepository{db: db}
}

const selectAttachment = `
SELECT a.id, a.task_id, a.uploaded_by, a.storage_key, a.file_name,
       a.content_type, a.size_bytes, a.created_at, COALESCE(e.full_name,'')
FROM task_attachments a
LEFT JOIN employees e ON e.id = a.uploaded_by
`

func (r *TaskAttachmentRepository) Create(ctx context.Context, a *domainproject.Attachment) error {
	const q = `
		INSERT INTO task_attachments
		  (task_id, uploaded_by, storage_key, file_name, content_type, size_bytes)
		VALUES ($1,$2,$3,$4,$5,$6)
		RETURNING id, created_at`

	err := r.db.QueryRow(ctx, q, a.TaskID, a.UploadedBy, a.StorageKey,
		a.FileName, a.ContentType, a.SizeBytes).Scan(&a.ID, &a.CreatedAt)
	if err != nil {
		return fmt.Errorf("lưu tệp đính kèm: %w", err)
	}
	return nil
}

// Delete xoá cứng. Bản ghi đính kèm không có giá trị lịch sử sau khi tệp
// trên R2 đã bị xoá — giữ lại chỉ tạo liên kết hỏng.
func (r *TaskAttachmentRepository) Delete(ctx context.Context, id uuid.UUID) error {
	const q = `DELETE FROM task_attachments WHERE id = $1`

	tag, err := r.db.Exec(ctx, q, id)
	if err != nil {
		return fmt.Errorf("xoá tệp đính kèm: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainproject.ErrNotFound
	}
	return nil
}

func (r *TaskAttachmentRepository) GetByID(
	ctx context.Context,
	id uuid.UUID,
) (*domainproject.Attachment, error) {
	q := selectAttachment + ` WHERE a.id = $1`

	var a domainproject.Attachment
	err := r.db.QueryRow(ctx, q, id).Scan(
		&a.ID, &a.TaskID, &a.UploadedBy, &a.StorageKey, &a.FileName,
		&a.ContentType, &a.SizeBytes, &a.CreatedAt, &a.UploaderName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainproject.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("đọc tệp đính kèm: %w", err)
	}
	return &a, nil
}

func (r *TaskAttachmentRepository) ListByTask(
	ctx context.Context,
	taskID uuid.UUID,
) ([]*domainproject.Attachment, error) {
	q := selectAttachment + ` WHERE a.task_id = $1 ORDER BY a.created_at DESC`

	rows, err := r.db.Query(ctx, q, taskID)
	if err != nil {
		return nil, fmt.Errorf("liệt kê tệp đính kèm: %w", err)
	}
	defer rows.Close()

	out := make([]*domainproject.Attachment, 0)
	for rows.Next() {
		var a domainproject.Attachment
		if err := rows.Scan(&a.ID, &a.TaskID, &a.UploadedBy, &a.StorageKey,
			&a.FileName, &a.ContentType, &a.SizeBytes, &a.CreatedAt,
			&a.UploaderName); err != nil {
			return nil, err
		}
		out = append(out, &a)
	}
	return out, rows.Err()
}

// =========================================================================
// NHẬT KÝ THAY ĐỔI
// =========================================================================

type TaskActivityRepository struct {
	db *postgres.DB
}

func NewTaskActivityRepository(db *postgres.DB) *TaskActivityRepository {
	return &TaskActivityRepository{db: db}
}

func (r *TaskActivityRepository) Log(ctx context.Context, a *domainproject.Activity) error {
	const q = `
		INSERT INTO task_activities (task_id, actor_id, action, field, old_value, new_value)
		VALUES ($1, $2, $3, NULLIF($4,''), NULLIF($5,''), NULLIF($6,''))
		RETURNING id, created_at`

	err := r.db.QueryRow(ctx, q, a.TaskID, a.ActorID, a.Action,
		a.Field, a.OldValue, a.NewValue).Scan(&a.ID, &a.CreatedAt)
	if err != nil {
		return fmt.Errorf("ghi nhật ký công việc: %w", err)
	}
	return nil
}

func (r *TaskActivityRepository) ListByTask(
	ctx context.Context,
	taskID uuid.UUID,
	limit int,
) ([]*domainproject.Activity, error) {
	const q = `
		SELECT a.id, a.task_id, a.actor_id, a.action,
		       COALESCE(a.field,''), COALESCE(a.old_value,''), COALESCE(a.new_value,''),
		       a.created_at, COALESCE(e.full_name,'')
		FROM task_activities a
		LEFT JOIN employees e ON e.id = a.actor_id
		WHERE a.task_id = $1
		ORDER BY a.created_at DESC
		LIMIT $2`

	rows, err := r.db.Query(ctx, q, taskID, limit)
	if err != nil {
		return nil, fmt.Errorf("liệt kê nhật ký: %w", err)
	}
	defer rows.Close()

	out := make([]*domainproject.Activity, 0)
	for rows.Next() {
		var a domainproject.Activity
		if err := rows.Scan(&a.ID, &a.TaskID, &a.ActorID, &a.Action,
			&a.Field, &a.OldValue, &a.NewValue, &a.CreatedAt, &a.ActorName); err != nil {
			return nil, err
		}
		out = append(out, &a)
	}
	return out, rows.Err()
}

// =========================================================================
// GHI NHẬN THỜI GIAN
// =========================================================================

type TaskTimelogRepository struct {
	db *postgres.DB
}

func NewTaskTimelogRepository(db *postgres.DB) *TaskTimelogRepository {
	return &TaskTimelogRepository{db: db}
}

const selectTimelog = `
SELECT tl.id, tl.task_id, tl.employee_id, tl.spent_minutes,
       COALESCE(tl.note,''), tl.logged_on, tl.created_at,
       COALESCE(e.full_name,''), COALESCE(t.title,'')
FROM task_timelogs tl
LEFT JOIN employees e ON e.id = tl.employee_id
LEFT JOIN tasks     t ON t.id = tl.task_id
`

func (r *TaskTimelogRepository) Create(ctx context.Context, t *domainproject.Timelog) error {
	const q = `
		INSERT INTO task_timelogs (task_id, employee_id, spent_minutes, note, logged_on)
		VALUES ($1, $2, $3, NULLIF($4,''), $5)
		RETURNING id, created_at`

	err := r.db.QueryRow(ctx, q, t.TaskID, t.EmployeeID, t.SpentMinutes,
		t.Note, t.LoggedOn).Scan(&t.ID, &t.CreatedAt)
	if err != nil {
		return fmt.Errorf("ghi nhận thời gian: %w", err)
	}
	return nil
}

func (r *TaskTimelogRepository) Delete(ctx context.Context, id uuid.UUID) error {
	const q = `DELETE FROM task_timelogs WHERE id = $1`

	tag, err := r.db.Exec(ctx, q, id)
	if err != nil {
		return fmt.Errorf("xoá bản ghi thời gian: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domainproject.ErrNotFound
	}
	return nil
}

func (r *TaskTimelogRepository) GetByID(
	ctx context.Context,
	id uuid.UUID,
) (*domainproject.Timelog, error) {
	q := selectTimelog + ` WHERE tl.id = $1`

	var t domainproject.Timelog
	err := r.db.QueryRow(ctx, q, id).Scan(&t.ID, &t.TaskID, &t.EmployeeID,
		&t.SpentMinutes, &t.Note, &t.LoggedOn, &t.CreatedAt,
		&t.EmployeeName, &t.TaskTitle)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainproject.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("đọc bản ghi thời gian: %w", err)
	}
	return &t, nil
}

func (r *TaskTimelogRepository) ListByTask(
	ctx context.Context,
	taskID uuid.UUID,
) ([]*domainproject.Timelog, error) {
	q := selectTimelog + ` WHERE tl.task_id = $1 ORDER BY tl.logged_on DESC, tl.created_at DESC`

	rows, err := r.db.Query(ctx, q, taskID)
	if err != nil {
		return nil, fmt.Errorf("liệt kê thời gian: %w", err)
	}
	defer rows.Close()

	out := make([]*domainproject.Timelog, 0)
	for rows.Next() {
		var t domainproject.Timelog
		if err := rows.Scan(&t.ID, &t.TaskID, &t.EmployeeID, &t.SpentMinutes,
			&t.Note, &t.LoggedOn, &t.CreatedAt, &t.EmployeeName, &t.TaskTitle); err != nil {
			return nil, err
		}
		out = append(out, &t)
	}
	return out, rows.Err()
}

func (r *TaskTimelogRepository) SumMinutesByTask(ctx context.Context, taskID uuid.UUID) (int, error) {
	const q = `SELECT COALESCE(SUM(spent_minutes),0) FROM task_timelogs WHERE task_id = $1`

	var n int
	if err := r.db.QueryRow(ctx, q, taskID).Scan(&n); err != nil {
		return 0, fmt.Errorf("tổng thời gian: %w", err)
	}
	return n, nil
}
