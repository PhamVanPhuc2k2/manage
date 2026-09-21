// Package project là tầng nghiệp vụ của module dự án và công việc.
package project

import (
	"context"

	"github.com/google/uuid"

	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	domainproject "github.com/PhamVanPhuc2k2/manage/internal/domain/project"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
	"github.com/PhamVanPhuc2k2/manage/pkg/logger"
)

const (
	defaultPageSize = 20
	maxPageSize     = 100

	// Nhật ký một task hiếm khi cần xem quá xa. Giới hạn để một task bị sửa
	// hàng nghìn lần không kéo sập màn hình chi tiết.
	maxActivityRows = 100
)

// CompanyLookup là cổng lấy id công ty hiện tại.
//
// Module project không có bảng companies của riêng nó, và cũng không được
// import usecase/hr (import chéo giữa hai tầng nghiệp vụ là đường nhanh nhất
// tới phụ thuộc vòng). Một interface một method giải quyết gọn.
type CompanyLookup interface {
	CurrentCompanyID(ctx context.Context) (uuid.UUID, error)
}

type Usecase struct {
	projects    domainproject.Repository
	members     domainproject.MemberRepository
	tasks       domainproject.TaskRepository
	comments    domainproject.CommentRepository
	attachments domainproject.AttachmentRepository
	activities  domainproject.ActivityRepository
	timelogs    domainproject.TimelogRepository
	reports     domainproject.ReportRepository

	employees domainproject.EmployeeLookup
	company   CompanyLookup

	// storage có thể nil khi chưa cấu hình Cloudflare R2. Mọi chỗ dùng phải
	// kiểm tra nil và báo lỗi rõ ràng — hệ thống vẫn chạy, chỉ riêng chức
	// năng tệp đính kèm là không.
	storage domainproject.FileStorage

	// events có thể nil trong test. Mọi chỗ phát sự kiện đều kiểm tra nil.
	events domainproject.EventPublisher
}

func NewUsecase(
	projects domainproject.Repository,
	members domainproject.MemberRepository,
	tasks domainproject.TaskRepository,
	comments domainproject.CommentRepository,
	attachments domainproject.AttachmentRepository,
	activities domainproject.ActivityRepository,
	timelogs domainproject.TimelogRepository,
	reports domainproject.ReportRepository,
	employees domainproject.EmployeeLookup,
	company CompanyLookup,
	storage domainproject.FileStorage,
	events domainproject.EventPublisher,
) *Usecase {
	return &Usecase{
		projects:    projects,
		members:     members,
		tasks:       tasks,
		comments:    comments,
		attachments: attachments,
		activities:  activities,
		timelogs:    timelogs,
		reports:     reports,
		employees:   employees,
		company:     company,
		storage:     storage,
		events:      events,
	}
}

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

// =========================================================================
// KIỂM TRA QUYỀN TRÊN DỰ ÁN
//
// ĐÂY là trái tim phân quyền của module. Middleware chỉ biết "người này có
// quyền task:update", nó KHÔNG biết task đó thuộc dự án nào và người này có
// chân trong dự án đó hay không. Bỏ qua lớp này là mở toang cửa: bất kỳ nhân
// viên nào cũng sửa được task của mọi dự án trong công ty.
// =========================================================================

// access gom kết quả kiểm tra quyền trên một dự án cụ thể.
type access struct {
	project *domainproject.Project
	// role là vai trò trong dự án. Rỗng khi actor không phải thành viên
	// nhưng vẫn được xem nhờ phạm vi toàn công ty.
	role domainproject.Role
	// companyWide là true khi actor có phạm vi dữ liệu "all".
	companyWide bool
}

// canManage: được sửa dự án, quản lý thành viên, đóng dự án.
func (a access) canManage() bool { return a.companyWide || a.role.CanManage() }

// canWrite: được tạo và sửa task trong dự án.
func (a access) canWrite() bool { return a.companyWide || a.role.CanWrite() }

// loadAccess đọc dự án và tính quyền của actor trên nó.
//
// Trả về NotFound thay vì Forbidden khi actor không được xem. 403 xác nhận
// rằng dự án có tồn tại — với người ngoài, đó đã là rò rỉ thông tin. Cùng
// nguyên tắc đang dùng ở GetEmployee của module hr.
func (u *Usecase) loadAccess(
	ctx context.Context,
	actor *domainauth.Actor,
	projectID uuid.UUID,
) (access, error) {
	p, err := u.projects.GetByID(ctx, projectID)
	if err != nil {
		return access{}, apperror.NotFound("dự án")
	}

	companyWide := actor != nil && actor.Scope == domainauth.ScopeAll

	var role domainproject.Role
	if actor != nil {
		if m, err := u.members.Get(ctx, projectID, actor.EmployeeID); err == nil {
			role = m.Role
		} else if p.OwnerID == actor.EmployeeID {
			// Chủ dự án luôn có quyền owner kể cả khi dòng project_members
			// bị thiếu vì dữ liệu sửa tay.
			role = domainproject.RoleOwner
		}
	}

	if !companyWide && role == "" {
		return access{}, apperror.NotFound("dự án")
	}

	p.ViewerRole = role
	return access{project: p, role: role, companyWide: companyWide}, nil
}

// visibleProjectIDs trả về danh sách dự án actor được xem, và cờ cho biết có
// phải giới hạn hay không.
//
// restrict=false nghĩa là xem tất cả. restrict=true kèm danh sách RỖNG nghĩa
// là không thấy dự án nào — hai trường hợp này phải phân biệt được, gộp lại
// là nhân viên thường nhìn thấy toàn bộ task của công ty.
func (u *Usecase) visibleProjectIDs(
	ctx context.Context,
	actor *domainauth.Actor,
) (ids []uuid.UUID, restrict bool, err error) {
	if actor == nil {
		return []uuid.UUID{}, true, nil
	}
	if actor.Scope == domainauth.ScopeAll {
		return nil, false, nil
	}

	ids, err = u.projects.ListIDsForMember(ctx, actor.EmployeeID)
	if err != nil {
		return nil, true, apperror.Internal(err)
	}
	if ids == nil {
		ids = []uuid.UUID{}
	}
	return ids, true, nil
}

// logActivity ghi một dòng nhật ký, nuốt lỗi.
//
// Nuốt lỗi là CÓ CHỦ Ý: thao tác nghiệp vụ đã thành công và đã ghi vào
// database. Trả lỗi ra ngoài lúc này chỉ khiến người dùng thấy "thất bại"
// trong khi việc của họ đã xong, và họ sẽ bấm lại — tạo ra bản ghi trùng.
func (u *Usecase) logActivity(ctx context.Context, a *domainproject.Activity) {
	if err := u.activities.Log(ctx, a); err != nil {
		log := logger.FromContext(ctx)
		log.Warn().Err(err).
			Str("task_id", a.TaskID.String()).
			Str("action", a.Action).
			Msg("không ghi được nhật ký công việc")
	}
}

// publish phát một sự kiện, nuốt lỗi. Cùng lý do với logActivity: giao việc
// thành công mà không gửi được thông báo thì vẫn là giao việc thành công.
func (u *Usecase) publish(ctx context.Context, e domainproject.Event) {
	if u.events == nil || len(e.Recipients) == 0 {
		return
	}
	if err := u.events.PublishEvent(ctx, e); err != nil {
		log := logger.FromContext(ctx)
		log.Warn().Err(err).
			Str("event", e.Name).
			Str("task_id", e.TaskID.String()).
			Msg("không phát được sự kiện dự án")
	}
}
