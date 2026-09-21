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

type ChatConversationRepository struct {
	db *postgres.DB
}

func NewChatConversationRepository(db *postgres.DB) *ChatConversationRepository {
	return &ChatConversationRepository{db: db}
}

// unreadExpr đếm tin chưa đọc của một thành viên.
//
// So sánh theo created_at của mốc đã đọc chứ không theo id: uuid_generate_v4
// sinh id ngẫu nhiên nên không sắp thứ tự được. Mốc NULL nghĩa là chưa đọc gì,
// khi đó đếm toàn bộ.
const unreadExpr = `
  (SELECT COUNT(*) FROM messages msg
   WHERE msg.conversation_id = c.id
     AND msg.deleted_at IS NULL
     AND msg.sender_id IS DISTINCT FROM cm.employee_id
     AND (cm.last_read_at IS NULL OR msg.created_at > cm.last_read_at))`

// selectConversation nạp hội thoại KÈM góc nhìn của người xem ($1).
//
// Tên hiển thị của hội thoại 1-1 lấy từ người đối diện, tính ngay trong câu
// truy vấn: với A thì nó tên là B, với B thì nó tên là A, nên không có cách
// nào lưu sẵn một tên đúng cho cả hai.
const selectConversation = `
SELECT c.id, c.company_id, c.kind, COALESCE(c.name,''),
       c.department_id, c.project_id, COALESCE(c.direct_key,''),
       c.created_by, c.last_message_at, c.deleted_at, c.created_at, c.updated_at,
       cm.is_admin, cm.is_pinned, cm.is_muted,
       (SELECT COUNT(*) FROM conversation_members x
        WHERE x.conversation_id = c.id AND x.left_at IS NULL),
       ` + unreadExpr + `,
       peer.id, COALESCE(peer.full_name,'')
FROM conversations c
JOIN conversation_members cm
  ON cm.conversation_id = c.id AND cm.employee_id = $1 AND cm.left_at IS NULL
LEFT JOIN LATERAL (
  SELECT e.id, e.full_name
  FROM conversation_members p
  JOIN employees e ON e.id = p.employee_id
  WHERE c.kind = 'direct' AND p.conversation_id = c.id AND p.employee_id <> $1
  LIMIT 1
) peer ON TRUE
`

func scanConversation(row pgx.Row) (*chat.Conversation, error) {
	var c chat.Conversation
	err := row.Scan(&c.ID, &c.CompanyID, &c.Kind, &c.Name,
		&c.DepartmentID, &c.ProjectID, &c.DirectKey,
		&c.CreatedBy, &c.LastMessageAt, &c.DeletedAt, &c.CreatedAt, &c.UpdatedAt,
		&c.IsAdmin, &c.IsPinned, &c.IsMuted,
		&c.MemberCount, &c.UnreadCount,
		&c.PeerID, &c.DisplayName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, chat.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("đọc hội thoại: %w", err)
	}

	// Nhóm thì dùng tên đã lưu; chỉ hội thoại 1-1 mới lấy tên người đối diện.
	if c.Kind != chat.KindDirect {
		c.DisplayName = c.Name
	}
	return &c, nil
}

func (r *ChatConversationRepository) Create(ctx context.Context, c *chat.Conversation) error {
	const q = `
		INSERT INTO conversations
		  (company_id, kind, name, department_id, project_id, direct_key, created_by)
		VALUES ($1, $2::conversation_kind, $3, $4, $5, $6, $7)
		RETURNING id, created_at, updated_at`

	err := r.db.QueryRow(ctx, q, c.CompanyID, string(c.Kind), nullStr(c.Name),
		c.DepartmentID, c.ProjectID, nullStr(c.DirectKey), c.CreatedBy).
		Scan(&c.ID, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return fmt.Errorf("tạo hội thoại: %w", err)
	}
	return nil
}

func (r *ChatConversationRepository) Update(ctx context.Context, c *chat.Conversation) error {
	const q = `UPDATE conversations SET name = $2 WHERE id = $1 AND deleted_at IS NULL`

	tag, err := r.db.Exec(ctx, q, c.ID, nullStr(c.Name))
	if err != nil {
		return fmt.Errorf("cập nhật hội thoại: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return chat.ErrNotFound
	}
	return nil
}

func (r *ChatConversationRepository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	const q = `UPDATE conversations SET deleted_at = NOW() WHERE id = $1 AND deleted_at IS NULL`

	tag, err := r.db.Exec(ctx, q, id)
	if err != nil {
		return fmt.Errorf("xoá hội thoại: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return chat.ErrNotFound
	}
	return nil
}

func (r *ChatConversationRepository) ByID(
	ctx context.Context,
	id, viewerID uuid.UUID,
) (*chat.Conversation, error) {
	q := selectConversation + " WHERE c.id = $2 AND c.deleted_at IS NULL"
	return scanConversation(r.db.QueryRow(ctx, q, viewerID, id))
}

// ByDirectKey tìm hội thoại 1-1 đã có, KHÔNG theo góc nhìn người xem.
//
// Dùng lúc tạo, khi chưa biết có hội thoại nào để mà nhìn. Vì vậy nó là một
// câu truy vấn riêng chứ không dùng selectConversation.
func (r *ChatConversationRepository) ByDirectKey(
	ctx context.Context,
	companyID uuid.UUID,
	key string,
) (*chat.Conversation, error) {
	const q = `
		SELECT id, company_id, kind, COALESCE(name,''), department_id, project_id,
		       COALESCE(direct_key,''), created_by, last_message_at,
		       deleted_at, created_at, updated_at
		FROM conversations
		WHERE company_id = $1 AND direct_key = $2 AND deleted_at IS NULL`

	return scanBareConversation(r.db.QueryRow(ctx, q, companyID, key))
}

func (r *ChatConversationRepository) BySource(
	ctx context.Context,
	field string,
	sourceID uuid.UUID,
) (*chat.Conversation, error) {
	// Tên cột do TẦNG GỌI truyền vào nên phải kiểm tra bằng danh sách trắng.
	// Ghép thẳng vào SQL một chuỗi đến từ bên ngoài là lỗ hổng, kể cả khi hôm
	// nay mọi chỗ gọi đều truyền hằng số.
	if field != "department_id" && field != "project_id" {
		return nil, fmt.Errorf("cột nguồn không hợp lệ: %s", field)
	}

	q := `
		SELECT id, company_id, kind, COALESCE(name,''), department_id, project_id,
		       COALESCE(direct_key,''), created_by, last_message_at,
		       deleted_at, created_at, updated_at
		FROM conversations
		WHERE ` + field + ` = $1 AND deleted_at IS NULL`

	return scanBareConversation(r.db.QueryRow(ctx, q, sourceID))
}

func scanBareConversation(row pgx.Row) (*chat.Conversation, error) {
	var c chat.Conversation
	err := row.Scan(&c.ID, &c.CompanyID, &c.Kind, &c.Name,
		&c.DepartmentID, &c.ProjectID, &c.DirectKey, &c.CreatedBy,
		&c.LastMessageAt, &c.DeletedAt, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, chat.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("đọc hội thoại: %w", err)
	}
	c.DisplayName = c.Name
	return &c, nil
}

// ListFor liệt kê hội thoại của một người: ghim lên trước, rồi mới nhất trước.
//
// NULLS LAST ở last_message_at để nhóm vừa tạo chưa có tin nào không nhảy lên
// đầu danh sách — thứ tự đó làm người dùng tưởng có tin mới.
func (r *ChatConversationRepository) ListFor(
	ctx context.Context,
	viewerID uuid.UUID,
	search string,
) ([]*chat.Conversation, error) {
	var (
		where = []string{"c.deleted_at IS NULL"}
		args  = []any{viewerID}
	)

	if s := strings.TrimSpace(search); s != "" {
		args = append(args, "%"+s+"%")
		where = append(where, fmt.Sprintf(
			"(c.name ILIKE $%d OR peer.full_name ILIKE $%d)", len(args), len(args)))
	}

	q := selectConversation +
		" WHERE " + strings.Join(where, " AND ") +
		" ORDER BY cm.is_pinned DESC, c.last_message_at DESC NULLS LAST, c.created_at DESC" +
		" LIMIT 200"

	rows, err := r.db.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("liệt kê hội thoại: %w", err)
	}
	defer rows.Close()

	var out []*chat.Conversation
	for rows.Next() {
		c, err := scanConversation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// =========================================================================
// THÀNH VIÊN
// =========================================================================

func (r *ChatConversationRepository) Members(
	ctx context.Context,
	conversationID uuid.UUID,
) ([]*chat.Member, error) {
	const q = `
		SELECT cm.conversation_id, cm.employee_id, cm.is_admin,
		       cm.last_read_message_id, cm.last_read_at,
		       cm.is_pinned, cm.is_muted, cm.joined_at, cm.left_at,
		       COALESCE(e.full_name,''), COALESCE(e.employee_code,'')
		FROM conversation_members cm
		JOIN employees e ON e.id = cm.employee_id
		WHERE cm.conversation_id = $1 AND cm.left_at IS NULL
		ORDER BY cm.is_admin DESC, e.full_name`

	rows, err := r.db.Query(ctx, q, conversationID)
	if err != nil {
		return nil, fmt.Errorf("liệt kê thành viên hội thoại: %w", err)
	}
	defer rows.Close()

	var out []*chat.Member
	for rows.Next() {
		m, err := scanChatMember(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func scanChatMember(row pgx.Row) (*chat.Member, error) {
	var m chat.Member
	err := row.Scan(&m.ConversationID, &m.EmployeeID, &m.IsAdmin,
		&m.LastReadMessageID, &m.LastReadAt,
		&m.IsPinned, &m.IsMuted, &m.JoinedAt, &m.LeftAt,
		&m.EmployeeName, &m.EmployeeCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, chat.ErrNotMember
	}
	if err != nil {
		return nil, fmt.Errorf("đọc thành viên hội thoại: %w", err)
	}
	return &m, nil
}

func (r *ChatConversationRepository) MemberIDs(
	ctx context.Context,
	conversationID uuid.UUID,
) ([]uuid.UUID, error) {
	const q = `
		SELECT employee_id FROM conversation_members
		WHERE conversation_id = $1 AND left_at IS NULL`

	rows, err := r.db.Query(ctx, q, conversationID)
	if err != nil {
		return nil, fmt.Errorf("đọc id thành viên: %w", err)
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

func (r *ChatConversationRepository) Member(
	ctx context.Context,
	conversationID, employeeID uuid.UUID,
) (*chat.Member, error) {
	const q = `
		SELECT cm.conversation_id, cm.employee_id, cm.is_admin,
		       cm.last_read_message_id, cm.last_read_at,
		       cm.is_pinned, cm.is_muted, cm.joined_at, cm.left_at,
		       COALESCE(e.full_name,''), COALESCE(e.employee_code,'')
		FROM conversation_members cm
		JOIN employees e ON e.id = cm.employee_id
		WHERE cm.conversation_id = $1 AND cm.employee_id = $2 AND cm.left_at IS NULL`

	return scanChatMember(r.db.QueryRow(ctx, q, conversationID, employeeID))
}

// AddMembers thêm thành viên. Người từng rời nhóm thì được NHẬN LẠI.
//
// ON CONFLICT đặt left_at về NULL thay vì bỏ qua: mời lại một người đã rời là
// việc bình thường, và nếu bỏ qua thì lời mời im lặng không có tác dụng gì.
func (r *ChatConversationRepository) AddMembers(
	ctx context.Context,
	conversationID uuid.UUID,
	employeeIDs []uuid.UUID,
	admin bool,
) error {
	if len(employeeIDs) == 0 {
		return nil
	}

	const q = `
		INSERT INTO conversation_members (conversation_id, employee_id, is_admin)
		SELECT $1, x, $3 FROM UNNEST($2::uuid[]) AS x
		ON CONFLICT (conversation_id, employee_id)
		DO UPDATE SET left_at = NULL, joined_at = NOW(),
		              is_admin = conversation_members.is_admin OR EXCLUDED.is_admin`

	if _, err := r.db.Exec(ctx, q, conversationID, employeeIDs, admin); err != nil {
		return fmt.Errorf("thêm thành viên hội thoại: %w", err)
	}
	return nil
}

// RemoveMember đánh dấu rời nhóm chứ không xoá dòng.
//
// Giữ lại để tin nhắn cũ của người đó vẫn hiện tên, và để mốc đã đọc còn
// nguyên nếu họ được mời lại.
func (r *ChatConversationRepository) RemoveMember(
	ctx context.Context,
	conversationID, employeeID uuid.UUID,
) error {
	const q = `
		UPDATE conversation_members SET left_at = NOW()
		WHERE conversation_id = $1 AND employee_id = $2 AND left_at IS NULL`

	tag, err := r.db.Exec(ctx, q, conversationID, employeeID)
	if err != nil {
		return fmt.Errorf("gỡ thành viên hội thoại: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return chat.ErrNotMember
	}
	return nil
}

func (r *ChatConversationRepository) SetAdmin(
	ctx context.Context,
	conversationID, employeeID uuid.UUID,
	admin bool,
) error {
	const q = `
		UPDATE conversation_members SET is_admin = $3
		WHERE conversation_id = $1 AND employee_id = $2 AND left_at IS NULL`

	tag, err := r.db.Exec(ctx, q, conversationID, employeeID, admin)
	if err != nil {
		return fmt.Errorf("đổi quyền quản trị nhóm: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return chat.ErrNotMember
	}
	return nil
}

// SyncMembers đưa thành viên nhóm tự động về đúng nguồn gốc, trong MỘT giao
// dịch.
//
// Thêm và gỡ phải cùng thành công hoặc cùng thất bại: nửa chừng sẽ để lại một
// nhóm phòng ban vừa thiếu người mới vừa còn người đã chuyển đi, và lần chạy
// sau không có cách nào biết là nó đang dở.
func (r *ChatConversationRepository) SyncMembers(
	ctx context.Context,
	conversationID uuid.UUID,
	want []uuid.UUID,
) (int, int, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("mở giao dịch đồng bộ thành viên: %w", err)
	}
	defer tx.Rollback(ctx)

	const addQ = `
		INSERT INTO conversation_members (conversation_id, employee_id)
		SELECT $1, x FROM UNNEST($2::uuid[]) AS x
		ON CONFLICT (conversation_id, employee_id)
		DO UPDATE SET left_at = NULL
		WHERE conversation_members.left_at IS NOT NULL`

	addTag, err := tx.Exec(ctx, addQ, conversationID, want)
	if err != nil {
		return 0, 0, fmt.Errorf("thêm thành viên đồng bộ: %w", err)
	}

	const delQ = `
		UPDATE conversation_members SET left_at = NOW()
		WHERE conversation_id = $1 AND left_at IS NULL AND NOT (employee_id = ANY($2))`

	delTag, err := tx.Exec(ctx, delQ, conversationID, want)
	if err != nil {
		return 0, 0, fmt.Errorf("gỡ thành viên đồng bộ: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, 0, fmt.Errorf("ghi đồng bộ thành viên: %w", err)
	}
	return int(addTag.RowsAffected()), int(delTag.RowsAffected()), nil
}

// =========================================================================
// TUỲ CHỌN CỦA TỪNG NGƯỜI
// =========================================================================

func (r *ChatConversationRepository) SetPinned(
	ctx context.Context,
	conversationID, employeeID uuid.UUID,
	pinned bool,
) error {
	return r.setMemberFlag(ctx, "is_pinned", conversationID, employeeID, pinned)
}

func (r *ChatConversationRepository) SetMuted(
	ctx context.Context,
	conversationID, employeeID uuid.UUID,
	muted bool,
) error {
	return r.setMemberFlag(ctx, "is_muted", conversationID, employeeID, muted)
}

// setMemberFlag gom hai câu UPDATE chỉ khác tên cột. Tên cột là hằng số trong
// chính tệp này, không đến từ người dùng.
func (r *ChatConversationRepository) setMemberFlag(
	ctx context.Context,
	column string,
	conversationID, employeeID uuid.UUID,
	value bool,
) error {
	q := `UPDATE conversation_members SET ` + column + ` = $3
	      WHERE conversation_id = $1 AND employee_id = $2 AND left_at IS NULL`

	tag, err := r.db.Exec(ctx, q, conversationID, employeeID, value)
	if err != nil {
		return fmt.Errorf("lưu tuỳ chọn hội thoại: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return chat.ErrNotMember
	}
	return nil
}

// MarkRead dời mốc đã đọc, và chỉ dời VỀ PHÍA TRƯỚC.
//
// Điều kiện so sánh thời điểm ở cuối câu chặn việc lùi mốc: hai tab cùng mở
// một hội thoại sẽ báo đọc theo thứ tự bất kỳ, và không có điều kiện này thì
// tab cuộn ngược lên sẽ làm tin mới "chưa đọc" trở lại.
func (r *ChatConversationRepository) MarkRead(
	ctx context.Context,
	conversationID, employeeID, messageID uuid.UUID,
) error {
	const q = `
		UPDATE conversation_members cm
		SET last_read_message_id = m.id, last_read_at = m.created_at
		FROM messages m
		WHERE cm.conversation_id = $1 AND cm.employee_id = $2 AND cm.left_at IS NULL
		  AND m.id = $3 AND m.conversation_id = $1
		  AND (cm.last_read_at IS NULL OR m.created_at > cm.last_read_at)`

	if _, err := r.db.Exec(ctx, q, conversationID, employeeID, messageID); err != nil {
		return fmt.Errorf("đánh dấu đã đọc: %w", err)
	}
	// Không báo lỗi khi không có dòng nào đổi: mốc đã ở phía trước rồi, và đó
	// là kết quả người gọi mong muốn chứ không phải sự cố.
	return nil
}

func (r *ChatConversationRepository) TotalUnread(
	ctx context.Context,
	employeeID uuid.UUID,
) (int, error) {
	const q = `
		SELECT COALESCE(SUM(
		  (SELECT COUNT(*) FROM messages m
		   WHERE m.conversation_id = cm.conversation_id
		     AND m.deleted_at IS NULL
		     AND m.sender_id IS DISTINCT FROM cm.employee_id
		     AND (cm.last_read_at IS NULL OR m.created_at > cm.last_read_at))
		), 0)
		FROM conversation_members cm
		JOIN conversations c ON c.id = cm.conversation_id AND c.deleted_at IS NULL
		WHERE cm.employee_id = $1 AND cm.left_at IS NULL`

	var n int
	if err := r.db.QueryRow(ctx, q, employeeID).Scan(&n); err != nil {
		return 0, fmt.Errorf("đếm tin chưa đọc: %w", err)
	}
	return n, nil
}
