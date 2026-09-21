package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/PhamVanPhuc2k2/manage/internal/domain/chat"
	"github.com/PhamVanPhuc2k2/manage/pkg/postgres"
)

type ChatMessageRepository struct {
	db *postgres.DB
}

func NewChatMessageRepository(db *postgres.DB) *ChatMessageRepository {
	return &ChatMessageRepository{db: db}
}

// selectMessage nạp tin nhắn kèm tên người gửi và trích dẫn tin được trả lời.
//
// Lấy luôn nội dung tin gốc trong cùng câu truy vấn thay vì để client tự tra:
// một trang lịch sử 50 tin mà mỗi tin trả lời lại gọi thêm một lượt là 50 lượt
// round-trip cho một việc JOIN làm xong.
const selectMessage = `
SELECT m.id, m.conversation_id, m.sender_id, m.kind, m.content, m.reply_to_id,
       COALESCE(m.client_message_id,''), m.edited_at, m.deleted_at, m.created_at,
       COALESCE(s.full_name,''),
       COALESCE(rs.full_name,''),
       COALESCE(CASE WHEN r.deleted_at IS NULL THEN LEFT(r.content, 120) END, '')
FROM messages m
LEFT JOIN employees s  ON s.id  = m.sender_id
LEFT JOIN messages  r  ON r.id  = m.reply_to_id
LEFT JOIN employees rs ON rs.id = r.sender_id
`

func scanMessage(row pgx.Row) (*chat.Message, error) {
	var m chat.Message
	err := row.Scan(&m.ID, &m.ConversationID, &m.SenderID, &m.Kind, &m.Content,
		&m.ReplyToID, &m.ClientMessageID, &m.EditedAt, &m.DeletedAt, &m.CreatedAt,
		&m.SenderName, &m.ReplyToSender, &m.ReplyToContent)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, chat.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("đọc tin nhắn: %w", err)
	}
	return &m, nil
}

func (r *ChatMessageRepository) Create(ctx context.Context, m *chat.Message) error {
	const q = `
		INSERT INTO messages
		  (conversation_id, sender_id, kind, content, reply_to_id, client_message_id)
		VALUES ($1, $2, $3::message_kind, $4, $5, $6)
		RETURNING id, created_at`

	err := r.db.QueryRow(ctx, q, m.ConversationID, m.SenderID, string(m.Kind),
		m.Content, m.ReplyToID, nullStr(m.ClientMessageID)).
		Scan(&m.ID, &m.CreatedAt)
	if err != nil {
		return fmt.Errorf("ghi tin nhắn: %w", err)
	}
	return nil
}

// ByClientID tra tin đã ghi theo mã do client sinh.
//
// Trả về (nil, nil) khi không có, chứ không phải ErrNotFound: "chưa từng gửi"
// là kết quả bình thường và hay gặp nhất của phép tra này, không phải lỗi.
func (r *ChatMessageRepository) ByClientID(
	ctx context.Context,
	conversationID, senderID uuid.UUID,
	clientID string,
) (*chat.Message, error) {
	if clientID == "" {
		return nil, nil
	}

	q := selectMessage + `
		WHERE m.conversation_id = $1 AND m.sender_id = $2 AND m.client_message_id = $3`

	msg, err := scanMessage(r.db.QueryRow(ctx, q, conversationID, senderID, clientID))
	if errors.Is(err, chat.ErrNotFound) {
		return nil, nil
	}
	return msg, err
}

func (r *ChatMessageRepository) ByID(ctx context.Context, id uuid.UUID) (*chat.Message, error) {
	return scanMessage(r.db.QueryRow(ctx, selectMessage+" WHERE m.id = $1", id))
}

// List đọc lịch sử, mới nhất trước, rồi ĐẢO lại trước khi trả về.
//
// Truy vấn phải sắp giảm dần để LIMIT lấy đúng những tin sát cursor, nhưng
// khung chat hiển thị tăng dần. Đảo ở đây để mọi chỗ gọi không phải nhớ làm.
func (r *ChatMessageRepository) List(
	ctx context.Context,
	f chat.MessageFilter,
) ([]*chat.Message, error) {
	var (
		where = []string{"m.conversation_id = $1"}
		args  = []any{f.ConversationID}
	)

	if f.Before != nil {
		args = append(args, *f.Before)
		where = append(where, fmt.Sprintf("m.created_at < $%d", len(args)))
	}

	if s := strings.TrimSpace(f.Search); s != "" {
		// f_unaccent ở cả hai vế: search_vector lưu bản đã bỏ dấu, nên từ khoá
		// cũng phải bỏ dấu thì mới khớp. Thiếu một vế là tìm gì cũng ra rỗng.
		args = append(args, s)
		where = append(where, fmt.Sprintf(
			"m.search_vector @@ plainto_tsquery('simple', f_unaccent($%d))", len(args)))
		where = append(where, "m.deleted_at IS NULL")
	}

	limit := f.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	args = append(args, limit)

	q := selectMessage +
		" WHERE " + strings.Join(where, " AND ") +
		fmt.Sprintf(" ORDER BY m.created_at DESC, m.id DESC LIMIT $%d", len(args))

	rows, err := r.db.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("đọc lịch sử tin nhắn: %w", err)
	}
	defer rows.Close()

	out := make([]*chat.Message, 0, limit)
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

func (r *ChatMessageRepository) Edit(ctx context.Context, id uuid.UUID, content string) error {
	const q = `
		UPDATE messages SET content = $2, edited_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL`

	tag, err := r.db.Exec(ctx, q, id, content)
	if err != nil {
		return fmt.Errorf("sửa tin nhắn: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return chat.ErrNotFound
	}
	return nil
}

// SoftDelete thu hồi tin nhắn: xoá nội dung nhưng GIỮ dòng.
//
// Giữ dòng để tin trả lời nó không mất ngữ cảnh và để thứ tự lịch sử không
// thủng. Xoá luôn content vì thu hồi mà nội dung vẫn nằm trong database là
// thu hồi trên danh nghĩa.
func (r *ChatMessageRepository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	const q = `
		UPDATE messages SET deleted_at = NOW(), content = ''
		WHERE id = $1 AND deleted_at IS NULL`

	tag, err := r.db.Exec(ctx, q, id)
	if err != nil {
		return fmt.Errorf("thu hồi tin nhắn: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return chat.ErrNotFound
	}
	return nil
}

// =========================================================================
// TỆP ĐÍNH KÈM
// =========================================================================

func (r *ChatMessageRepository) AddAttachments(
	ctx context.Context,
	items []*chat.Attachment,
) error {
	if len(items) == 0 {
		return nil
	}

	var (
		b    strings.Builder
		args = make([]any, 0, len(items)*7)
	)
	b.WriteString(`INSERT INTO message_attachments
		(message_id, storage_key, file_name, content_type, size_bytes, width, height)
		VALUES `)

	for i, it := range items {
		if i > 0 {
			b.WriteString(", ")
		}
		p := i * 7
		fmt.Fprintf(&b, "($%d, $%d, $%d, $%d, $%d, $%d, $%d)",
			p+1, p+2, p+3, p+4, p+5, p+6, p+7)

		args = append(args, it.MessageID, it.StorageKey, it.FileName,
			it.ContentType, it.SizeBytes, it.Width, it.Height)
	}
	b.WriteString(" RETURNING id, created_at")

	rows, err := r.db.Query(ctx, b.String(), args...)
	if err != nil {
		return fmt.Errorf("ghi tệp đính kèm: %w", err)
	}
	defer rows.Close()

	i := 0
	for rows.Next() {
		if err := rows.Scan(&items[i].ID, &items[i].CreatedAt); err != nil {
			return err
		}
		i++
	}
	return rows.Err()
}

func (r *ChatMessageRepository) AttachmentsOf(
	ctx context.Context,
	messageIDs []uuid.UUID,
) (map[uuid.UUID][]*chat.Attachment, error) {
	out := make(map[uuid.UUID][]*chat.Attachment, len(messageIDs))
	if len(messageIDs) == 0 {
		return out, nil
	}

	const q = `
		SELECT id, message_id, storage_key, file_name, content_type, size_bytes,
		       width, height, created_at
		FROM message_attachments
		WHERE message_id = ANY($1)
		ORDER BY created_at`

	rows, err := r.db.Query(ctx, q, messageIDs)
	if err != nil {
		return nil, fmt.Errorf("đọc tệp đính kèm: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var a chat.Attachment
		if err := rows.Scan(&a.ID, &a.MessageID, &a.StorageKey, &a.FileName,
			&a.ContentType, &a.SizeBytes, &a.Width, &a.Height, &a.CreatedAt); err != nil {
			return nil, err
		}
		out[a.MessageID] = append(out[a.MessageID], &a)
	}
	return out, rows.Err()
}
