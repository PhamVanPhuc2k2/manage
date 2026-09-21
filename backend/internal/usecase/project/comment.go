package project

import (
	"context"
	"regexp"
	"strings"

	"github.com/google/uuid"

	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	domainproject "github.com/PhamVanPhuc2k2/manage/internal/domain/project"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
)

const maxCommentLength = 5000

// mentionPattern khớp cú pháp @[tên](uuid) do trình soạn thảo sinh ra.
//
// Vì sao không bắt @tên trần? Vì tên trùng nhau là chuyện thường trong công
// ty, và đoán xem "@Hà" là ai sẽ sai. Trình soạn thảo ở frontend cho người
// dùng chọn từ danh sách rồi nhúng sẵn id vào — ở đây chỉ việc đọc ra.
var mentionPattern = regexp.MustCompile(`@\[[^\]]{1,100}\]\(([0-9a-fA-F-]{36})\)`)

// extractMentions rút danh sách id được nhắc tên khỏi nội dung.
//
// Trả về danh sách đã khử trùng. KHÔNG kiểm tra id có thật hay không ở đây —
// việc đó cần truy vấn, và làm ở hàm gọi để gộp một lượt.
func extractMentions(content string) []uuid.UUID {
	matches := mentionPattern.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return []uuid.UUID{}
	}

	seen := make(map[uuid.UUID]struct{}, len(matches))
	out := make([]uuid.UUID, 0, len(matches))
	for _, m := range matches {
		id, err := uuid.Parse(m[1])
		if err != nil {
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

func (u *Usecase) ListComments(
	ctx context.Context,
	actor *domainauth.Actor,
	taskID uuid.UUID,
) ([]*domainproject.Comment, error) {
	if _, err := u.GetTask(ctx, actor, taskID); err != nil {
		return nil, err
	}
	list, err := u.comments.ListByTask(ctx, taskID)
	if err != nil {
		return nil, apperror.Internal(err)
	}
	return list, nil
}

func (u *Usecase) CreateComment(
	ctx context.Context,
	actor *domainauth.Actor,
	taskID uuid.UUID,
	content string,
) (*domainproject.Comment, error) {
	t, err := u.GetTask(ctx, actor, taskID)
	if err != nil {
		return nil, err
	}
	acc, err := u.loadAccess(ctx, actor, t.ProjectID)
	if err != nil {
		return nil, err
	}
	// Người chỉ có quyền xem thì không bình luận được. Cho phép sẽ biến
	// "viewer" thành một vai trò ghi được, khác với điều tên nó hứa hẹn.
	if !acc.canWrite() {
		return nil, apperror.Forbidden("Bạn chỉ có quyền xem dự án này")
	}

	content = strings.TrimSpace(content)
	if content == "" {
		return nil, apperror.Invalid("Nội dung bình luận không được để trống", nil)
	}
	if len(content) > maxCommentLength {
		return nil, apperror.Invalid("Bình luận quá dài", nil)
	}

	mentions, err := u.validMentions(ctx, t.ProjectID, extractMentions(content))
	if err != nil {
		return nil, err
	}

	c := &domainproject.Comment{
		TaskID:       taskID,
		AuthorID:     actor.EmployeeID,
		Content:      content,
		MentionedIDs: mentions,
	}
	if err := u.comments.Create(ctx, c); err != nil {
		return nil, apperror.Internal(err)
	}

	created, err := u.comments.GetByID(ctx, c.ID)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	u.logActivity(ctx, &domainproject.Activity{
		TaskID:  taskID,
		ActorID: actorEmployeeID(actor),
		Action:  domainproject.ActionCommented,
	})

	// Hai sự kiện tách riêng, có chủ ý:
	//   - người được @mention cần biết vì có người gọi đích danh họ
	//   - người thực hiện và người tạo task cần biết vì task của họ có thảo luận
	// Gộp làm một thì Phase 5 không phân biệt được hai loại thông báo, mà
	// mức độ khẩn của chúng khác hẳn nhau.
	if len(mentions) > 0 {
		u.publish(ctx, domainproject.Event{
			Name:       domainproject.JobTaskMentioned,
			TaskID:     t.ID,
			TaskCode:   t.Code(),
			TaskTitle:  t.Title,
			ProjectID:  t.ProjectID,
			ActorID:    actorID(actor),
			ActorName:  created.AuthorName,
			Recipients: dedupeExcept(mentions, actorID(actor)),
		})
	}

	return created, nil
}

func (u *Usecase) UpdateComment(
	ctx context.Context,
	actor *domainauth.Actor,
	commentID uuid.UUID,
	content string,
) (*domainproject.Comment, error) {
	c, err := u.comments.GetByID(ctx, commentID)
	if err != nil {
		return nil, apperror.NotFound("bình luận")
	}
	t, err := u.GetTask(ctx, actor, c.TaskID)
	if err != nil {
		return nil, apperror.NotFound("bình luận")
	}

	// Chỉ tác giả sửa được bình luận của mình — kể cả chủ dự án cũng không.
	// Sửa lời người khác rồi để nguyên tên họ là làm sai lệch hồ sơ thảo luận.
	if c.AuthorID != actor.EmployeeID {
		return nil, apperror.Forbidden("Chỉ người viết mới sửa được bình luận này")
	}

	content = strings.TrimSpace(content)
	if content == "" {
		return nil, apperror.Invalid("Nội dung bình luận không được để trống", nil)
	}
	if len(content) > maxCommentLength {
		return nil, apperror.Invalid("Bình luận quá dài", nil)
	}

	mentions, err := u.validMentions(ctx, t.ProjectID, extractMentions(content))
	if err != nil {
		return nil, err
	}

	c.Content = content
	c.MentionedIDs = mentions
	if err := u.comments.Update(ctx, c); err != nil {
		return nil, apperror.Internal(err)
	}
	return u.comments.GetByID(ctx, commentID)
}

func (u *Usecase) DeleteComment(
	ctx context.Context,
	actor *domainauth.Actor,
	commentID uuid.UUID,
) error {
	c, err := u.comments.GetByID(ctx, commentID)
	if err != nil {
		return apperror.NotFound("bình luận")
	}
	t, err := u.GetTask(ctx, actor, c.TaskID)
	if err != nil {
		return apperror.NotFound("bình luận")
	}
	acc, err := u.loadAccess(ctx, actor, t.ProjectID)
	if err != nil {
		return apperror.NotFound("bình luận")
	}

	// Tác giả xoá lời mình; chủ dự án xoá được nội dung không phù hợp.
	if c.AuthorID != actor.EmployeeID && !acc.canManage() {
		return apperror.Forbidden("Chỉ người viết hoặc chủ dự án mới xoá được bình luận này")
	}

	if err := u.comments.SoftDelete(ctx, commentID); err != nil {
		return apperror.Internal(err)
	}
	return nil
}

// validMentions lọc danh sách được nhắc tên, chỉ giữ THÀNH VIÊN DỰ ÁN.
//
// Nhắc tên người ngoài dự án là vô nghĩa: họ nhận thông báo rồi bấm vào chỉ
// thấy 404. Lọc im lặng thay vì báo lỗi — người viết không nên bị chặn gửi
// bình luận chỉ vì gõ nhầm một cái tên.
func (u *Usecase) validMentions(
	ctx context.Context,
	projectID uuid.UUID,
	ids []uuid.UUID,
) ([]uuid.UUID, error) {
	if len(ids) == 0 {
		return []uuid.UUID{}, nil
	}

	members, err := u.members.List(ctx, projectID)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	inProject := make(map[uuid.UUID]struct{}, len(members))
	for _, m := range members {
		inProject[m.EmployeeID] = struct{}{}
	}

	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if _, ok := inProject[id]; ok {
			out = append(out, id)
		}
	}
	return out, nil
}
