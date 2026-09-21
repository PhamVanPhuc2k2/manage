package project

import (
	"context"

	"github.com/google/uuid"

	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	domainproject "github.com/PhamVanPhuc2k2/manage/internal/domain/project"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
)

func (u *Usecase) ListMembers(
	ctx context.Context,
	actor *domainauth.Actor,
	projectID uuid.UUID,
) ([]*domainproject.Member, error) {
	if _, err := u.loadAccess(ctx, actor, projectID); err != nil {
		return nil, err
	}

	list, err := u.members.List(ctx, projectID)
	if err != nil {
		return nil, apperror.Internal(err)
	}
	return list, nil
}

func (u *Usecase) AddMember(
	ctx context.Context,
	actor *domainauth.Actor,
	projectID, employeeID uuid.UUID,
	role domainproject.Role,
) (*domainproject.Member, error) {
	acc, err := u.loadAccess(ctx, actor, projectID)
	if err != nil {
		return nil, err
	}
	if !acc.canManage() {
		return nil, apperror.Forbidden("Chỉ chủ dự án mới được thêm thành viên")
	}
	if role == "" {
		role = domainproject.RoleMember
	}
	if !role.Valid() {
		return nil, apperror.Invalid("Vai trò trong dự án không hợp lệ", nil)
	}

	ok, err := u.employees.Exists(ctx, employeeID)
	if err != nil {
		return nil, apperror.Internal(err)
	}
	if !ok {
		return nil, apperror.Invalid("Nhân viên không tồn tại hoặc đã nghỉ việc", nil)
	}

	m := &domainproject.Member{
		ProjectID:  projectID,
		EmployeeID: employeeID,
		Role:       role,
		AddedBy:    actorEmployeeID(actor),
	}
	if err := u.members.Add(ctx, m); err != nil {
		return nil, apperror.Internal(err)
	}
	return u.members.Get(ctx, projectID, employeeID)
}

func (u *Usecase) UpdateMemberRole(
	ctx context.Context,
	actor *domainauth.Actor,
	projectID, employeeID uuid.UUID,
	role domainproject.Role,
) (*domainproject.Member, error) {
	acc, err := u.loadAccess(ctx, actor, projectID)
	if err != nil {
		return nil, err
	}
	if !acc.canManage() {
		return nil, apperror.Forbidden("Chỉ chủ dự án mới được đổi vai trò thành viên")
	}
	if !role.Valid() {
		return nil, apperror.Invalid("Vai trò trong dự án không hợp lệ", nil)
	}

	if err := u.ensureNotLastOwner(ctx, projectID, employeeID, role); err != nil {
		return nil, err
	}

	if err := u.members.UpdateRole(ctx, projectID, employeeID, role); err != nil {
		return nil, apperror.NotFound("thành viên trong dự án")
	}
	return u.members.Get(ctx, projectID, employeeID)
}

func (u *Usecase) RemoveMember(
	ctx context.Context,
	actor *domainauth.Actor,
	projectID, employeeID uuid.UUID,
) error {
	acc, err := u.loadAccess(ctx, actor, projectID)
	if err != nil {
		return err
	}

	// Ai cũng tự rời dự án được; gỡ NGƯỜI KHÁC thì phải là chủ dự án.
	selfLeave := actor != nil && actor.EmployeeID == employeeID
	if !selfLeave && !acc.canManage() {
		return apperror.Forbidden("Chỉ chủ dự án mới được gỡ thành viên")
	}

	// Chủ dự án không tự rời được — phải chuyển quyền cho người khác trước.
	// Cho rời sẽ để lại dự án không có chủ, không ai sửa hay đóng được nó.
	if employeeID == acc.project.OwnerID {
		return apperror.Conflict(
			"Không gỡ được chủ dự án. Chuyển quyền chủ dự án cho người khác trước.")
	}
	if err := u.ensureNotLastOwner(ctx, projectID, employeeID, domainproject.RoleViewer); err != nil {
		return err
	}

	// Task đang giao cho người bị gỡ vẫn giữ nguyên người thực hiện.
	//
	// CÓ CHỦ Ý: tự động bỏ gán sẽ làm mất dấu vết ai từng làm việc đó, và
	// chủ dự án sẽ không biết có bao nhiêu việc vừa trở thành vô chủ. Báo
	// cáo khối lượng việc vẫn hiện tên họ, đó là tín hiệu để giao lại.
	if err := u.members.Remove(ctx, projectID, employeeID); err != nil {
		return apperror.NotFound("thành viên trong dự án")
	}
	return nil
}

// ensureNotLastOwner chặn thao tác biến dự án thành không có owner nào.
//
// Kiểm tra ở đây chứ không dựa vào ràng buộc database: PostgreSQL không diễn
// đạt được "bảng này phải luôn có ít nhất một dòng role = 'owner' cho mỗi
// project_id" bằng CHECK.
func (u *Usecase) ensureNotLastOwner(
	ctx context.Context,
	projectID, employeeID uuid.UUID,
	newRole domainproject.Role,
) error {
	if newRole == domainproject.RoleOwner {
		return nil // vẫn là owner, không giảm số lượng
	}

	current, err := u.members.Get(ctx, projectID, employeeID)
	if err != nil {
		return apperror.NotFound("thành viên trong dự án")
	}
	if current.Role != domainproject.RoleOwner {
		return nil // vốn không phải owner
	}

	n, err := u.members.CountByRole(ctx, projectID, domainproject.RoleOwner)
	if err != nil {
		return apperror.Internal(err)
	}
	if n <= 1 {
		return apperror.Conflict(
			"Dự án phải có ít nhất một chủ dự án. Chỉ định người khác trước.")
	}
	return nil
}
