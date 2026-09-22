package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	domaincall "github.com/PhamVanPhuc2k2/manage/internal/domain/call"
	"github.com/PhamVanPhuc2k2/manage/pkg/postgres"
)

type CallRepository struct {
	db *postgres.DB
}

func NewCallRepository(db *postgres.DB) *CallRepository {
	return &CallRepository{db: db}
}

const selectCall = `
SELECT c.id, c.conversation_id, c.initiator_id, c.kind, c.status,
       c.room_name, c.started_at, c.ended_at, COALESCE(c.end_reason,''),
       c.relay_ratio, c.created_at,
       COALESCE(e.full_name,'')
FROM calls c
LEFT JOIN employees e ON e.id = c.initiator_id
`

func scanCall(row pgx.Row) (*domaincall.Call, error) {
	var c domaincall.Call
	err := row.Scan(
		&c.ID, &c.ConversationID, &c.InitiatorID, &c.Kind, &c.Status,
		&c.RoomName, &c.StartedAt, &c.EndedAt, &c.EndReason,
		&c.RelayRatio, &c.CreatedAt,
		&c.InitiatorName,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domaincall.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("đọc cuộc gọi: %w", err)
	}
	return &c, nil
}

func (r *CallRepository) Create(ctx context.Context, c *domaincall.Call) error {
	const q = `
		INSERT INTO calls (id, conversation_id, initiator_id, kind, status,
		                   room_name, started_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at`

	err := r.db.QueryRow(ctx, q,
		c.ID, c.ConversationID, c.InitiatorID, c.Kind, c.Status,
		c.RoomName, c.StartedAt,
	).Scan(&c.ID, &c.CreatedAt)

	if err != nil {
		// Đụng chỉ mục một phần nghĩa là hội thoại đã có cuộc gọi đang chạy.
		//
		// Dịch thành lỗi nghiệp vụ thay vì để lỗi database nổi lên: tầng
		// trên đã hỏi LiveInConversation trước khi tạo, nên tới được đây
		// nghĩa là có người khác chen vào giữa hai bước. Đó là cuộc đua
		// thật, và câu trả lời đúng là "đã có cuộc gọi rồi" chứ không phải
		// một lỗi 500.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return domaincall.ErrNotFound
		}
		return fmt.Errorf("tạo cuộc gọi: %w", err)
	}
	return nil
}

func (r *CallRepository) GetByID(
	ctx context.Context, id uuid.UUID,
) (*domaincall.Call, error) {
	return scanCall(r.db.QueryRow(ctx, selectCall+` WHERE c.id = $1`, id))
}

func (r *CallRepository) LiveInConversation(
	ctx context.Context, conversationID uuid.UUID,
) (*domaincall.Call, error) {
	const where = ` WHERE c.conversation_id = $1 AND c.status IN ('ringing','active')`
	return scanCall(r.db.QueryRow(ctx, selectCall+where, conversationID))
}

// LiveForEmployee tìm cuộc gọi mà người này đang THẬT SỰ tham gia.
//
// Điều kiện là đã vào phòng và chưa rời (joined_at NOT NULL, left_at NULL),
// KHÔNG phải chỉ được mời. Người đang nghe chuông vẫn rảnh: họ chưa nói
// chuyện với ai, và từ chối lời mời thứ hai của họ sẽ là sai.
func (r *CallRepository) LiveForEmployee(
	ctx context.Context, employeeID uuid.UUID,
) (*domaincall.Call, error) {
	const where = `
		JOIN call_participants p ON p.call_id = c.id
		WHERE p.employee_id = $1
		  AND p.joined_at IS NOT NULL AND p.left_at IS NULL
		  AND c.status IN ('ringing','active')
		LIMIT 1`
	return scanCall(r.db.QueryRow(ctx, selectCall+where, employeeID))
}

// UpdateStatus đổi trạng thái CÓ ĐIỀU KIỆN.
//
// Mệnh đề `AND status = $2` là phần quan trọng nhất: hai người cùng bấm cúp
// máy thì chỉ một lời gọi khớp, lời còn lại nhận ErrNotFound và biết mình
// đã thua. Không có nó thì lời gọi thứ hai ghi đè lý do kết thúc của lời
// gọi thứ nhất, và lịch sử cuộc gọi nói sai.
func (r *CallRepository) UpdateStatus(
	ctx context.Context,
	id uuid.UUID,
	from, to domaincall.Status,
	reason string,
) error {
	// Ép kiểu $3 tường minh thành call_status ở CẢ HAI chỗ dùng.
	//
	// Không ép thì PostgreSQL báo "inconsistent types deduced for parameter
	// $3": một chỗ nó suần ra enum (vế phải của `status =`), chỗ kia ra text
	// (so với chuỗi trong IN), và một tham số không thể mang hai kiểu.
	const q = `
		UPDATE calls
		SET status     = $3::call_status,
		    end_reason = NULLIF($4, ''),
		    -- Chỉ đặt ended_at khi chuyển sang trạng thái kết thúc, và chỉ
		    -- đặt MỘT LẦN: ghi đè sẽ kéo dài thời lượng cuộc gọi mỗi lần
		    -- có ai đó gọi lại hàm này.
		    ended_at   = CASE
		                   WHEN $3::call_status IN ('ringing','active') THEN NULL
		                   ELSE COALESCE(ended_at, NOW())
		                 END
		WHERE id = $1 AND status = $2::call_status`

	tag, err := r.db.Exec(ctx, q, id, from, to, reason)
	if err != nil {
		return fmt.Errorf("đổi trạng thái cuộc gọi: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domaincall.ErrNotFound
	}
	return nil
}

func (r *CallRepository) SetRelayRatio(
	ctx context.Context, id uuid.UUID, ratio float64,
) error {
	const q = `UPDATE calls SET relay_ratio = $2 WHERE id = $1`
	if _, err := r.db.Exec(ctx, q, id, ratio); err != nil {
		return fmt.Errorf("ghi tỉ lệ relay: %w", err)
	}
	return nil
}

func (r *CallRepository) ListForConversation(
	ctx context.Context, conversationID uuid.UUID, limit int,
) ([]*domaincall.Call, error) {
	const where = `
		WHERE c.conversation_id = $1
		ORDER BY c.started_at DESC
		LIMIT $2`

	rows, err := r.db.Query(ctx, selectCall+where, conversationID, limit)
	if err != nil {
		return nil, fmt.Errorf("liệt kê cuộc gọi: %w", err)
	}
	defer rows.Close()

	out := make([]*domaincall.Call, 0, limit)
	for rows.Next() {
		c, err := scanCall(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ExpireRinging đánh dấu nhỡ cho mọi cuộc gọi đổ chuông quá hạn.
//
// Dùng UPDATE ... RETURNING để đổi và lấy về trong MỘT lượt. Đọc trước rồi
// cập nhật sau sẽ mở ra khoảng giữa hai câu lệnh, và hai worker chạy song
// song sẽ cùng xử lý một cuộc gọi — người dùng nhận hai lần thông báo gọi
// nhỡ cho cùng một cuộc.
func (r *CallRepository) ExpireRinging(
	ctx context.Context, olderThan time.Time,
) ([]*domaincall.Call, error) {
	const q = `
		UPDATE calls
		SET status = 'missed', end_reason = 'timeout', ended_at = NOW()
		WHERE status = 'ringing' AND started_at < $1
		RETURNING id, conversation_id, initiator_id, kind, status,
		          room_name, started_at, ended_at, COALESCE(end_reason,''),
		          relay_ratio, created_at`

	rows, err := r.db.Query(ctx, q, olderThan)
	if err != nil {
		return nil, fmt.Errorf("dọn cuộc gọi quá hạn: %w", err)
	}
	defer rows.Close()

	var out []*domaincall.Call
	for rows.Next() {
		var c domaincall.Call
		if err := rows.Scan(
			&c.ID, &c.ConversationID, &c.InitiatorID, &c.Kind, &c.Status,
			&c.RoomName, &c.StartedAt, &c.EndedAt, &c.EndReason,
			&c.RelayRatio, &c.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("đọc cuộc gọi quá hạn: %w", err)
		}
		out = append(out, &c)
	}
	return out, rows.Err()
}

// =========================================================================
// NGƯỜI THAM GIA
// =========================================================================

type CallParticipantRepository struct {
	db *postgres.DB
}

func NewCallParticipantRepository(db *postgres.DB) *CallParticipantRepository {
	return &CallParticipantRepository{db: db}
}

// Invite ghi nhiều người trong MỘT câu lệnh.
//
// unnest thay vì vòng lặp: mời mười người bằng mười lần đi lại tới database
// là mười lần chờ mạng, ngay trong lúc người dùng đang nhìn màn hình chờ
// chuông đổ.
func (r *CallParticipantRepository) Invite(
	ctx context.Context, callID uuid.UUID, employeeIDs []uuid.UUID,
) error {
	if len(employeeIDs) == 0 {
		return nil
	}

	const q = `
		INSERT INTO call_participants (call_id, employee_id)
		SELECT $1, x FROM unnest($2::uuid[]) AS x
		ON CONFLICT (call_id, employee_id) DO NOTHING`

	if _, err := r.db.Exec(ctx, q, callID, employeeIDs); err != nil {
		return fmt.Errorf("mời vào cuộc gọi: %w", err)
	}
	return nil
}

// Join đánh dấu một người đã vào phòng.
//
// Chạy lại được. Vào lại sau khi rớt mạng chỉ xoá left_at và GIỮ NGUYÊN
// joined_at cũ: thời lượng tham gia tính từ lần vào đầu tiên, còn tính lại
// từ lần vào sau sẽ làm báo cáo nói rằng người đó chỉ họp có hai phút.
func (r *CallParticipantRepository) Join(
	ctx context.Context, callID, employeeID uuid.UUID, at time.Time,
) error {
	const q = `
		INSERT INTO call_participants (call_id, employee_id, joined_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (call_id, employee_id) DO UPDATE
		SET joined_at = COALESCE(call_participants.joined_at, EXCLUDED.joined_at),
		    left_at   = NULL`

	if _, err := r.db.Exec(ctx, q, callID, employeeID, at); err != nil {
		return fmt.Errorf("vào phòng: %w", err)
	}
	return nil
}

func (r *CallParticipantRepository) Leave(
	ctx context.Context, callID, employeeID uuid.UUID, at time.Time,
) error {
	// COALESCE để left_at không bị đẩy lùi mỗi lần gọi lại: người đã rời
	// rồi thì giữ mốc cũ.
	const q = `
		UPDATE call_participants
		SET left_at = COALESCE(left_at, $3)
		WHERE call_id = $1 AND employee_id = $2`

	if _, err := r.db.Exec(ctx, q, callID, employeeID, at); err != nil {
		return fmt.Errorf("rời phòng: %w", err)
	}
	return nil
}

// SetTracks chỉ BẬT cờ, không bao giờ tắt.
//
// Đây là dấu vết phục vụ audit: câu hỏi cần trả lời là "trong cuộc họp đó
// người này có chiếu màn hình không", chứ không phải "lúc 10h03 có đang
// chiếu không". Dùng OR nên gọi bao nhiêu lần cũng cho cùng kết quả.
func (r *CallParticipantRepository) SetTracks(
	ctx context.Context, callID, employeeID uuid.UUID, audio, video, screen bool,
) error {
	const q = `
		UPDATE call_participants
		SET had_audio  = had_audio  OR $3,
		    had_video  = had_video  OR $4,
		    had_screen = had_screen OR $5
		WHERE call_id = $1 AND employee_id = $2`

	if _, err := r.db.Exec(ctx, q, callID, employeeID, audio, video, screen); err != nil {
		return fmt.Errorf("ghi nhận luồng media: %w", err)
	}
	return nil
}

func (r *CallParticipantRepository) SetRelay(
	ctx context.Context, callID, employeeID uuid.UUID, relay bool,
) error {
	const q = `
		UPDATE call_participants SET used_relay = $3
		WHERE call_id = $1 AND employee_id = $2`

	if _, err := r.db.Exec(ctx, q, callID, employeeID, relay); err != nil {
		return fmt.Errorf("ghi nhận relay: %w", err)
	}
	return nil
}

func (r *CallParticipantRepository) ListForCall(
	ctx context.Context, callID uuid.UUID,
) ([]*domaincall.Participant, error) {
	const q = `
		SELECT p.id, p.call_id, p.employee_id,
		       p.joined_at, p.left_at,
		       p.had_audio, p.had_video, p.had_screen,
		       p.used_relay, p.created_at,
		       COALESCE(e.full_name,''), COALESCE(e.avatar_key,'')
		FROM call_participants p
		JOIN employees e ON e.id = p.employee_id
		WHERE p.call_id = $1
		ORDER BY p.joined_at NULLS LAST, p.created_at`

	rows, err := r.db.Query(ctx, q, callID)
	if err != nil {
		return nil, fmt.Errorf("liệt kê người tham gia: %w", err)
	}
	defer rows.Close()

	var out []*domaincall.Participant
	for rows.Next() {
		var p domaincall.Participant
		if err := rows.Scan(
			&p.ID, &p.CallID, &p.EmployeeID,
			&p.JoinedAt, &p.LeftAt,
			&p.HadAudio, &p.HadVideo, &p.HadScreen,
			&p.UsedRelay, &p.CreatedAt,
			&p.EmployeeName, &p.AvatarKey,
		); err != nil {
			return nil, fmt.Errorf("đọc người tham gia: %w", err)
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

// RoomState đếm người đang trong phòng và người còn đang đổ chuông.
//
// Một câu truy vấn cho cả hai con số. Hỏi hai lần sẽ mở ra khoảng giữa, và
// một người vào phòng đúng lúc đó khiến phép tính "cuộc gọi còn sống không"
// cho kết quả sai — cuộc gọi bị kết thúc ngay khi có người vừa bắt máy.
func (r *CallParticipantRepository) RoomState(
	ctx context.Context, callID uuid.UUID,
) (inRoom, pending int, err error) {
	const q = `
		SELECT
		  COUNT(*) FILTER (WHERE joined_at IS NOT NULL AND left_at IS NULL),
		  COUNT(*) FILTER (WHERE joined_at IS NULL     AND left_at IS NULL)
		FROM call_participants
		WHERE call_id = $1`

	if err := r.db.QueryRow(ctx, q, callID).Scan(&inRoom, &pending); err != nil {
		return 0, 0, fmt.Errorf("đếm người trong phòng: %w", err)
	}
	return inRoom, pending, nil
}

// Ràng buộc kiểu: repository phải khớp cổng ở tầng domain.
//
// Không có hai dòng này thì một chữ ký lệch chỉ lộ ra ở composition root,
// tức là cách xa chỗ sai vài tệp và với một thông báo lỗi dài.
var (
	_ domaincall.Repository            = (*CallRepository)(nil)
	_ domaincall.ParticipantRepository = (*CallParticipantRepository)(nil)
)
