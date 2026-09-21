package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	domainchat "github.com/PhamVanPhuc2k2/manage/internal/domain/chat"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
)

// maxGroupMembers là trần thành viên một nhóm tạo tay.
//
// Nhóm lớn hơn thế gần như luôn là nhóm theo phòng ban hoặc dự án, và những
// nhóm đó có đường riêng để tạo — đường tự đồng bộ thành viên.
const maxGroupMembers = 200

// List trả về danh sách hội thoại của actor, kèm tin nhắn cuối và chấm xanh.
func (u *Usecase) List(
	ctx context.Context,
	actor *domainauth.Actor,
	search string,
) ([]*domainchat.Conversation, error) {
	items, err := u.convs.ListFor(ctx, actor.EmployeeID, search)
	if err != nil {
		return nil, err
	}

	peers := make([]uuid.UUID, 0, len(items))
	for _, c := range items {
		if c.PeerID != nil {
			peers = append(peers, *c.PeerID)
		}
	}

	status := u.statusOf(ctx, peers)
	for _, c := range items {
		if c.PeerID != nil {
			c.PeerStatus = statusOrOffline(status[*c.PeerID])
		}
	}
	return items, nil
}

func statusOrOffline(s string) string {
	if s == "" {
		return "offline"
	}
	return s
}

// Get trả về một hội thoại kèm thành viên.
func (u *Usecase) Get(
	ctx context.Context,
	actor *domainauth.Actor,
	id uuid.UUID,
) (*domainchat.Conversation, []*domainchat.Member, error) {
	if _, err := u.requireMember(ctx, id, actor.EmployeeID); err != nil {
		return nil, nil, err
	}

	c, err := u.convs.ByID(ctx, id, actor.EmployeeID)
	if err != nil {
		return nil, nil, notFoundOr(err)
	}

	members, err := u.convs.Members(ctx, id)
	if err != nil {
		return nil, nil, err
	}

	ids := make([]uuid.UUID, 0, len(members))
	for _, m := range members {
		ids = append(ids, m.EmployeeID)
	}
	status := u.statusOf(ctx, ids)
	for _, m := range members {
		m.Status = statusOrOffline(status[m.EmployeeID])
	}

	if c.PeerID != nil {
		c.PeerStatus = statusOrOffline(status[*c.PeerID])
	}
	return c, members, nil
}

// OpenDirect mở hội thoại 1-1, TÁI SỬ DỤNG cái đã có nếu có.
//
// Đây là điểm mấu chốt của chat 1-1: hai người bấm "nhắn tin" cho nhau ở hai
// thời điểm khác nhau phải rơi vào cùng một hội thoại, nếu không thì mỗi
// người thấy một nửa lịch sử và không ai hiểu tin nhắn kia đi đâu.
func (u *Usecase) OpenDirect(
	ctx context.Context,
	actor *domainauth.Actor,
	peerID uuid.UUID,
) (*domainchat.Conversation, error) {
	if peerID == actor.EmployeeID {
		return nil, apperror.Invalid("không thể mở hội thoại với chính mình", nil)
	}

	companyID, err := u.company.CurrentCompanyID(ctx)
	if err != nil {
		return nil, err
	}

	ok, err := u.employee.SameCompany(ctx, companyID, []uuid.UUID{peerID})
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, apperror.NotFound("nhân viên")
	}

	key := domainchat.DirectKeyFor(actor.EmployeeID, peerID)

	existing, err := u.convs.ByDirectKey(ctx, companyID, key)
	if err != nil && !errors.Is(err, domainchat.ErrNotFound) {
		return nil, err
	}
	if existing != nil {
		// Người từng rời hội thoại 1-1 (hoặc chưa từng được thêm vì dữ liệu
		// cũ) được đưa trở lại. Không làm bước này thì hội thoại tồn tại
		// nhưng người gọi không nhìn thấy nó, và cũng không tạo mới được vì
		// direct_key đã bị chiếm.
		if err := u.convs.AddMembers(ctx, existing.ID,
			[]uuid.UUID{actor.EmployeeID, peerID}, false); err != nil {
			return nil, err
		}
		return u.viewOf(ctx, existing.ID, actor.EmployeeID)
	}

	c := &domainchat.Conversation{
		CompanyID: companyID,
		Kind:      domainchat.KindDirect,
		DirectKey: key,
		CreatedBy: &actor.EmployeeID,
	}
	if err := u.convs.Create(ctx, c); err != nil {
		return nil, err
	}
	if err := u.convs.AddMembers(ctx, c.ID,
		[]uuid.UUID{actor.EmployeeID, peerID}, false); err != nil {
		return nil, err
	}

	return u.viewOf(ctx, c.ID, actor.EmployeeID)
}

// CreateGroupInput là dữ liệu tạo nhóm tay.
type CreateGroupInput struct {
	Name      string
	MemberIDs []uuid.UUID
}

// CreateGroup tạo nhóm tay. Người tạo luôn là quản trị nhóm.
func (u *Usecase) CreateGroup(
	ctx context.Context,
	actor *domainauth.Actor,
	in CreateGroupInput,
) (*domainchat.Conversation, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, apperror.Invalid("tên nhóm không được để trống", nil)
	}

	companyID, err := u.company.CurrentCompanyID(ctx)
	if err != nil {
		return nil, err
	}

	members := dedupExcept(in.MemberIDs, actor.EmployeeID)
	if len(members) > maxGroupMembers {
		return nil, apperror.Invalid(
			fmt.Sprintf("nhóm tối đa %d thành viên", maxGroupMembers), nil)
	}
	if len(members) > 0 {
		ok, err := u.employee.SameCompany(ctx, companyID, members)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, apperror.Invalid("có nhân viên không thuộc công ty", nil)
		}
	}

	c := &domainchat.Conversation{
		CompanyID: companyID,
		Kind:      domainchat.KindGroup,
		Name:      name,
		CreatedBy: &actor.EmployeeID,
	}
	if err := u.convs.Create(ctx, c); err != nil {
		return nil, err
	}

	// Người tạo vào trước với cờ quản trị, rồi mới tới những người còn lại.
	// Gộp một lượt thì mọi người cùng thành quản trị, vì cờ is_admin là tham
	// số chung cho cả lượt thêm.
	if err := u.convs.AddMembers(ctx, c.ID,
		[]uuid.UUID{actor.EmployeeID}, true); err != nil {
		return nil, err
	}
	if err := u.convs.AddMembers(ctx, c.ID, members, false); err != nil {
		return nil, err
	}

	u.announce(ctx, c.ID, actor.EmployeeID, fmt.Sprintf("%s đã tạo nhóm", actorName(ctx, u, actor)))
	return u.viewOf(ctx, c.ID, actor.EmployeeID)
}

// Rename đổi tên nhóm.
func (u *Usecase) Rename(
	ctx context.Context,
	actor *domainauth.Actor,
	id uuid.UUID,
	name string,
) (*domainchat.Conversation, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, apperror.Invalid("tên nhóm không được để trống", nil)
	}
	if _, err := u.requireAdmin(ctx, id, actor.EmployeeID); err != nil {
		return nil, err
	}

	if err := u.convs.Update(ctx, &domainchat.Conversation{ID: id, Name: name}); err != nil {
		return nil, notFoundOr(err)
	}

	u.announce(ctx, id, actor.EmployeeID,
		fmt.Sprintf("%s đã đổi tên nhóm thành \"%s\"", actorName(ctx, u, actor), name))
	return u.viewOf(ctx, id, actor.EmployeeID)
}

// AddMembers thêm người vào nhóm tay.
func (u *Usecase) AddMembers(
	ctx context.Context,
	actor *domainauth.Actor,
	id uuid.UUID,
	memberIDs []uuid.UUID,
) error {
	c, err := u.requireAdmin(ctx, id, actor.EmployeeID)
	if err != nil {
		return err
	}
	if c.Kind == domainchat.KindDirect {
		return apperror.Invalid("hội thoại 1-1 không thêm được thành viên", nil)
	}

	members := dedupExcept(memberIDs, uuid.Nil)
	if len(members) == 0 {
		return apperror.Invalid("chưa chọn thành viên nào", nil)
	}

	ok, err := u.employee.SameCompany(ctx, c.CompanyID, members)
	if err != nil {
		return err
	}
	if !ok {
		return apperror.Invalid("có nhân viên không thuộc công ty", nil)
	}

	if err := u.convs.AddMembers(ctx, id, members, false); err != nil {
		return err
	}

	names, _ := u.employee.NamesOf(ctx, members)
	u.announce(ctx, id, actor.EmployeeID, fmt.Sprintf("%s đã thêm %s",
		actorName(ctx, u, actor), joinNames(names, members)))
	return nil
}

// RemoveMember gỡ người khỏi nhóm.
func (u *Usecase) RemoveMember(
	ctx context.Context,
	actor *domainauth.Actor,
	id, employeeID uuid.UUID,
) error {
	if _, err := u.requireAdmin(ctx, id, actor.EmployeeID); err != nil {
		return err
	}
	if employeeID == actor.EmployeeID {
		return apperror.Invalid("dùng chức năng rời nhóm để tự rời", nil)
	}

	if err := u.convs.RemoveMember(ctx, id, employeeID); err != nil {
		if errors.Is(err, domainchat.ErrNotMember) {
			return apperror.NotFound("thành viên")
		}
		return err
	}

	names, _ := u.employee.NamesOf(ctx, []uuid.UUID{employeeID})
	u.announce(ctx, id, actor.EmployeeID, fmt.Sprintf("%s đã gỡ %s khỏi nhóm",
		actorName(ctx, u, actor), names[employeeID]))
	return nil
}

// SetAdmin trao hoặc thu quyền quản trị nhóm.
func (u *Usecase) SetAdmin(
	ctx context.Context,
	actor *domainauth.Actor,
	id, employeeID uuid.UUID,
	admin bool,
) error {
	if _, err := u.requireAdmin(ctx, id, actor.EmployeeID); err != nil {
		return err
	}
	// Tự bỏ quyền của chính mình có thể để lại nhóm không còn quản trị viên
	// nào, và khi đó không ai thêm/gỡ được ai nữa.
	if employeeID == actor.EmployeeID && !admin {
		return apperror.Invalid("không tự bỏ quyền quản trị của mình được", nil)
	}

	if err := u.convs.SetAdmin(ctx, id, employeeID, admin); err != nil {
		if errors.Is(err, domainchat.ErrNotMember) {
			return apperror.NotFound("thành viên")
		}
		return err
	}
	return nil
}

// Leave là đường để người dùng tự rời nhóm.
func (u *Usecase) Leave(ctx context.Context, actor *domainauth.Actor, id uuid.UUID) error {
	c, err := u.convs.ByID(ctx, id, actor.EmployeeID)
	if err != nil {
		return notFoundOr(err)
	}
	if c.Kind == domainchat.KindDirect {
		return apperror.Invalid("không rời được hội thoại 1-1", nil)
	}
	if c.Kind.Managed() {
		return apperror.Invalid(
			"nhóm theo phòng ban hoặc dự án do hệ thống quản lý, không rời được", nil)
	}

	name := actorName(ctx, u, actor)
	// Thông báo TRƯỚC khi rời: sau khi rời thì người này không còn trong danh
	// sách người nhận, và chính họ sẽ không thấy dòng thông báo mình vừa rời.
	u.announce(ctx, id, actor.EmployeeID, fmt.Sprintf("%s đã rời nhóm", name))

	if err := u.convs.RemoveMember(ctx, id, actor.EmployeeID); err != nil {
		if errors.Is(err, domainchat.ErrNotMember) {
			return apperror.NotFound("hội thoại")
		}
		return err
	}
	return nil
}

// SetPinned và SetMuted là tuỳ chọn RIÊNG của từng người trên một hội thoại.
func (u *Usecase) SetPinned(
	ctx context.Context,
	actor *domainauth.Actor,
	id uuid.UUID,
	pinned bool,
) error {
	if _, err := u.requireMember(ctx, id, actor.EmployeeID); err != nil {
		return err
	}
	return u.convs.SetPinned(ctx, id, actor.EmployeeID, pinned)
}

func (u *Usecase) SetMuted(
	ctx context.Context,
	actor *domainauth.Actor,
	id uuid.UUID,
	muted bool,
) error {
	if _, err := u.requireMember(ctx, id, actor.EmployeeID); err != nil {
		return err
	}
	return u.convs.SetMuted(ctx, id, actor.EmployeeID, muted)
}

// viewOf nạp lại hội thoại theo góc nhìn người gọi.
func (u *Usecase) viewOf(
	ctx context.Context,
	id, viewerID uuid.UUID,
) (*domainchat.Conversation, error) {
	c, err := u.convs.ByID(ctx, id, viewerID)
	if err != nil {
		return nil, notFoundOr(err)
	}
	if c.PeerID != nil {
		c.PeerStatus = statusOrOffline(u.statusOf(ctx, []uuid.UUID{*c.PeerID})[*c.PeerID])
	}
	return c, nil
}

func dedupExcept(ids []uuid.UUID, exclude uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(ids))
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id == uuid.Nil || id == exclude {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func joinNames(names map[uuid.UUID]string, ids []uuid.UUID) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		if n := names[id]; n != "" {
			parts = append(parts, n)
		}
	}
	return strings.Join(parts, ", ")
}

// actorName tra tên người đang thao tác, để dựng câu thông báo hệ thống.
func actorName(ctx context.Context, u *Usecase, actor *domainauth.Actor) string {
	names, err := u.employee.NamesOf(ctx, []uuid.UUID{actor.EmployeeID})
	if err != nil || names[actor.EmployeeID] == "" {
		return "Một thành viên"
	}
	return names[actor.EmployeeID]
}
