package project

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	domainproject "github.com/PhamVanPhuc2k2/manage/internal/domain/project"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
)

type ProjectInput struct {
	Code        string
	Name        string
	Description string
	Status      domainproject.Status

	// OwnerID là CON TRỎ để phân biệt "không truyền" với "truyền giá trị rác".
	//
	// Dùng uuid.UUID trần thì cả hai trường hợp đều ra uuid.Nil, và nhánh
	// "không truyền thì lấy người tạo" sẽ âm thầm nuốt luôn một id sai do
	// client gửi lên — người dùng tưởng đã giao dự án cho ai đó, thực ra
	// không phải.
	OwnerID      *uuid.UUID
	DepartmentID *uuid.UUID
	StartDate    *time.Time
	DueDate      *time.Time
}

type ListProjectsResult struct {
	Items      []*domainproject.Project
	Page       int
	PageSize   int
	TotalItems int
	TotalPages int
}

func (u *Usecase) ListProjects(
	ctx context.Context,
	actor *domainauth.Actor,
	f domainproject.ProjectFilter,
) (*ListProjectsResult, error) {
	// Xoá sạch giá trị phạm vi có thể lọt vào từ query string. Chỉ đoạn code
	// ngay dưới mới được đặt trường này.
	f.MemberEmployeeID = nil
	if actor != nil && actor.Scope != domainauth.ScopeAll {
		id := actor.EmployeeID
		f.MemberEmployeeID = &id
	}

	f.Page, f.PageSize = normalizePage(f.Page, f.PageSize)

	items, total, err := u.projects.List(ctx, f)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	// Gắn vai trò của người xem vào từng dự án để giao diện biết nên hiện
	// nút nào. Với phạm vi toàn công ty, vai trò có thể rỗng — đó là bình
	// thường, giám đốc xem được mọi dự án mà không phải thành viên dự án nào.
	if actor != nil {
		for _, p := range items {
			if p.OwnerID == actor.EmployeeID {
				p.ViewerRole = domainproject.RoleOwner
				continue
			}
			if m, err := u.members.Get(ctx, p.ID, actor.EmployeeID); err == nil {
				p.ViewerRole = m.Role
			}
		}
	}

	totalPages := (total + f.PageSize - 1) / f.PageSize
	return &ListProjectsResult{
		Items:      items,
		Page:       f.Page,
		PageSize:   f.PageSize,
		TotalItems: total,
		TotalPages: totalPages,
	}, nil
}

func (u *Usecase) GetProject(
	ctx context.Context,
	actor *domainauth.Actor,
	id uuid.UUID,
) (*domainproject.Project, error) {
	acc, err := u.loadAccess(ctx, actor, id)
	if err != nil {
		return nil, err
	}
	return acc.project, nil
}

func (u *Usecase) CreateProject(
	ctx context.Context,
	actor *domainauth.Actor,
	in ProjectInput,
) (*domainproject.Project, error) {
	companyID, err := u.company.CurrentCompanyID(ctx)
	if err != nil {
		return nil, err
	}

	// Không truyền chủ dự án thì người tạo làm chủ. Đây là mặc định đúng
	// trong phần lớn trường hợp và tránh được dự án không có chủ.
	//
	// Chỉ áp dụng khi trường VẮNG MẶT (nil). Truyền một id không tồn tại thì
	// phải báo lỗi, không được lặng lẽ thay bằng người tạo.
	if in.OwnerID == nil && actor != nil {
		id := actor.EmployeeID
		in.OwnerID = &id
	}

	if err := u.validateProjectInput(ctx, companyID, in, nil); err != nil {
		return nil, err
	}

	p := &domainproject.Project{
		CompanyID:    companyID,
		Code:         strings.ToUpper(strings.TrimSpace(in.Code)),
		Name:         strings.TrimSpace(in.Name),
		Description:  in.Description,
		Status:       in.Status,
		OwnerID:      *in.OwnerID,
		DepartmentID: in.DepartmentID,
		StartDate:    in.StartDate,
		DueDate:      in.DueDate,
	}
	if p.Status == "" {
		p.Status = domainproject.StatusPlanning
	}

	if err := u.projects.Create(ctx, p); err != nil {
		return nil, apperror.Internal(err)
	}

	// Chủ dự án PHẢI là thành viên.
	//
	// Thiếu bước này, chủ dự án vẫn sửa được dự án (loadAccess có nhánh dự
	// phòng theo owner_id) nhưng sẽ không thấy dự án của chính mình trong
	// danh sách — vì bộ lọc danh sách đi qua project_members.
	if err := u.members.Add(ctx, &domainproject.Member{
		ProjectID:  p.ID,
		EmployeeID: p.OwnerID,
		Role:       domainproject.RoleOwner,
		AddedBy:    actorEmployeeID(actor),
	}); err != nil {
		return nil, apperror.Internal(err)
	}

	return u.projects.GetByID(ctx, p.ID)
}

func (u *Usecase) UpdateProject(
	ctx context.Context,
	actor *domainauth.Actor,
	id uuid.UUID,
	in ProjectInput,
) (*domainproject.Project, error) {
	acc, err := u.loadAccess(ctx, actor, id)
	if err != nil {
		return nil, err
	}
	if !acc.canManage() {
		return nil, apperror.Forbidden(
			"Chỉ chủ dự án hoặc giám đốc mới được sửa dự án này")
	}

	existing := acc.project
	if err := u.validateProjectInput(ctx, existing.CompanyID, in, &id); err != nil {
		return nil, err
	}

	// Đổi chủ dự án thì chủ mới phải là thành viên, cùng lý do như lúc tạo.
	if *in.OwnerID != existing.OwnerID {
		if err := u.members.Add(ctx, &domainproject.Member{
			ProjectID:  id,
			EmployeeID: *in.OwnerID,
			Role:       domainproject.RoleOwner,
			AddedBy:    actorEmployeeID(actor),
		}); err != nil {
			return nil, apperror.Internal(err)
		}
	}

	// completed_at bám theo trạng thái. Đặt ở đây chứ không để client gửi
	// lên: mốc hoàn thành là sự thật của hệ thống, không phải dữ liệu nhập.
	switch {
	case in.Status == domainproject.StatusCompleted && existing.CompletedAt == nil:
		now := time.Now()
		existing.CompletedAt = &now
	case in.Status != domainproject.StatusCompleted:
		existing.CompletedAt = nil
	}

	existing.Code = strings.ToUpper(strings.TrimSpace(in.Code))
	existing.Name = strings.TrimSpace(in.Name)
	existing.Description = in.Description
	existing.Status = in.Status
	existing.OwnerID = *in.OwnerID
	existing.DepartmentID = in.DepartmentID
	existing.StartDate = in.StartDate
	existing.DueDate = in.DueDate

	if err := u.projects.Update(ctx, existing); err != nil {
		return nil, apperror.Internal(err)
	}
	return u.projects.GetByID(ctx, id)
}

func (u *Usecase) DeleteProject(
	ctx context.Context,
	actor *domainauth.Actor,
	id uuid.UUID,
) error {
	acc, err := u.loadAccess(ctx, actor, id)
	if err != nil {
		return err
	}
	if !acc.canManage() {
		return apperror.Forbidden("Chỉ chủ dự án hoặc giám đốc mới được xoá dự án này")
	}

	// Không cho xoá dự án đang chạy còn việc dang dở. Xoá mềm nên dữ liệu
	// không mất, nhưng dự án biến khỏi mọi màn hình và những người đang làm
	// task trong đó sẽ không hiểu chuyện gì xảy ra.
	if acc.project.TaskCount > acc.project.DoneTaskCount &&
		acc.project.Status == domainproject.StatusActive {
		return apperror.Conflict(
			"Dự án còn công việc chưa hoàn thành. Chuyển sang trạng thái tạm dừng hoặc huỷ trước khi xoá.")
	}

	if err := u.projects.SoftDelete(ctx, id); err != nil {
		return apperror.Internal(err)
	}
	return nil
}

func (u *Usecase) validateProjectInput(
	ctx context.Context,
	companyID uuid.UUID,
	in ProjectInput,
	excludeID *uuid.UUID,
) error {
	code := strings.TrimSpace(in.Code)
	if code == "" {
		return apperror.Invalid("Mã dự án không được để trống", nil)
	}
	// Mã dự án đi vào mã task hiển thị ("WEB-42") nên phải ngắn và không có
	// khoảng trắng, nếu không thì mã task sẽ rất khó đọc.
	if len(code) > 20 {
		return apperror.Invalid("Mã dự án không quá 20 ký tự", nil)
	}
	if strings.ContainsAny(code, " \t\n-") {
		return apperror.Invalid(
			"Mã dự án không được chứa khoảng trắng hoặc dấu gạch ngang", nil)
	}
	if strings.TrimSpace(in.Name) == "" {
		return apperror.Invalid("Tên dự án không được để trống", nil)
	}
	if in.Status != "" && !in.Status.Valid() {
		return apperror.Invalid("Trạng thái dự án không hợp lệ", nil)
	}
	if in.StartDate != nil && in.DueDate != nil && in.DueDate.Before(*in.StartDate) {
		return apperror.Invalid("Hạn hoàn thành phải sau ngày bắt đầu", nil)
	}

	dup, err := u.projects.ExistsCode(ctx, companyID, code, excludeID)
	if err != nil {
		return apperror.Internal(err)
	}
	if dup {
		return apperror.Conflict("Mã dự án đã tồn tại")
	}

	if in.OwnerID == nil || *in.OwnerID == uuid.Nil {
		return apperror.Invalid("Dự án phải có chủ dự án", nil)
	}
	ok, err := u.employees.Exists(ctx, *in.OwnerID)
	if err != nil {
		return apperror.Internal(err)
	}
	if !ok {
		return apperror.Invalid("Chủ dự án không tồn tại hoặc đã nghỉ việc", nil)
	}
	return nil
}

func actorEmployeeID(actor *domainauth.Actor) *uuid.UUID {
	if actor == nil {
		return nil
	}
	id := actor.EmployeeID
	return &id
}
