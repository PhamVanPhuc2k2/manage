package project

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type Repository interface {
	Create(ctx context.Context, p *Project) error
	Update(ctx context.Context, p *Project) error
	SoftDelete(ctx context.Context, id uuid.UUID) error
	GetByID(ctx context.Context, id uuid.UUID) (*Project, error)
	List(ctx context.Context, f ProjectFilter) ([]*Project, int, error)
	ExistsCode(ctx context.Context, companyID uuid.UUID, code string, excludeID *uuid.UUID) (bool, error)

	// ListIDsForMember trả về id mọi dự án mà nhân viên này tham gia.
	// Dùng để giới hạn phạm vi xem của người không có quyền toàn công ty.
	ListIDsForMember(ctx context.Context, employeeID uuid.UUID) ([]uuid.UUID, error)

	// NextTaskSeq tăng bộ đếm task của dự án và trả về giá trị mới.
	// Phải chạy trong cùng giao dịch với việc chèn task.
	NextTaskSeq(ctx context.Context, projectID uuid.UUID) (int, error)
}

type MemberRepository interface {
	Add(ctx context.Context, m *Member) error
	UpdateRole(ctx context.Context, projectID, employeeID uuid.UUID, role Role) error
	Remove(ctx context.Context, projectID, employeeID uuid.UUID) error
	List(ctx context.Context, projectID uuid.UUID) ([]*Member, error)
	Get(ctx context.Context, projectID, employeeID uuid.UUID) (*Member, error)
	CountByRole(ctx context.Context, projectID uuid.UUID, role Role) (int, error)
}

type TaskRepository interface {
	Create(ctx context.Context, t *Task) error
	Update(ctx context.Context, t *Task) error
	SoftDelete(ctx context.Context, id uuid.UUID) error
	GetByID(ctx context.Context, id uuid.UUID) (*Task, error)
	List(ctx context.Context, f TaskFilter) ([]*Task, int, error)

	// ListBoard trả về mọi task gốc của một dự án, đã sắp theo cột và
	// sort_order. Không phân trang: bảng Kanban cần thấy cả cột một lúc.
	ListBoard(ctx context.Context, projectID uuid.UUID) ([]*Task, error)

	ListSubtasks(ctx context.Context, parentID uuid.UUID) ([]*Task, error)
	CountSubtasks(ctx context.Context, parentID uuid.UUID) (int, error)

	// Neighbours đọc sort_order của hai task để tính vị trí chèn vào giữa.
	// Trả về ErrNotFound nếu một trong hai không thuộc đúng dự án và cột.
	SortOrderOf(ctx context.Context, taskID uuid.UUID) (float64, error)

	// Reorder ghi vị trí mới cho một task, kèm trạng thái cột đích.
	Reorder(ctx context.Context, taskID uuid.UUID, status TaskStatus, sortOrder float64) error

	// RenumberColumn đánh số lại toàn bộ một cột theo bước đều.
	// Gọi khi khoảng cách giữa hai vị trí nhỏ tới mức không chèn được nữa.
	RenumberColumn(ctx context.Context, projectID uuid.UUID, status TaskStatus) error

	// MaxSortOrder trả về vị trí lớn nhất trong một cột, dùng khi thêm task
	// vào cuối. Trả về 0 khi cột rỗng.
	MaxSortOrder(ctx context.Context, projectID uuid.UUID, status TaskStatus) (float64, error)
}

type CommentRepository interface {
	Create(ctx context.Context, c *Comment) error
	Update(ctx context.Context, c *Comment) error
	SoftDelete(ctx context.Context, id uuid.UUID) error
	GetByID(ctx context.Context, id uuid.UUID) (*Comment, error)
	ListByTask(ctx context.Context, taskID uuid.UUID) ([]*Comment, error)
	CountByTask(ctx context.Context, taskID uuid.UUID) (int, error)
}

type AttachmentRepository interface {
	Create(ctx context.Context, a *Attachment) error
	Delete(ctx context.Context, id uuid.UUID) error
	GetByID(ctx context.Context, id uuid.UUID) (*Attachment, error)
	ListByTask(ctx context.Context, taskID uuid.UUID) ([]*Attachment, error)
}

type ActivityRepository interface {
	// Log ghi một dòng nhật ký.
	//
	// KHÔNG trả lỗi ra ngoài ở chỗ gọi: ghi nhật ký thất bại không được làm
	// hỏng thao tác nghiệp vụ đã thành công. Chỗ gọi chỉ log cảnh báo.
	Log(ctx context.Context, a *Activity) error
	ListByTask(ctx context.Context, taskID uuid.UUID, limit int) ([]*Activity, error)
}

type TimelogRepository interface {
	Create(ctx context.Context, t *Timelog) error
	Delete(ctx context.Context, id uuid.UUID) error
	GetByID(ctx context.Context, id uuid.UUID) (*Timelog, error)
	ListByTask(ctx context.Context, taskID uuid.UUID) ([]*Timelog, error)
	SumMinutesByTask(ctx context.Context, taskID uuid.UUID) (int, error)
}

type ReportRepository interface {
	ProjectProgress(ctx context.Context, projectID uuid.UUID) (*ProjectProgress, error)
	Workload(ctx context.Context, projectIDs []uuid.UUID, restrict bool) ([]*Workload, error)
}

// EmployeeLookup khai báo ĐÚNG những gì module project cần từ module nhân
// sự — không hơn.
//
// Khai báo ở ĐÂY, phía người dùng, chứ không phải ở package hr. Nhờ interface
// hẹp này, module project test được mà không cần module hr thật, và sau này
// đổi cách lấy dữ liệu nhân viên cũng không phải sửa gì trong project.
//
// Tuyệt đối không cho usecase/project gọi thẳng repository/postgres của hr.
type EmployeeLookup interface {
	// Exists cho biết nhân viên có tồn tại và CÒN LÀM VIỆC không.
	// Gộp hai câu hỏi vào một hàm vì mọi chỗ gọi đều cần cả hai: giao việc
	// cho người đã nghỉ là lỗi nghiệp vụ, không phải lỗi dữ liệu.
	Exists(ctx context.Context, id uuid.UUID) (bool, error)

	// NamesOf trả về tên của nhiều nhân viên trong một lượt, dùng để hiển
	// thị danh sách mà không tạo N+1 truy vấn.
	NamesOf(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error)

	// DepartmentOf trả về phòng ban của nhân viên, phục vụ kiểm tra phạm vi.
	DepartmentOf(ctx context.Context, id uuid.UUID) (*uuid.UUID, error)
}

// EventPublisher đẩy sự kiện nghiệp vụ lên hàng đợi.
//
// Lỗi ở đây KHÔNG được làm hỏng thao tác chính: giao việc thành công mà
// không gửi được thông báo thì vẫn là giao việc thành công.
type EventPublisher interface {
	PublishEvent(ctx context.Context, e Event) error
}

// FileStorage là cổng lưu trữ tệp đính kèm.
//
// Khai báo lại ở module này thay vì dùng chung với hr: hai module cần những
// thao tác khác nhau, và dùng chung interface sẽ buộc bên này phải biết về
// nhu cầu của bên kia.
type FileStorage interface {
	PresignPut(ctx context.Context, key, contentType string, ttl time.Duration) (string, error)
	PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error)
	Stat(ctx context.Context, key string) (size int64, contentType string, err error)
	DetectContentType(ctx context.Context, key string) (string, error)
	Delete(ctx context.Context, key string) error
}
