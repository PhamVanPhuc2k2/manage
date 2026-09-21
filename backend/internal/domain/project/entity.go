// Package project chứa entity và port của module dự án và công việc.
package project

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound      = errors.New("không tìm thấy")
	ErrDuplicateCode = errors.New("mã đã tồn tại")
)

// =========================================================================
// DỰ ÁN
// =========================================================================

type Status string

const (
	StatusPlanning  Status = "planning"
	StatusActive    Status = "active"
	StatusOnHold    Status = "on_hold"
	StatusCompleted Status = "completed"
	StatusCancelled Status = "cancelled"
)

func (s Status) Valid() bool {
	switch s {
	case StatusPlanning, StatusActive, StatusOnHold, StatusCompleted, StatusCancelled:
		return true
	}
	return false
}

// Closed cho biết dự án đã kết thúc hay chưa.
//
// Dự án đã đóng thì không nhận task mới và không sửa được task cũ — gom
// điều kiện đó vào một chỗ để mọi nơi kiểm tra giống nhau.
func (s Status) Closed() bool {
	return s == StatusCompleted || s == StatusCancelled
}

// Role là vai trò của một người TRONG MỘT DỰ ÁN.
//
// Đừng nhầm với vai trò hệ thống (admin, manager...). Một trưởng phòng có
// thể chỉ là 'viewer' ở dự án của phòng khác.
type Role string

const (
	RoleOwner  Role = "owner"
	RoleMember Role = "member"
	RoleViewer Role = "viewer"
)

func (r Role) Valid() bool {
	switch r {
	case RoleOwner, RoleMember, RoleViewer:
		return true
	}
	return false
}

// CanWrite cho biết vai trò này có được tạo/sửa task hay không.
func (r Role) CanWrite() bool { return r == RoleOwner || r == RoleMember }

// CanManage cho biết vai trò này có được sửa dự án và quản lý thành viên không.
func (r Role) CanManage() bool { return r == RoleOwner }

type Project struct {
	ID           uuid.UUID
	CompanyID    uuid.UUID
	Code         string
	Name         string
	Description  string
	Status       Status
	OwnerID      uuid.UUID
	DepartmentID *uuid.UUID
	StartDate    *time.Time
	DueDate      *time.Time
	CompletedAt  *time.Time

	// Các trường dưới đây được JOIN hoặc tính lúc đọc, không lưu trong bảng.
	OwnerName      string
	DepartmentName string
	MemberCount    int
	TaskCount      int
	DoneTaskCount  int

	// ViewerRole là vai trò của người đang gọi API trong dự án này. Rỗng khi
	// người đó không phải thành viên (ví dụ giám đốc xem toàn công ty).
	ViewerRole Role

	DeletedAt *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Progress trả về phần trăm hoàn thành, làm tròn xuống.
func (p *Project) Progress() int {
	if p.TaskCount == 0 {
		return 0
	}
	return p.DoneTaskCount * 100 / p.TaskCount
}

type Member struct {
	ProjectID  uuid.UUID
	EmployeeID uuid.UUID
	Role       Role
	AddedBy    *uuid.UUID
	AddedAt    time.Time

	// JOIN lúc đọc.
	EmployeeName   string
	EmployeeCode   string
	Email          string
	PositionName   string
	DepartmentName string
	AvatarKey      string
}

type ProjectFilter struct {
	Search       string
	Status       *Status
	OwnerID      *uuid.UUID
	DepartmentID *uuid.UUID

	// MemberEmployeeID giới hạn kết quả về những dự án mà người này tham gia.
	//
	// KHÔNG đến từ query string — usecase đặt nó dựa trên phạm vi của actor.
	// Xem ghi chú tương tự ở EmployeeFilter của module hr.
	MemberEmployeeID *uuid.UUID

	Page     int
	PageSize int
	SortBy   string
	SortDesc bool
}

// =========================================================================
// CÔNG VIỆC
// =========================================================================

type TaskStatus string

const (
	TaskTodo       TaskStatus = "todo"
	TaskInProgress TaskStatus = "in_progress"
	TaskReview     TaskStatus = "review"
	TaskDone       TaskStatus = "done"
)

func (s TaskStatus) Valid() bool {
	switch s {
	case TaskTodo, TaskInProgress, TaskReview, TaskDone:
		return true
	}
	return false
}

// BoardColumns là thứ tự các cột trên bảng Kanban, từ trái sang phải.
func BoardColumns() []TaskStatus {
	return []TaskStatus{TaskTodo, TaskInProgress, TaskReview, TaskDone}
}

// allowedTransitions liệt kê những bước chuyển trạng thái hợp lệ.
//
// Quy tắc: tiến MỘT bước, hoặc lùi MỘT bước. Cấm nhảy cóc.
//
// Vì sao cấm "todo → done"? Vì mỗi bước nhảy đều có nghĩa nghiệp vụ: qua
// in_progress mới có started_at để đo thời gian làm, qua review mới có người
// kiểm. Cho nhảy thẳng thì hai mốc đó rỗng, và mọi báo cáo dựa trên chúng
// đều sai mà không ai biết.
//
// Lùi được một bước là có chủ ý: review trả về in_progress là chuyện xảy ra
// hằng ngày.
var allowedTransitions = map[TaskStatus][]TaskStatus{
	TaskTodo:       {TaskInProgress},
	TaskInProgress: {TaskTodo, TaskReview},
	TaskReview:     {TaskInProgress, TaskDone},
	TaskDone:       {TaskReview},
}

// CanTransitionTo kiểm tra một bước chuyển trạng thái có hợp lệ không.
// Chuyển sang chính nó luôn hợp lệ (không phải là thay đổi gì).
func (s TaskStatus) CanTransitionTo(next TaskStatus) bool {
	if s == next {
		return true
	}
	for _, allowed := range allowedTransitions[s] {
		if allowed == next {
			return true
		}
	}
	return false
}

type Priority string

const (
	PriorityLow    Priority = "low"
	PriorityMedium Priority = "medium"
	PriorityHigh   Priority = "high"
	PriorityUrgent Priority = "urgent"
)

func (p Priority) Valid() bool {
	switch p {
	case PriorityLow, PriorityMedium, PriorityHigh, PriorityUrgent:
		return true
	}
	return false
}

type Task struct {
	ID           uuid.UUID
	ProjectID    uuid.UUID
	Seq          int
	ParentTaskID *uuid.UUID

	Title         string
	Description   string
	Status        TaskStatus
	Priority      Priority
	AssigneeID    *uuid.UUID
	ReporterID    uuid.UUID
	DueDate       *time.Time
	EstimateHours *float64
	SortOrder     float64

	StartedAt   *time.Time
	CompletedAt *time.Time

	// JOIN hoặc tính lúc đọc.
	ProjectCode     string
	ProjectName     string
	AssigneeName    string
	AssigneeAvatar  string
	ReporterName    string
	CommentCount    int
	AttachmentCount int
	SubtaskCount    int
	DoneSubtasks    int
	SpentMinutes    int

	DeletedAt *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Overdue cho biết task đã quá hạn chưa. Task đã xong thì không còn quá hạn.
func (t *Task) Overdue(now time.Time) bool {
	return t.DueDate != nil && t.Status != TaskDone && now.After(*t.DueDate)
}

// Code trả về mã hiển thị của task, ví dụ "WEB-42".
func (t *Task) Code() string {
	if t.ProjectCode == "" {
		return ""
	}
	return t.ProjectCode + "-" + itoa(t.Seq)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

type TaskFilter struct {
	ProjectID    *uuid.UUID
	Status       *TaskStatus
	Priority     *Priority
	AssigneeID   *uuid.UUID
	ParentTaskID *uuid.UUID
	Search       string

	// OnlyRoots chỉ lấy task gốc (không phải task con). Dùng cho bảng Kanban,
	// nơi task con hiển thị lồng trong task cha chứ không đứng riêng.
	OnlyRoots bool

	// DueBefore lọc task đến hạn trước một mốc. Dùng cho báo cáo quá hạn.
	DueBefore *time.Time
	// Unfinished loại bỏ task đã done.
	Unfinished bool

	// VisibleProjectIDs giới hạn kết quả trong những dự án actor được xem.
	//
	// nil nghĩa là KHÔNG giới hạn (actor có phạm vi toàn công ty). Một lát
	// cắt RỖNG nghĩa là không dự án nào — hai trường hợp này khác nhau, đừng
	// gộp. Trường này do usecase đặt, không bao giờ bind từ query string.
	VisibleProjectIDs []uuid.UUID
	RestrictProjects  bool

	Page     int
	PageSize int
	SortBy   string
	SortDesc bool
}

// =========================================================================
// BÌNH LUẬN, TỆP ĐÍNH KÈM, NHẬT KÝ, THỜI GIAN
// =========================================================================

type Comment struct {
	ID           uuid.UUID
	TaskID       uuid.UUID
	AuthorID     uuid.UUID
	Content      string
	MentionedIDs []uuid.UUID
	EditedAt     *time.Time

	AuthorName   string
	AuthorAvatar string

	DeletedAt *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Attachment struct {
	ID          uuid.UUID
	TaskID      uuid.UUID
	UploadedBy  uuid.UUID
	StorageKey  string
	FileName    string
	ContentType string
	SizeBytes   int64
	CreatedAt   time.Time

	UploaderName string
}

// Tên các hành động ghi vào nhật ký. Khai báo hằng để không gõ sai chuỗi.
const (
	ActionCreated         = "created"
	ActionStatusChanged   = "status_changed"
	ActionAssigned        = "assigned"
	ActionFieldChanged    = "field_changed"
	ActionCommented       = "commented"
	ActionAttachmentAdded = "attachment_added"
	ActionAttachmentDel   = "attachment_removed"
	ActionTimeLogged      = "time_logged"
	ActionReordered       = "reordered"
	ActionDeleted         = "deleted"
)

type Activity struct {
	ID        uuid.UUID
	TaskID    uuid.UUID
	ActorID   *uuid.UUID
	Action    string
	Field     string
	OldValue  string
	NewValue  string
	CreatedAt time.Time

	ActorName string
}

type Timelog struct {
	ID           uuid.UUID
	TaskID       uuid.UUID
	EmployeeID   uuid.UUID
	SpentMinutes int
	Note         string
	LoggedOn     time.Time
	CreatedAt    time.Time

	EmployeeName string
	TaskTitle    string
}

// =========================================================================
// BÁO CÁO
// =========================================================================

// ProjectProgress là ảnh chụp tiến độ một dự án.
type ProjectProgress struct {
	ProjectID   uuid.UUID
	ProjectCode string
	ProjectName string
	Status      Status
	DueDate     *time.Time

	TotalTasks   int
	ByStatus     map[TaskStatus]int
	OverdueTasks int
	SpentMinutes int
	EstimateHrs  float64
}

// Progress trả về phần trăm hoàn thành.
func (p *ProjectProgress) Progress() int {
	if p.TotalTasks == 0 {
		return 0
	}
	return p.ByStatus[TaskDone] * 100 / p.TotalTasks
}

// Workload là khối lượng việc của một nhân viên.
type Workload struct {
	EmployeeID   uuid.UUID
	EmployeeName string
	DepartmentID *uuid.UUID

	OpenTasks    int
	OverdueTasks int
	DoneTasks    int
	EstimateHrs  float64
	SpentMinutes int
}

// =========================================================================
// SỰ KIỆN ĐẨY LÊN RABBITMQ
// =========================================================================

// Tên job sự kiện. Worker đăng ký handler theo đúng các tên này.
const (
	JobTaskAssigned      = "project.task_assigned"
	JobTaskStatusChanged = "project.task_status_changed"
	JobTaskMentioned     = "project.task_mentioned"
	JobTaskDueSoon       = "project.task_due_soon"
)

// Event là dữ liệu kèm theo một sự kiện của module dự án.
//
// Dùng một struct chung cho mọi loại sự kiện thay vì mỗi loại một struct:
// Phase 5 sẽ đọc chúng để sinh thông báo, và một hình dạng duy nhất khiến
// phần đó chỉ phải viết một lần.
type Event struct {
	Name       string      `json:"name"`
	TaskID     uuid.UUID   `json:"task_id"`
	TaskCode   string      `json:"task_code"`
	TaskTitle  string      `json:"task_title"`
	ProjectID  uuid.UUID   `json:"project_id"`
	ActorID    uuid.UUID   `json:"actor_id"`
	ActorName  string      `json:"actor_name"`
	Recipients []uuid.UUID `json:"recipients"`
	OldValue   string      `json:"old_value,omitempty"`
	NewValue   string      `json:"new_value,omitempty"`
}
