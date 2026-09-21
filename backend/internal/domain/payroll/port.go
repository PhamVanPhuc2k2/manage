package payroll

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type SettingsRepository interface {
	// Current trả về tham số có hiệu lực tại một thời điểm, kèm biểu thuế.
	//
	// Nhận thời điểm chứ không phải luôn lấy bản mới nhất: tính lại kỳ lương
	// tháng trước phải dùng tham số của tháng trước, không phải tham số vừa
	// sửa hôm qua.
	Current(ctx context.Context, companyID uuid.UUID, at time.Time) (*Settings, error)

	// SaveVersion ghi tham số có hiệu lực TỪ MỘT NGÀY, tạo bản mới nếu ngày
	// đó chưa có.
	//
	// Không ghi đè bản cũ. Ghi đè sẽ phá hỏng chính lời hứa của Current():
	// nhân sự sửa mức giảm trừ vào tháng Một (luật đổi), rồi tính lại kỳ
	// tháng Mười Hai — và tháng Mười Hai nhận mức của tháng Một. Sai, và
	// không có gì báo.
	//
	// Trả về id của bản đã ghi để chỗ gọi gắn biểu thuế vào đúng bản đó.
	SaveVersion(ctx context.Context, s *Settings) (uuid.UUID, error)

	ReplaceBrackets(ctx context.Context, settingsID uuid.UUID, brackets []TaxBracket) error
}

type StructureRepository interface {
	Create(ctx context.Context, s *Structure) error
	Update(ctx context.Context, s *Structure) error
	GetByID(ctx context.Context, id uuid.UUID) (*Structure, error)

	// EffectiveOn trả về cấu hình lương có hiệu lực cho một nhân viên vào
	// một ngày, kèm các thành phần lương.
	EffectiveOn(ctx context.Context, employeeID uuid.UUID, on time.Time) (*Structure, error)

	// EffectiveOnMany làm cùng việc cho nhiều người trong MỘT lượt.
	//
	// Máy tính lương chạy qua toàn bộ nhân viên; hỏi từng người một sẽ là
	// N truy vấn cộng N truy vấn nữa cho thành phần lương.
	EffectiveOnMany(ctx context.Context, employeeIDs []uuid.UUID, on time.Time) (
		map[uuid.UUID]*Structure, error)

	// History liệt kê lịch sử lương của một người, mới nhất trước.
	History(ctx context.Context, employeeID uuid.UUID) ([]*Structure, error)

	// CloseOpenEnded đóng khoảng hiệu lực của cấu hình đang mở.
	//
	// Gọi trước khi thêm cấu hình mới: hai dòng cùng mở sẽ khiến
	// EffectiveOn trả về kết quả phụ thuộc thứ tự sắp xếp, tức là không
	// xác định.
	CloseOpenEnded(ctx context.Context, employeeID uuid.UUID, before time.Time) error

	ReplaceComponents(ctx context.Context, structureID uuid.UUID, items []*Component) error
}

type PeriodRepository interface {
	Create(ctx context.Context, p *Period) error
	Update(ctx context.Context, p *Period) error
	GetByID(ctx context.Context, id uuid.UUID) (*Period, error)
	List(ctx context.Context, companyID uuid.UUID, limit int) ([]*Period, error)
	ExistsForMonth(ctx context.Context, companyID uuid.UUID, year, month int) (bool, error)

	// UpdateTotals ghi lại các số tổng sau khi chạy máy tính lương.
	UpdateTotals(ctx context.Context, periodID uuid.UUID) error
}

type PayslipRepository interface {
	// ReplaceForPeriod xoá sạch phiếu cũ của kỳ rồi ghi bộ mới, trong MỘT
	// giao dịch.
	//
	// Chạy lại máy tính lương phải cho ra kết quả sạch, không trộn lẫn với
	// lần chạy trước. Xoá rồi chèn ở hai giao dịch riêng sẽ để lại khoảng
	// thời gian kỳ lương rỗng, và nếu bước chèn hỏng thì mất hết.
	ReplaceForPeriod(ctx context.Context, periodID uuid.UUID, slips []*Payslip) error

	GetByID(ctx context.Context, id uuid.UUID) (*Payslip, error)
	GetForEmployee(ctx context.Context, periodID, employeeID uuid.UUID) (*Payslip, error)
	List(ctx context.Context, f PayslipFilter) ([]*Payslip, int, error)
	Update(ctx context.Context, p *Payslip) error

	// ListForEmployee trả về lịch sử phiếu lương của một người.
	ListForEmployee(ctx context.Context, employeeID uuid.UUID, limit int) ([]*Payslip, error)

	CostByDepartment(ctx context.Context, periodID uuid.UUID) ([]*CostRow, error)
	CostByMonth(ctx context.Context, companyID uuid.UUID, year int) ([]*CostRow, error)
}

type AuditRepository interface {
	// Log ghi một dòng nhật ký.
	//
	// KHÔNG trả lỗi ra chỗ gọi: ghi nhật ký hỏng không được chặn thao tác
	// nghiệp vụ đã thành công. Chỗ gọi chỉ ghi cảnh báo.
	Log(ctx context.Context, e *AuditEntry) error
	List(ctx context.Context, resource string, resourceID *uuid.UUID, limit int) ([]*AuditEntry, error)
}

// AttendanceLookup khai báo ĐÚNG những gì module lương cần từ module chấm
// công — không hơn.
//
// Khai báo ở ĐÂY, phía người dùng. Module lương không được import
// usecase/attendance: hai tầng nghiệp vụ import chéo nhau là đường nhanh
// nhất tới phụ thuộc vòng.
type AttendanceLookup interface {
	// WorkdaysInPeriod trả về số ngày công thực tế của nhiều người trong
	// một khoảng, lấy một lượt.
	WorkdaysInPeriod(ctx context.Context, employeeIDs []uuid.UUID, from, to time.Time) (
		map[uuid.UUID]Workdays, error)
}

// Workdays là ảnh chụp dữ liệu công dùng để tính lương.
type Workdays struct {
	Present float64
	Leave   float64
	Absent  float64
}

// EmployeeLookup khai báo ĐÚNG những gì module lương cần từ module nhân sự.
type EmployeeLookup interface {
	ListActiveIDs(ctx context.Context, departmentIDs []uuid.UUID) ([]uuid.UUID, error)
	Exists(ctx context.Context, id uuid.UUID) (bool, error)
}

// CompanyLookup là cổng lấy công ty và múi giờ hiện tại.
type CompanyLookup interface {
	CurrentCompanyID(ctx context.Context) (uuid.UUID, error)
	CurrentTimezone(ctx context.Context) (*time.Location, error)
}

// JobPublisher đẩy việc tính lương sang worker.
//
// Kỳ lương của công ty vài trăm người mất vài giây tới vài chục giây: quá
// lâu cho một request HTTP, và nếu người dùng đóng tab giữa chừng thì kỳ
// lương nằm lại ở trạng thái 'calculating' mãi mãi.
type JobPublisher interface {
	PublishCalculate(ctx context.Context, periodID uuid.UUID, requestID string) error
}

// PayslipMailer gửi phiếu lương qua email, qua hàng đợi worker.
type PayslipMailer interface {
	SendPayslip(ctx context.Context, email, name, subject, htmlBody string) error
}
