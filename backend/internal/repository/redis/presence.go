package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"

	domainrealtime "github.com/PhamVanPhuc2k2/manage/internal/domain/realtime"
)

// PresenceStore lưu trạng thái hiện diện trên Redis.
//
// Cấu trúc dữ liệu: mỗi nhân viên là MỘT hash `presence:{employee_id}`,
// trong đó mỗi field là một conn_id và giá trị là Connection dạng JSON.
//
// Vì sao hash chứ không phải một khoá cho mỗi kết nối: câu hỏi hay gặp nhất
// là "người này có online không", và với hash nó là đúng MỘT lệnh HGETALL.
// Một khoá cho mỗi kết nối sẽ phải SCAN theo mẫu — thao tác O(n) trên toàn
// bộ keyspace mà Redis khuyến cáo không dùng trong đường xử lý chính.
//
// Cái giá: TTL đặt được trên cả hash chứ không trên từng field, nên một kết
// nối chết vẫn nằm trong hash tới khi CẢ hash hết hạn. Bù lại bằng cách lọc
// theo LastSeenAt lúc đọc — xem prune().
type PresenceStore struct {
	rdb *goredis.Client
}

func NewPresenceStore(rdb *goredis.Client) *PresenceStore {
	return &PresenceStore{rdb: rdb}
}

const presenceIndexKey = "presence:index"

func presenceKey(employeeID uuid.UUID) string {
	return "presence:" + employeeID.String()
}

func (s *PresenceStore) Touch(
	ctx context.Context,
	employeeID uuid.UUID,
	conn *domainrealtime.Connection,
) error {
	data, err := json.Marshal(conn)
	if err != nil {
		return fmt.Errorf("mã hoá presence: %w", err)
	}

	key := presenceKey(employeeID)

	// Gộp vào một pipeline: bốn lệnh đi một vòng mạng thay vì bốn. Heartbeat
	// chạy 30 giây một lần cho MỌI người đang online, nên chi phí này nhân
	// lên theo số nhân viên.
	pipe := s.rdb.TxPipeline()
	pipe.HSet(ctx, key, conn.ConnID.String(), data)
	pipe.Expire(ctx, key, domainrealtime.PresenceTTL)
	// Chỉ mục để liệt kê người đang online mà không phải SCAN keyspace.
	pipe.SAdd(ctx, presenceIndexKey, employeeID.String())
	// Chỉ mục sống lâu hơn presence: nó chỉ là danh sách ứng viên, việc
	// người đó còn online hay không do chính hash trả lời.
	pipe.Expire(ctx, presenceIndexKey, 24*time.Hour)

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("ghi presence: %w", err)
	}
	return nil
}

func (s *PresenceStore) Remove(ctx context.Context, employeeID, connID uuid.UUID) error {
	key := presenceKey(employeeID)

	pipe := s.rdb.TxPipeline()
	pipe.HDel(ctx, key, connID.String())
	remaining := pipe.HLen(ctx, key)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("gỡ presence: %w", err)
	}

	// Hết kết nối thì dọn luôn khỏi chỉ mục, để ListOnline không phải duyệt
	// qua những người đã offline từ lâu.
	if remaining.Val() == 0 {
		if err := s.rdb.SRem(ctx, presenceIndexKey, employeeID.String()).Err(); err != nil {
			return fmt.Errorf("gỡ khỏi chỉ mục presence: %w", err)
		}
	}
	return nil
}

func (s *PresenceStore) Get(
	ctx context.Context,
	employeeID uuid.UUID,
) (*domainrealtime.Presence, error) {
	raw, err := s.rdb.HGetAll(ctx, presenceKey(employeeID)).Result()
	if err != nil {
		return nil, fmt.Errorf("đọc presence: %w", err)
	}
	return &domainrealtime.Presence{
		EmployeeID:  employeeID,
		Connections: prune(raw),
	}, nil
}

func (s *PresenceStore) GetMany(
	ctx context.Context,
	employeeIDs []uuid.UUID,
) (map[uuid.UUID]*domainrealtime.Presence, error) {
	out := make(map[uuid.UUID]*domainrealtime.Presence, len(employeeIDs))
	if len(employeeIDs) == 0 {
		return out, nil
	}

	// Một pipeline cho tất cả: tra 50 người trong danh sách nhân viên tốn
	// một vòng mạng thay vì 50.
	pipe := s.rdb.Pipeline()
	cmds := make([]*goredis.MapStringStringCmd, len(employeeIDs))
	for i, id := range employeeIDs {
		cmds[i] = pipe.HGetAll(ctx, presenceKey(id))
	}
	// redis.Nil ở đây là bình thường (người không online), không phải lỗi.
	if _, err := pipe.Exec(ctx); err != nil && err != goredis.Nil {
		return nil, fmt.Errorf("đọc presence hàng loạt: %w", err)
	}

	for i, id := range employeeIDs {
		out[id] = &domainrealtime.Presence{
			EmployeeID:  id,
			Connections: prune(cmds[i].Val()),
		}
	}
	return out, nil
}

func (s *PresenceStore) ListOnline(ctx context.Context) ([]*domainrealtime.Presence, error) {
	ids, err := s.rdb.SMembers(ctx, presenceIndexKey).Result()
	if err != nil {
		return nil, fmt.Errorf("đọc chỉ mục presence: %w", err)
	}
	if len(ids) == 0 {
		return []*domainrealtime.Presence{}, nil
	}

	parsed := make([]uuid.UUID, 0, len(ids))
	for _, s := range ids {
		if id, err := uuid.Parse(s); err == nil {
			parsed = append(parsed, id)
		}
	}

	byID, err := s.GetMany(ctx, parsed)
	if err != nil {
		return nil, err
	}

	out := make([]*domainrealtime.Presence, 0, len(byID))
	for _, p := range byID {
		// Chỉ mục có thể còn tên người đã offline (hash hết hạn trước khi
		// kịp gọi Remove, ví dụ instance bị kill -9). Lọc ở đây.
		if p.Online() {
			out = append(out, p)
		}
	}
	return out, nil
}

// prune giải mã các kết nối và loại bỏ những cái đã quá hạn.
//
// Cần thiết vì TTL của Redis đặt trên cả hash, không trên từng field: một
// instance bị kill đột ngột sẽ để lại kết nối mồ côi trong hash của người
// vẫn đang online ở thiết bị khác. Không lọc thì người đó mãi mãi hiện là
// đang online trên một máy đã tắt.
func prune(raw map[string]string) []*domainrealtime.Connection {
	if len(raw) == 0 {
		return nil
	}

	cutoff := time.Now().Add(-domainrealtime.PresenceTTL)
	out := make([]*domainrealtime.Connection, 0, len(raw))

	for _, v := range raw {
		var c domainrealtime.Connection
		if err := json.Unmarshal([]byte(v), &c); err != nil {
			continue // bản ghi hỏng: bỏ qua, không làm hỏng cả phép đọc
		}
		if c.LastSeenAt.Before(cutoff) {
			continue
		}
		out = append(out, &c)
	}

	if len(out) == 0 {
		return nil
	}
	return out
}
