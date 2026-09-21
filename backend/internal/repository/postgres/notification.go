package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/PhamVanPhuc2k2/manage/internal/domain/notification"
	"github.com/PhamVanPhuc2k2/manage/pkg/postgres"
)

type NotificationRepository struct {
	db *postgres.DB
}

func NewNotificationRepository(db *postgres.DB) *NotificationRepository {
	return &NotificationRepository{db: db}
}

const selectNotification = `
SELECT n.id, n.employee_id, n.type, n.title, COALESCE(n.body,''), COALESCE(n.link,''),
       n.actor_id, COALESCE(n.resource,''), n.resource_id,
       n.read_at, n.emailed_at, n.created_at,
       COALESCE(a.full_name,'')
FROM notifications n
LEFT JOIN employees a ON a.id = n.actor_id
`

func scanNotification(row pgx.Row) (*notification.Notification, error) {
	var n notification.Notification
	err := row.Scan(&n.ID, &n.EmployeeID, &n.Type, &n.Title, &n.Body, &n.Link,
		&n.ActorID, &n.Resource, &n.ResourceID,
		&n.ReadAt, &n.EmailedAt, &n.CreatedAt, &n.ActorName)
	if err != nil {
		return nil, fmt.Errorf("đọc thông báo: %w", err)
	}
	return &n, nil
}

// CreateMany ghi nhiều thông báo bằng MỘT câu INSERT nhiều dòng.
//
// Dựng tham số động thay vì dùng CopyFrom: số người nhận một sự kiện thường
// chỉ vài chục, và CopyFrom không trả về id đã sinh — mà id thì cần ngay để
// đẩy realtime.
func (r *NotificationRepository) CreateMany(
	ctx context.Context,
	items []*notification.Notification,
) error {
	if len(items) == 0 {
		return nil
	}

	var (
		b    strings.Builder
		args = make([]any, 0, len(items)*8)
	)
	b.WriteString(`INSERT INTO notifications
		(employee_id, type, title, body, link, actor_id, resource, resource_id)
		VALUES `)

	for i, it := range items {
		if i > 0 {
			b.WriteString(", ")
		}
		p := i * 8
		fmt.Fprintf(&b, "($%d, $%d::notification_type, $%d, $%d, $%d, $%d, $%d, $%d)",
			p+1, p+2, p+3, p+4, p+5, p+6, p+7, p+8)

		args = append(args, it.EmployeeID, string(it.Type), it.Title,
			nullStr(it.Body), nullStr(it.Link), it.ActorID,
			nullStr(it.Resource), it.ResourceID)
	}
	b.WriteString(" RETURNING id, created_at")

	rows, err := r.db.Query(ctx, b.String(), args...)
	if err != nil {
		return fmt.Errorf("tạo thông báo: %w", err)
	}
	defer rows.Close()

	// Gán id và thời điểm trở lại đúng từng phần tử, theo thứ tự INSERT.
	// Postgres trả RETURNING theo thứ tự VALUES nên chỉ số khớp nhau.
	i := 0
	for rows.Next() {
		if err := rows.Scan(&items[i].ID, &items[i].CreatedAt); err != nil {
			return err
		}
		i++
	}
	return rows.Err()
}

func (r *NotificationRepository) List(
	ctx context.Context,
	f notification.Filter,
) ([]*notification.Notification, error) {
	var (
		where = []string{"n.employee_id = $1"}
		args  = []any{f.EmployeeID}
	)

	if f.UnreadOnly {
		where = append(where, "n.read_at IS NULL")
	}
	if f.Type != nil {
		args = append(args, string(*f.Type))
		where = append(where, fmt.Sprintf("n.type = $%d::notification_type", len(args)))
	}
	if f.Before != nil {
		args = append(args, *f.Before)
		where = append(where, fmt.Sprintf("n.created_at < $%d", len(args)))
	}

	limit := f.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	args = append(args, limit)

	q := selectNotification +
		" WHERE " + strings.Join(where, " AND ") +
		fmt.Sprintf(" ORDER BY n.created_at DESC LIMIT $%d", len(args))

	rows, err := r.db.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("liệt kê thông báo: %w", err)
	}
	defer rows.Close()

	out := make([]*notification.Notification, 0, limit)
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (r *NotificationRepository) CountUnread(ctx context.Context, employeeID uuid.UUID) (int, error) {
	const q = `SELECT COUNT(*) FROM notifications WHERE employee_id = $1 AND read_at IS NULL`

	var n int
	if err := r.db.QueryRow(ctx, q, employeeID).Scan(&n); err != nil {
		return 0, fmt.Errorf("đếm thông báo chưa đọc: %w", err)
	}
	return n, nil
}

// MarkRead đánh dấu đã đọc. Điều kiện employee_id nằm ngay trong câu UPDATE.
//
// Đặt ở đây chứ không kiểm tra trước bằng một câu SELECT: kiểm tra rồi mới ghi
// là hai lượt và vẫn còn khe hở, còn ràng buộc trong WHERE thì không ai đánh
// dấu hộ thông báo của người khác được, dù gửi id nào lên.
func (r *NotificationRepository) MarkRead(
	ctx context.Context,
	employeeID uuid.UUID,
	ids []uuid.UUID,
) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	const q = `
		UPDATE notifications SET read_at = NOW()
		WHERE employee_id = $1 AND id = ANY($2) AND read_at IS NULL`

	tag, err := r.db.Exec(ctx, q, employeeID, ids)
	if err != nil {
		return 0, fmt.Errorf("đánh dấu đã đọc: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (r *NotificationRepository) MarkAllRead(ctx context.Context, employeeID uuid.UUID) (int64, error) {
	const q = `UPDATE notifications SET read_at = NOW() WHERE employee_id = $1 AND read_at IS NULL`

	tag, err := r.db.Exec(ctx, q, employeeID)
	if err != nil {
		return 0, fmt.Errorf("đánh dấu đã đọc tất cả: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (r *NotificationRepository) ListMuted(
	ctx context.Context,
	employeeID uuid.UUID,
) ([]notification.Type, error) {
	const q = `SELECT type FROM notification_mutes WHERE employee_id = $1`

	rows, err := r.db.Query(ctx, q, employeeID)
	if err != nil {
		return nil, fmt.Errorf("đọc cấu hình thông báo: %w", err)
	}
	defer rows.Close()

	var out []notification.Type
	for rows.Next() {
		var t notification.Type
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// SetMuted bật/tắt một loại. Bật lại là XOÁ dòng, không phải đặt cờ false.
//
// Bảng chỉ chứa những loại đã tắt, nên nó rỗng với phần lớn người dùng.
func (r *NotificationRepository) SetMuted(
	ctx context.Context,
	employeeID uuid.UUID,
	t notification.Type,
	muted bool,
) error {
	q := `DELETE FROM notification_mutes WHERE employee_id = $1 AND type = $2::notification_type`
	if muted {
		q = `INSERT INTO notification_mutes (employee_id, type)
		     VALUES ($1, $2::notification_type) ON CONFLICT DO NOTHING`
	}

	if _, err := r.db.Exec(ctx, q, employeeID, string(t)); err != nil {
		return fmt.Errorf("lưu cấu hình thông báo: %w", err)
	}
	return nil
}

func (r *NotificationRepository) MutedByMany(
	ctx context.Context,
	employeeIDs []uuid.UUID,
	t notification.Type,
) (map[uuid.UUID]bool, error) {
	out := make(map[uuid.UUID]bool, len(employeeIDs))
	if len(employeeIDs) == 0 {
		return out, nil
	}

	const q = `
		SELECT employee_id FROM notification_mutes
		WHERE employee_id = ANY($1) AND type = $2::notification_type`

	rows, err := r.db.Query(ctx, q, employeeIDs, string(t))
	if err != nil {
		return nil, fmt.Errorf("tra cấu hình thông báo: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

func (r *NotificationRepository) PendingEmail(
	ctx context.Context,
	olderThan time.Time,
	limit int,
) ([]*notification.Notification, error) {
	q := selectNotification + `
		WHERE n.read_at IS NULL AND n.emailed_at IS NULL AND n.created_at < $1
		ORDER BY n.created_at LIMIT $2`

	rows, err := r.db.Query(ctx, q, olderThan, limit)
	if err != nil {
		return nil, fmt.Errorf("tìm thông báo cần gửi mail: %w", err)
	}
	defer rows.Close()

	var out []*notification.Notification
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// MarkEmailed đánh dấu đã gửi mail.
//
// Gọi NGAY sau khi đẩy vào hàng đợi chứ không chờ gửi xong: job chạy lại mỗi
// vài phút, và một email chậm còn hơn mười email trùng.
func (r *NotificationRepository) MarkEmailed(ctx context.Context, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}

	const q = `UPDATE notifications SET emailed_at = NOW() WHERE id = ANY($1)`

	if _, err := r.db.Exec(ctx, q, ids); err != nil {
		return fmt.Errorf("đánh dấu đã gửi mail: %w", err)
	}
	return nil
}

// nullStr đổi chuỗi rỗng thành NULL.
//
// Cột body/link/resource cho phép NULL, và lưu chuỗi rỗng ở đó tạo ra hai
// cách biểu diễn cùng một ý "không có" — mọi truy vấn về sau phải nhớ kiểm
// tra cả hai.
func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
