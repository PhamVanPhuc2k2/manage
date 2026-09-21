package notification

import (
	"time"

	"github.com/google/uuid"

	domainnotif "github.com/PhamVanPhuc2k2/manage/internal/domain/notification"
)

// View là hình dạng thông báo trả ra API và đẩy qua WebSocket.
//
// Dùng CHUNG một kiểu cho cả hai đường là có chủ ý: client dựng một thẻ thông
// báo duy nhất, và hai hình dạng khác nhau sẽ buộc nó có hai nhánh vẽ — nhánh
// realtime là nhánh ít được kiểm thử hơn, nên cũng là nhánh hay lệch.
type View struct {
	ID    uuid.UUID        `json:"id"`
	Type  domainnotif.Type `json:"type"`
	Title string           `json:"title"`
	Body  string           `json:"body,omitempty"`
	Link  string           `json:"link,omitempty"`

	ActorID   *uuid.UUID `json:"actor_id,omitempty"`
	ActorName string     `json:"actor_name,omitempty"`

	Resource   string     `json:"resource,omitempty"`
	ResourceID *uuid.UUID `json:"resource_id,omitempty"`

	Read      bool       `json:"read"`
	ReadAt    *time.Time `json:"read_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

func toView(n *domainnotif.Notification) *View {
	return &View{
		ID:         n.ID,
		Type:       n.Type,
		Title:      n.Title,
		Body:       n.Body,
		Link:       n.Link,
		ActorID:    n.ActorID,
		ActorName:  n.ActorName,
		Resource:   n.Resource,
		ResourceID: n.ResourceID,
		Read:       n.Read(),
		ReadAt:     n.ReadAt,
		CreatedAt:  n.CreatedAt,
	}
}
