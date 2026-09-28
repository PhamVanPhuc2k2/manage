package hr

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// =========================================================================
// NHẬP NHÂN VIÊN HÀNG LOẠT
// =========================================================================

// MaxImportRows chặn trên số dòng một lượt nhập.
//
// Một nghìn là dư cho một công ty vừa và nhỏ nhập một lần. Lớn hơn thì
// nên chia tệp: kết quả từng dòng hiện trên một trang, và một trang hai
// chục nghìn dòng lỗi thì không ai đọc hết.
const MaxImportRows = 1000

type ImportStatus string

const (
	ImportQueued     ImportStatus = "queued"
	ImportProcessing ImportStatus = "processing"
	ImportDone       ImportStatus = "done"
	ImportFailed     ImportStatus = "failed"
)

// Finished cho biết lượt nhập đã xong, dù thành công hay không.
func (s ImportStatus) Finished() bool {
	return s == ImportDone || s == ImportFailed
}

// ImportRow là một dòng của tệp, đã đổi tiêu đề cột về tên chuẩn
// (ma_nhan_vien, ho_ten, ...) nhưng GIÁ TRỊ vẫn là chuỗi thô.
//
// Chưa đổi sang kiểu thật ở bước đọc tệp: dòng sai định dạng ngày phải được
// báo là "dòng 12: ngày vào làm không hợp lệ" trong kết quả, không phải làm
// hỏng cả lượt nhập ngay lúc tải lên.
type ImportRow struct {
	Line   int               `json:"line"`
	Values map[string]string `json:"values"`
}

type ImportRowResult struct {
	Line         int        `json:"line"`
	EmployeeCode string     `json:"employee_code"`
	FullName     string     `json:"full_name"`
	EmployeeID   *uuid.UUID `json:"employee_id,omitempty"`

	// Error khác rỗng nghĩa là dòng này KHÔNG tạo được hồ sơ.
	Error string `json:"error,omitempty"`

	// Warning là hồ sơ ĐÃ tạo nhưng có phần phụ không xong — hiện chỉ có
	// "chưa tạo được tài khoản". Tách khỏi Error vì hai thứ đòi hai cách
	// xử lý khác nhau: dòng lỗi thì sửa tệp rồi nhập lại, dòng cảnh báo mà
	// nhập lại thì trùng mã.
	Warning        string `json:"warning,omitempty"`
	AccountCreated bool   `json:"account_created"`
}

// ImportActor là ảnh chụp quyền của người bấm nhập, lúc bấm.
type ImportActor struct {
	UserID               uuid.UUID   `json:"user_id"`
	EmployeeID           uuid.UUID   `json:"employee_id"`
	Scope                string      `json:"scope"`
	ManagedDepartmentIDs []uuid.UUID `json:"managed_department_ids,omitempty"`
}

type EmployeeImport struct {
	ID             uuid.UUID
	CompanyID      uuid.UUID
	FileName       string
	Status         ImportStatus
	CreateAccounts bool
	TotalRows      int
	Rows           []ImportRow
	Results        []ImportRowResult
	CreatedBy      *uuid.UUID
	CreatorName    string
	Actor          ImportActor
	Error          string
	CreatedAt      time.Time
	StartedAt      *time.Time
	FinishedAt     *time.Time

	// Đếm sẵn ở database cho trang danh sách, để khỏi kéo cả results về
	// chỉ để đếm.
	Succeeded int
	Failed    int
}

type EmployeeImportRepository interface {
	Create(ctx context.Context, imp *EmployeeImport) error

	// GetByID trả về đầy đủ, kể cả Rows và Results.
	GetByID(ctx context.Context, id uuid.UUID) (*EmployeeImport, error)

	// List trả về các lượt nhập gần nhất, KHÔNG kèm Rows và Results.
	// createdBy khác nil thì chỉ lấy của người đó.
	List(ctx context.Context, companyID uuid.UUID, createdBy *uuid.UUID, limit int) ([]*EmployeeImport, error)

	MarkProcessing(ctx context.Context, id uuid.UUID) error

	// AppendResult nối kết quả của MỘT dòng. Gọi sau mỗi dòng, không phải
	// gom cuối: worker chết giữa chừng thì phần đã làm vẫn được ghi lại.
	AppendResult(ctx context.Context, id uuid.UUID, r ImportRowResult) error

	Finish(ctx context.Context, id uuid.UUID, status ImportStatus, errMsg string) error
}
