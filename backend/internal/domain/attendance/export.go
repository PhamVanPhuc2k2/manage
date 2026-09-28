package attendance

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// =========================================================================
// XUẤT BÁO CÁO CHẤM CÔNG RA EXCEL
// =========================================================================

type ExportStatus string

const (
	ExportQueued     ExportStatus = "queued"
	ExportProcessing ExportStatus = "processing"
	ExportDone       ExportStatus = "done"
	ExportFailed     ExportStatus = "failed"
)

func (s ExportStatus) Finished() bool {
	return s == ExportDone || s == ExportFailed
}

// ExportRetention là thời gian giữ tệp đã xuất. Quá hạn thì tệp bị xoá,
// bản ghi vẫn còn để biết ai đã xuất gì. Tệp chấm công chứa giờ giấc của
// từng người — không nên nằm mãi trong database khi không ai cần nữa.
const ExportRetention = 7 * 24 * time.Hour

// ExportActor là ảnh chụp quyền của người yêu cầu, lúc yêu cầu.
type ExportActor struct {
	UserID               uuid.UUID   `json:"user_id"`
	EmployeeID           uuid.UUID   `json:"employee_id"`
	Scope                string      `json:"scope"`
	ManagedDepartmentIDs []uuid.UUID `json:"managed_department_ids,omitempty"`
}

type Export struct {
	ID             uuid.UUID
	Year           int
	Month          int
	DepartmentID   *uuid.UUID
	DepartmentName string
	Status         ExportStatus
	RequestedBy    *uuid.UUID
	RequesterName  string
	Actor          ExportActor
	FileName       string
	HasFile        bool
	EmployeeCount  int
	Error          string
	CreatedAt      time.Time
	FinishedAt     *time.Time
}

type ExportRepository interface {
	Create(ctx context.Context, e *Export) error

	// GetByID KHÔNG kèm nội dung tệp — chỉ HasFile.
	GetByID(ctx context.Context, id uuid.UUID) (*Export, error)

	// GetFile trả về tên và nội dung tệp. ErrNotFound khi chưa có hoặc
	// đã bị dọn.
	GetFile(ctx context.Context, id uuid.UUID) (name string, data []byte, err error)

	// List trả về các lượt gần nhất; requestedBy khác nil thì chỉ của người đó.
	List(ctx context.Context, requestedBy *uuid.UUID, limit int) ([]*Export, error)

	MarkProcessing(ctx context.Context, id uuid.UUID) error
	Complete(ctx context.Context, id uuid.UUID, fileName string, data []byte, employeeCount int) error
	Fail(ctx context.Context, id uuid.UUID, msg string) error

	// PurgeFiles xoá nội dung tệp của các lượt tạo trước `before`.
	PurgeFiles(ctx context.Context, before time.Time) (int64, error)
}
