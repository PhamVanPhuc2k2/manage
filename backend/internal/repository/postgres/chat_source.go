package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	domainchat "github.com/PhamVanPhuc2k2/manage/internal/domain/chat"
	"github.com/PhamVanPhuc2k2/manage/pkg/postgres"
)

// ChatSourceRepository liệt kê phòng ban và dự án cần có nhóm chat tự động.
type ChatSourceRepository struct {
	db *postgres.DB
}

func NewChatSourceRepository(db *postgres.DB) *ChatSourceRepository {
	return &ChatSourceRepository{db: db}
}

// Sources gộp phòng ban và dự án vào MỘT truy vấn.
//
// Gộp bằng UNION ALL thay vì hai lượt gọi: job đồng bộ chỉ cần một danh sách
// phẳng, và hai lượt round-trip cho cùng một việc là lãng phí không có lý do.
// Dự án đã kết thúc hoặc bị huỷ không nằm trong danh sách — nhóm của chúng
// vẫn còn để đọc lại lịch sử, chỉ là thôi đồng bộ thành viên.
func (r *ChatSourceRepository) Sources(
	ctx context.Context,
	companyID uuid.UUID,
) ([]domainchat.GroupSource, error) {
	const q = `
		SELECT 'department' AS kind, id, name
		FROM departments
		WHERE company_id = $1 AND deleted_at IS NULL
		UNION ALL
		SELECT 'project' AS kind, id, name
		FROM projects
		WHERE company_id = $1 AND deleted_at IS NULL
		  AND status NOT IN ('completed', 'cancelled')
		ORDER BY kind, name`

	rows, err := r.db.Query(ctx, q, companyID)
	if err != nil {
		return nil, fmt.Errorf("liệt kê nguồn nhóm chat: %w", err)
	}
	defer rows.Close()

	var out []domainchat.GroupSource
	for rows.Next() {
		var s domainchat.GroupSource
		if err := rows.Scan(&s.Kind, &s.ID, &s.Name); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
