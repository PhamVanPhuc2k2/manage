package postgres

import (
	"context"

	"github.com/google/uuid"

	domaincall "github.com/PhamVanPhuc2k2/manage/internal/domain/call"
	"github.com/PhamVanPhuc2k2/manage/pkg/postgres"
)

// CallConversationLookup trả lời hai câu hỏi module gọi cần về hội thoại.
//
// Hỏi thẳng database thay vì đi qua usecase/chat, dù chat đã có sẵn cả hai
// thông tin này. Lý do: mỗi lần trao đổi SDP hay ICE đều gọi IsMember, và
// đường qua usecase kéo theo việc dựng cả đối tượng Member kèm tên, mã nhân
// viên và trạng thái online — ba thứ không ai dùng ở đây. Một câu EXISTS
// nhanh hơn hẳn trên một đường chạy rất nóng.
type CallConversationLookup struct {
	db *postgres.DB
}

func NewCallConversationLookup(db *postgres.DB) *CallConversationLookup {
	return &CallConversationLookup{db: db}
}

// IsMember là hàng rào phân quyền duy nhất của module gọi.
//
// left_at IS NULL là phần quan trọng: người đã rời nhóm không được gọi vào
// nhóm cũ, và cũng không được nghe lén cuộc gọi đang diễn ra ở đó.
func (l *CallConversationLookup) IsMember(
	ctx context.Context,
	conversationID, employeeID uuid.UUID,
) (bool, error) {
	const q = `
		SELECT EXISTS (
			SELECT 1 FROM conversation_members
			WHERE conversation_id = $1
			  AND employee_id = $2
			  AND left_at IS NULL
		)`

	var ok bool
	if err := l.db.QueryRow(ctx, q, conversationID, employeeID).Scan(&ok); err != nil {
		return false, err
	}
	return ok, nil
}

func (l *CallConversationLookup) MemberIDs(
	ctx context.Context,
	conversationID uuid.UUID,
) ([]uuid.UUID, error) {
	const q = `
		SELECT employee_id
		FROM conversation_members
		WHERE conversation_id = $1 AND left_at IS NULL`

	rows, err := l.db.Query(ctx, q, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]uuid.UUID, 0, 8)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

var _ domaincall.ConversationLookup = (*CallConversationLookup)(nil)
