package chat

import (
	"context"
	"errors"

	"github.com/google/uuid"

	domainchat "github.com/PhamVanPhuc2k2/manage/internal/domain/chat"
	"github.com/PhamVanPhuc2k2/manage/pkg/logger"
)

// SyncSources dựng và đồng bộ nhóm tự động cho một loạt nguồn.
//
// Chạy như một job định kỳ chứ không móc vào từng thao tác thêm/bớt thành
// viên dự án: móc vào từng thao tác nghĩa là mỗi module nghiệp vụ phải nhớ
// gọi chat, và chỉ cần một đường quên gọi là nhóm lệch vĩnh viễn mà không ai
// biết. Job định kỳ tự sửa mọi sai lệch, dù chúng đến từ đâu.
//
// Trả về số nhóm đã tạo và số nhóm có thay đổi thành viên.
func (u *Usecase) SyncSources(
	ctx context.Context,
	companyID uuid.UUID,
	sources []domainchat.GroupSource,
) (created, changed int, err error) {
	log := logger.FromContext(ctx)

	for _, s := range sources {
		c, err := u.ensureGroup(ctx, companyID, s)
		if err != nil {
			log.Warn().Err(err).
				Str("source_id", s.ID.String()).
				Str("kind", string(s.Kind)).
				Msg("không dựng được nhóm tự động")
			continue
		}
		if c.created {
			created++
		}

		want, err := u.membersOf(ctx, s)
		if err != nil {
			log.Warn().Err(err).Str("source_id", s.ID.String()).
				Msg("không lấy được thành viên nguồn")
			continue
		}
		// Nguồn rỗng thì BỎ QUA, không đồng bộ.
		//
		// Một phòng ban tạm thời không còn ai gần như luôn là dữ liệu đang
		// dở, và đồng bộ theo nó sẽ gỡ sạch thành viên khỏi nhóm — mà lịch
		// sử trò chuyện thì không lấy lại được bằng lần chạy sau.
		if len(want) == 0 {
			continue
		}

		added, removed, err := u.convs.SyncMembers(ctx, c.id, want)
		if err != nil {
			log.Warn().Err(err).Str("conversation_id", c.id.String()).
				Msg("không đồng bộ được thành viên nhóm")
			continue
		}
		if added > 0 || removed > 0 {
			changed++
			log.Info().
				Str("conversation_id", c.id.String()).
				Str("name", s.Name).
				Int("added", added).
				Int("removed", removed).
				Msg("đã đồng bộ nhóm tự động")
		}
	}
	return created, changed, nil
}

// SyncAll là thân của job đồng bộ nhóm tự động, worker gọi định kỳ.
func (u *Usecase) SyncAll(ctx context.Context) (created, changed int, err error) {
	if u.sources == nil {
		return 0, 0, nil
	}

	companyID, err := u.company.CurrentCompanyID(ctx)
	if err != nil {
		return 0, 0, err
	}

	list, err := u.sources.Sources(ctx, companyID)
	if err != nil {
		return 0, 0, err
	}
	return u.SyncSources(ctx, companyID, list)
}

type ensured struct {
	id      uuid.UUID
	created bool
}

// ensureGroup tìm nhóm tự động của một nguồn, tạo mới nếu chưa có.
func (u *Usecase) ensureGroup(
	ctx context.Context,
	companyID uuid.UUID,
	s domainchat.GroupSource,
) (*ensured, error) {
	field := "department_id"
	if s.Kind == domainchat.KindProject {
		field = "project_id"
	}

	c, err := u.convs.BySource(ctx, field, s.ID)
	if err != nil && !errors.Is(err, domainchat.ErrNotFound) {
		return nil, err
	}
	if c != nil {
		// Nguồn đổi tên thì nhóm đổi theo. Không làm thì tên nhóm đứng yên ở
		// tên cũ mãi mãi, và người dùng không có cách nào sửa (nhóm tự động
		// không cho sửa tay).
		if name := groupName(s); c.Name != name {
			if err := u.convs.Update(ctx, &domainchat.Conversation{ID: c.ID, Name: name}); err != nil {
				return nil, err
			}
		}
		return &ensured{id: c.ID}, nil
	}

	fresh := &domainchat.Conversation{
		CompanyID: companyID,
		Kind:      s.Kind,
		Name:      groupName(s),
	}
	if s.Kind == domainchat.KindProject {
		fresh.ProjectID = &s.ID
	} else {
		fresh.DepartmentID = &s.ID
	}

	if err := u.convs.Create(ctx, fresh); err != nil {
		return nil, err
	}
	return &ensured{id: fresh.ID, created: true}, nil
}

// groupName đặt tiền tố theo loại nguồn.
//
// Có tiền tố để trong danh sách hội thoại người dùng phân biệt được ngay nhóm
// nào là nhóm phòng ban, nhóm nào là nhóm dự án, mà không phải mở ra xem.
func groupName(s domainchat.GroupSource) string {
	if s.Kind == domainchat.KindProject {
		return "Dự án: " + s.Name
	}
	return "Phòng: " + s.Name
}

func (u *Usecase) membersOf(ctx context.Context, s domainchat.GroupSource) ([]uuid.UUID, error) {
	if s.Kind == domainchat.KindProject {
		return u.employee.ByProject(ctx, s.ID)
	}
	return u.employee.ByDepartment(ctx, s.ID)
}
