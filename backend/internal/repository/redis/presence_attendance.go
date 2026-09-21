// Adapter nối PresenceStore với mô-đun chấm công.
//
// Nằm ở file RIÊNG chứ không chung với presence.go, có chủ ý: presence.go là
// hạ tầng WebSocket thuần tuý và không được biết gì về nghiệp vụ chấm công.
// Giữ chúng trong cùng một file khiến tầng realtime không biên dịch được nếu
// thiếu mô-đun chấm công — một phụ thuộc ngược không ai muốn.
package redis

import (
	"context"

	"github.com/google/uuid"

	domainatt "github.com/PhamVanPhuc2k2/manage/internal/domain/attendance"
)

// =========================================================================
// ADAPTER CHO MODULE CHẤM CÔNG
//
// Hai method dưới đây khiến PresenceStore đáp ứng luôn cổng PresenceReader
// của module chấm công. Một bản hiện thực phục vụ hai interface hẹp là cách
// làm đúng trong Go: mỗi bên khai báo đúng thứ mình cần, và không bên nào
// phải biết về bên kia.
// =========================================================================

func (s *PresenceStore) SnapshotOnline(ctx context.Context) (
	[]domainatt.PresenceSnapshot, error,
) {
	list, err := s.ListOnline(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]domainatt.PresenceSnapshot, 0, len(list))
	for _, p := range list {
		out = append(out, domainatt.PresenceSnapshot{
			EmployeeID: p.EmployeeID,
			IsActive:   p.Active(),
			LastSeenAt: p.LastSeen(),
		})
	}
	return out, nil
}

func (s *PresenceStore) StatusOf(ctx context.Context, employeeIDs []uuid.UUID) (
	map[uuid.UUID]string, error,
) {
	byID, err := s.GetMany(ctx, employeeIDs)
	if err != nil {
		return nil, err
	}

	out := make(map[uuid.UUID]string, len(byID))
	for id, p := range byID {
		out[id] = string(p.Status())
	}
	return out, nil
}
