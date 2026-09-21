package attendance

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type ScheduleRepository interface {
	Create(ctx context.Context, s *Schedule) error
	Update(ctx context.Context, s *Schedule) error
	Delete(ctx context.Context, id uuid.UUID) error
	GetByID(ctx context.Context, id uuid.UUID) (*Schedule, error)
	List(ctx context.Context, companyID uuid.UUID) ([]*Schedule, error)

	// Applicable trả về mọi khung giờ có thể áp dụng cho một nhân viên vào
	// một ngày: của chính họ, của phòng họ, và của công ty.
	//
	// Trả về TẤT CẢ rồi để usecase chọn cái hẹp nhất, thay vì chọn sẵn trong
	// SQL: luật ưu tiên là luật nghiệp vụ, và nó phải nằm ở tầng nghiệp vụ
	// nơi có thể đọc và sửa được.
	Applicable(ctx context.Context, employeeID uuid.UUID, on time.Time) ([]*Schedule, error)
}

type HolidayRepository interface {
	Create(ctx context.Context, h *Holiday) error
	Delete(ctx context.Context, id uuid.UUID) error
	ListBetween(ctx context.Context, companyID uuid.UUID, from, to time.Time) ([]*Holiday, error)
}

type SessionRepository interface {
	Create(ctx context.Context, s *Session) error

	// ExtendLatest nới dài phiên gần nhất của một người nếu khoảng hở còn
	// trong ngưỡng. Trả về false khi không có phiên nào phù hợp — khi đó
	// usecase tạo phiên mới.
	//
	// Gộp "tìm rồi cập nhật" vào một lệnh SQL để hai lần quét chạy song song
	// (hai instance worker) không cùng thấy "chưa có phiên" rồi cùng tạo mới.
	ExtendLatest(ctx context.Context, employeeID uuid.UUID, until time.Time,
		addActiveMinutes int, maxGap time.Duration) (bool, error)

	ListByDay(ctx context.Context, employeeID uuid.UUID, day time.Time) ([]*Session, error)
	ListBetween(ctx context.Context, employeeID uuid.UUID, from, to time.Time) ([]*Session, error)

	// AggregateDay tổng hợp phiên của một người trong một ngày.
	AggregateDay(ctx context.Context, employeeID uuid.UUID, day time.Time) (
		onlineMinutes, activeMinutes int, first, last *time.Time, err error)

	// EmployeesWithSessions liệt kê những người có phiên trong một ngày,
	// dùng cho job tổng hợp cuối ngày.
	EmployeesWithSessions(ctx context.Context, day time.Time) ([]uuid.UUID, error)
}

type DayRepository interface {
	Upsert(ctx context.Context, d *Day) error
	GetByDate(ctx context.Context, employeeID uuid.UUID, day time.Time) (*Day, error)
	List(ctx context.Context, f DayFilter) ([]*Day, error)
	MonthSummary(ctx context.Context, f DayFilter) ([]*MonthSummary, error)
	SetLocked(ctx context.Context, from, to time.Time, locked bool) (int64, error)
}

type AdjustmentRepository interface {
	Create(ctx context.Context, a *Adjustment) error
	Update(ctx context.Context, a *Adjustment) error
	GetByID(ctx context.Context, id uuid.UUID) (*Adjustment, error)
	List(ctx context.Context, employeeIDs []uuid.UUID, restrict bool,
		status *ApprovalStatus) ([]*Adjustment, error)
}

type LeaveRepository interface {
	Create(ctx context.Context, r *LeaveRequest) error
	Update(ctx context.Context, r *LeaveRequest) error
	GetByID(ctx context.Context, id uuid.UUID) (*LeaveRequest, error)
	List(ctx context.Context, f LeaveFilter) ([]*LeaveRequest, int, error)

	// Overlapping tìm đơn đã duyệt hoặc đang chờ trùng khoảng ngày.
	// Dùng để chặn gửi hai đơn cho cùng một ngày.
	Overlapping(ctx context.Context, employeeID uuid.UUID, from, to time.Time,
		excludeID *uuid.UUID) ([]*LeaveRequest, error)

	// ApprovedOn liệt kê những người được duyệt nghỉ trong một ngày, dùng
	// cho job tổng hợp để đánh dấu trạng thái ngày là 'leave'.
	ApprovedOn(ctx context.Context, day time.Time) (map[uuid.UUID]*LeaveRequest, error)
}

type BalanceRepository interface {
	Get(ctx context.Context, employeeID uuid.UUID, year int) (*Balance, error)
	Upsert(ctx context.Context, b *Balance) error

	// AddUsed cộng (hoặc trừ, khi số âm) số ngày đã dùng.
	//
	// Là một lệnh cộng dồn chứ không phải đọc-sửa-ghi: hai đơn được duyệt
	// cùng lúc theo kiểu đọc-sửa-ghi sẽ ghi đè nhau và quỹ phép sai.
	AddUsed(ctx context.Context, employeeID uuid.UUID, year int, delta float64) error

	List(ctx context.Context, year int, employeeIDs []uuid.UUID, restrict bool) ([]*Balance, error)
}

// EmployeeLookup khai báo ĐÚNG những gì module chấm công cần từ module nhân
// sự — không hơn. Cùng nguyên tắc với EmployeeLookup của module dự án.
type EmployeeLookup interface {
	Exists(ctx context.Context, id uuid.UUID) (bool, error)
	DepartmentOf(ctx context.Context, id uuid.UUID) (*uuid.UUID, error)

	// ListActiveIDs trả về id mọi nhân viên còn làm việc, tuỳ chọn lọc theo
	// phòng ban. Job tổng hợp cuối ngày cần nó để đánh dấu người VẮNG —
	// người vắng không có phiên nào nên không thể suy ra từ bảng phiên.
	ListActiveIDs(ctx context.Context, departmentIDs []uuid.UUID) ([]uuid.UUID, error)
}

// PresenceReader là cổng đọc trạng thái hiện diện.
//
// Khai báo lại ở đây thay vì import domain/realtime: module chấm công chỉ
// cần đọc, và một interface hẹp khiến nó test được mà không cần Redis.
type PresenceReader interface {
	// SnapshotOnline trả về ai đang online và ai trong số đó đang hoạt động.
	SnapshotOnline(ctx context.Context) ([]PresenceSnapshot, error)
	StatusOf(ctx context.Context, employeeIDs []uuid.UUID) (map[uuid.UUID]string, error)
}

// PresenceSnapshot là ảnh chụp trạng thái một người tại thời điểm quét.
type PresenceSnapshot struct {
	EmployeeID uuid.UUID
	IsActive   bool
	LastSeenAt time.Time
}

// Clock cho phép thay đồng hồ trong test.
//
// Toàn bộ module này xoay quanh thời gian, và test một hệ thống chấm công
// bằng đồng hồ thật nghĩa là phải chờ thật.
type Clock interface {
	Now() time.Time
}
