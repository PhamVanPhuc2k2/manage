// Package notification chứa entity và port của module thông báo.
//
// Module này là NƠI NHẬN của mọi module nghiệp vụ khác: giao việc, duyệt
// đơn, tin nhắn mới đều sinh ra thông báo. Nó cố ý không biết gì về các
// module đó — chúng chỉ đưa vào một Request với đủ thông tin để hiển thị.
package notification

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var ErrNotFound = errors.New("không tìm thấy")

// Type là loại thông báo. Khớp với enum notification_type trong database.
type Type string

const (
	TypeTaskAssigned      Type = "task_assigned"
	TypeTaskStatusChanged Type = "task_status_changed"
	TypeTaskMentioned     Type = "task_mentioned"
	TypeTaskDueSoon       Type = "task_due_soon"
	TypeLeaveRequested    Type = "leave_requested"
	TypeLeaveDecided      Type = "leave_decided"
	TypeAdjustmentRequest Type = "adjustment_requested"
	TypeAdjustmentDecided Type = "adjustment_decided"
	TypePayslipReady      Type = "payslip_ready"
	TypeNewMessage        Type = "new_message"
	TypeSystem            Type = "system"
)

func (t Type) Valid() bool {
	switch t {
	case TypeTaskAssigned, TypeTaskStatusChanged, TypeTaskMentioned, TypeTaskDueSoon,
		TypeLeaveRequested, TypeLeaveDecided, TypeAdjustmentRequest,
		TypeAdjustmentDecided, TypePayslipReady, TypeNewMessage, TypeSystem:
		return true
	}
	return false
}

// Important cho biết loại này có gửi email nhắc khi người nhận offline lâu
// hay không.
//
// Danh sách hẹp có chủ ý. Gửi email cho mọi loại thông báo là cách nhanh
// nhất khiến người ta lập bộ lọc cho tất cả email từ hệ thống — và khi đó
// cả những email thật sự quan trọng cũng không ai đọc.
func (t Type) Important() bool {
	switch t {
	case TypeTaskAssigned, TypeLeaveDecided, TypeAdjustmentDecided, TypePayslipReady:
		return true
	}
	return false
}

// Mutable cho biết người dùng có được tắt loại này không.
//
// Thông báo hệ thống và quyết định đơn từ thì không: chúng mang thông tin
// người nhận buộc phải biết, và cho tắt sẽ tạo ra tình huống "tôi không
// biết đơn bị từ chối" mà không ai giải quyết được.
func (t Type) Mutable() bool {
	switch t {
	case TypeSystem, TypeLeaveDecided, TypeAdjustmentDecided, TypePayslipReady:
		return false
	}
	return true
}

var allTypes = []Type{
	TypeTaskAssigned, TypeTaskStatusChanged, TypeTaskMentioned, TypeTaskDueSoon,
	TypeLeaveRequested, TypeLeaveDecided, TypeAdjustmentRequest,
	TypeAdjustmentDecided, TypePayslipReady, TypeNewMessage, TypeSystem,
}

// AllTypes trả về danh mục loại thông báo, phục vụ màn hình cấu hình.
func AllTypes() []Type {
	out := make([]Type, len(allTypes))
	copy(out, allTypes)
	return out
}

// Label là tên hiển thị của loại thông báo.
func (t Type) Label() string {
	return map[Type]string{
		TypeTaskAssigned:      "Được giao việc",
		TypeTaskStatusChanged: "Công việc đổi trạng thái",
		TypeTaskMentioned:     "Được nhắc tên",
		TypeTaskDueSoon:       "Công việc sắp đến hạn",
		TypeLeaveRequested:    "Có đơn nghỉ phép cần duyệt",
		TypeLeaveDecided:      "Đơn nghỉ phép có kết quả",
		TypeAdjustmentRequest: "Có yêu cầu điều chỉnh công",
		TypeAdjustmentDecided: "Yêu cầu điều chỉnh có kết quả",
		TypePayslipReady:      "Phiếu lương đã sẵn sàng",
		TypeNewMessage:        "Tin nhắn mới",
		TypeSystem:            "Thông báo hệ thống",
	}[t]
}

type Notification struct {
	ID         uuid.UUID
	EmployeeID uuid.UUID

	Type  Type
	Title string
	Body  string
	Link  string

	ActorID    *uuid.UUID
	Resource   string
	ResourceID *uuid.UUID

	ReadAt    *time.Time
	EmailedAt *time.Time
	CreatedAt time.Time

	ActorName string // JOIN lúc đọc
}

func (n *Notification) Read() bool { return n.ReadAt != nil }

// Request là yêu cầu tạo thông báo, do module nghiệp vụ khác đưa vào.
//
// Chứa ĐỦ thông tin để hiển thị: tiêu đề, nội dung, đường dẫn. Module thông
// báo không đi tra ngược dữ liệu gốc — làm vậy sẽ buộc nó phải biết về mọi
// module trong hệ thống, và một thông báo sẽ hỏng khi đối tượng gốc bị xoá.
type Request struct {
	Recipients []uuid.UUID
	Type       Type
	Title      string
	Body       string
	Link       string
	ActorID    *uuid.UUID
	Resource   string
	ResourceID *uuid.UUID
}

// Filter gom điều kiện lọc danh sách thông báo.
type Filter struct {
	EmployeeID uuid.UUID
	UnreadOnly bool
	Type       *Type

	// Cursor phân trang: chỉ lấy thông báo CŨ HƠN mốc này.
	//
	// Dùng cursor chứ không dùng offset vì danh sách thay đổi liên tục từ
	// đầu: thông báo mới chen vào trên cùng sẽ đẩy mọi thứ xuống, và trang
	// 2 theo offset sẽ lặp lại những dòng đã thấy ở trang 1.
	Before *time.Time
	Limit  int
}

// Summary là con số hiển thị trên chuông thông báo.
type Summary struct {
	Unread int `json:"unread"`
}
