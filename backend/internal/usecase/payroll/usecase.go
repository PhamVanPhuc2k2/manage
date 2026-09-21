// Package payroll là tầng nghiệp vụ của module lương.
package payroll

import (
	"context"
	"time"

	"github.com/google/uuid"

	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	domainpay "github.com/PhamVanPhuc2k2/manage/internal/domain/payroll"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
	"github.com/PhamVanPhuc2k2/manage/pkg/logger"
)

const (
	defaultPageSize = 50
	maxPageSize     = 500

	// Số kỳ lương và phiếu lương trả về tối đa cho các API liệt kê.
	maxPeriods  = 60
	maxPayslips = 36
	maxAuditRow = 200
)

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// Clock cho phép thay đồng hồ trong test.
type Clock interface{ Now() time.Time }

type Usecase struct {
	settings   domainpay.SettingsRepository
	structures domainpay.StructureRepository
	periods    domainpay.PeriodRepository
	payslips   domainpay.PayslipRepository
	audit      domainpay.AuditRepository

	attendance domainpay.AttendanceLookup
	employees  domainpay.EmployeeLookup
	company    domainpay.CompanyLookup

	// jobs có thể nil trong worker (worker tự chạy, không cần đẩy việc cho
	// chính mình). Mọi chỗ dùng đều kiểm tra nil.
	jobs   domainpay.JobPublisher
	mailer domainpay.PayslipMailer

	clock Clock
}

func NewUsecase(
	settings domainpay.SettingsRepository,
	structures domainpay.StructureRepository,
	periods domainpay.PeriodRepository,
	payslips domainpay.PayslipRepository,
	audit domainpay.AuditRepository,
	attendance domainpay.AttendanceLookup,
	employees domainpay.EmployeeLookup,
	company domainpay.CompanyLookup,
	jobs domainpay.JobPublisher,
	mailer domainpay.PayslipMailer,
) *Usecase {
	return &Usecase{
		settings:   settings,
		structures: structures,
		periods:    periods,
		payslips:   payslips,
		audit:      audit,
		attendance: attendance,
		employees:  employees,
		company:    company,
		jobs:       jobs,
		mailer:     mailer,
		clock:      systemClock{},
	}
}

func (u *Usecase) SetClock(c Clock) { u.clock = c }

func normalizePage(page, size int) (int, int) {
	if page <= 0 {
		page = 1
	}
	switch {
	case size <= 0:
		size = defaultPageSize
	case size > maxPageSize:
		size = maxPageSize
	}
	return page, size
}

func (u *Usecase) location(ctx context.Context) *time.Location {
	loc, err := u.company.CurrentTimezone(ctx)
	if err != nil || loc == nil {
		return time.Local
	}
	return loc
}

// =========================================================================
// PHÂN QUYỀN
//
// Lương là dữ liệu nhạy cảm nhất trong hệ thống. Mặc định là CHỈ XEM ĐƯỢC
// CỦA MÌNH, và không có ngoại lệ theo phòng ban như chấm công — trưởng
// phòng KHÔNG xem được lương nhân viên phòng mình.
// =========================================================================

// canReadAll: được xem bảng lương của người khác.
func canReadAll(actor *domainauth.Actor) bool {
	return actor != nil && actor.Can(domainauth.PermPayrollReadAll)
}

// requireOwnOrAll kiểm tra actor có được xem phiếu lương của một người không.
//
// Trả về 404 chứ không phải 403: xác nhận "người này có phiếu lương nhưng
// bạn không được xem" đã là rò rỉ. Cùng nguyên tắc với các module khác.
func requireOwnOrAll(actor *domainauth.Actor, employeeID uuid.UUID) error {
	if actor == nil {
		return apperror.New(apperror.KindUnauthorized, "Chưa xác thực")
	}
	if actor.EmployeeID == employeeID || canReadAll(actor) {
		return nil
	}
	return apperror.NotFound("phiếu lương")
}

// scopeFor trả về giới hạn danh sách phiếu lương theo quyền của actor.
func scopeFor(actor *domainauth.Actor) (ids []uuid.UUID, restrict bool) {
	if canReadAll(actor) {
		return nil, false
	}
	if actor == nil {
		return []uuid.UUID{}, true
	}
	return []uuid.UUID{actor.EmployeeID}, true
}

// =========================================================================
// NHẬT KÝ TRUY CẬP
// =========================================================================

// logAccess ghi một dòng nhật ký, nuốt lỗi.
//
// Nuốt lỗi vì thao tác nghiệp vụ đã xong; trả lỗi lúc này chỉ khiến người
// dùng thấy "thất bại" trong khi việc của họ đã thành công. Nhưng lỗi vẫn
// được ghi ở mức Error chứ không phải Warn: nhật ký truy cập dữ liệu lương
// hỏng là sự cố tuân thủ, không phải phiền toái nhỏ.
func (u *Usecase) logAccess(
	ctx context.Context,
	actor *domainauth.Actor,
	action, resource string,
	resourceID *uuid.UUID,
	detail map[string]any,
) {
	e := &domainpay.AuditEntry{
		Action:     action,
		Resource:   resource,
		ResourceID: resourceID,
		Detail:     detail,
	}
	if actor != nil {
		id := actor.EmployeeID
		e.ActorID = &id
	}
	if md, ok := auditMetaFrom(ctx); ok {
		e.IP = md.IP
		e.RequestID = md.RequestID
	}

	if err := u.audit.Log(ctx, e); err != nil {
		log := logger.FromContext(ctx)
		log.Error().Err(err).
			Str("action", action).
			Msg("KHÔNG ghi được nhật ký truy cập dữ liệu lương")
	}
}

// auditMeta mang IP và request id từ tầng HTTP xuống usecase.
//
// Đi qua context thay vì thêm tham số vào mọi hàm: chúng là dữ liệu của
// TẦNG VẬN CHUYỂN, và usecase không nên có chữ "IP" trong chữ ký hàm.
type auditMeta struct {
	IP        string
	RequestID string
}

type auditMetaKey struct{}

// WithAuditMeta gắn thông tin request vào context. Gọi ở tầng HTTP.
func WithAuditMeta(ctx context.Context, ip, requestID string) context.Context {
	return context.WithValue(ctx, auditMetaKey{}, auditMeta{IP: ip, RequestID: requestID})
}

func auditMetaFrom(ctx context.Context) (auditMeta, bool) {
	m, ok := ctx.Value(auditMetaKey{}).(auditMeta)
	return m, ok
}

// ListAudit trả về nhật ký truy cập. Chỉ người có quyền audit:read.
func (u *Usecase) ListAudit(
	ctx context.Context,
	resource string,
	resourceID *uuid.UUID,
) ([]*domainpay.AuditEntry, error) {
	list, err := u.audit.List(ctx, resource, resourceID, maxAuditRow)
	if err != nil {
		return nil, apperror.Internal(err)
	}
	return list, nil
}
