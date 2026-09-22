package project

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	domainproject "github.com/PhamVanPhuc2k2/manage/internal/domain/project"
)

func validInput(owner uuid.UUID) ProjectInput {
	return ProjectInput{
		Code:    "WEB",
		Name:    "Trang chủ mới",
		OwnerID: &owner,
	}
}

// =========================================================================
// TẠO VÀ SỬA DỰ ÁN
// =========================================================================

// TestCreateProjectAddsOwnerAsMember: chủ dự án phải tự động là thành viên.
//
// Thiếu bước này thì dự án không xuất hiện trong danh sách của chính người
// tạo ra nó — họ tạo xong và không thấy gì.
func TestCreateProjectAddsOwnerAsMember(t *testing.T) {
	h := newProjectHarness()
	owner := uuid.New()

	p, err := h.uc.CreateProject(context.Background(), actorIn(owner), validInput(owner))
	if err != nil {
		t.Fatalf("CreateProject lỗi: %v", err)
	}

	m, err := h.members.Get(context.Background(), p.ID, owner)
	if err != nil {
		t.Fatalf("chủ dự án không phải thành viên: %v", err)
	}
	if m.Role != domainproject.RoleOwner {
		t.Errorf("vai trò = %q, muốn %q", m.Role, domainproject.RoleOwner)
	}
}

// TestCreateProjectRejectsBadCode: mã dự án đi vào mã công việc hiển thị
// ("WEB-42") nên phải ngắn và không có khoảng trắng hay gạch ngang — gạch
// ngang sẽ làm mã công việc không phân tách được.
func TestCreateProjectRejectsBadCode(t *testing.T) {
	owner := uuid.New()

	cases := map[string]string{
		"rỗng":             "",
		"chỉ khoảng trắng": "   ",
		"có khoảng trắng":  "WEB MOI",
		"có gạch ngang":    "WEB-MOI",
		"có tab":           "WEB\tMOI",
		"dài hơn 20 ký tự": strings.Repeat("A", 21),
	}

	for name, code := range cases {
		t.Run(name, func(t *testing.T) {
			h := newProjectHarness()
			in := validInput(owner)
			in.Code = code

			_, err := h.uc.CreateProject(context.Background(), actorIn(owner), in)
			if got := statusOf(err); got != http.StatusBadRequest {
				t.Errorf("mã %q: mã lỗi = %d, muốn 400", code, got)
			}
		})
	}

	// Đúng 20 ký tự thì PHẢI được — nếu không, giới hạn lệch một ký tự.
	h := newProjectHarness()
	in := validInput(owner)
	in.Code = strings.Repeat("A", 20)
	if _, err := h.uc.CreateProject(context.Background(), actorIn(owner), in); err != nil {
		t.Errorf("mã đúng 20 ký tự phải được chấp nhận: %v", err)
	}
}

func TestCreateProjectRejectsDuplicateCode(t *testing.T) {
	h := newProjectHarness()
	owner := uuid.New()
	h.projects.codes["WEB"] = true

	_, err := h.uc.CreateProject(context.Background(), actorIn(owner), validInput(owner))
	if got := statusOf(err); got != http.StatusConflict {
		t.Errorf("mã lỗi = %d, muốn 409", got)
	}
}

// TestCreateProjectRejectsResignedOwner: giao dự án cho người đã nghỉ việc là
// lỗi nghiệp vụ, không phải lỗi dữ liệu.
func TestCreateProjectRejectsResignedOwner(t *testing.T) {
	h := newProjectHarness()
	owner := uuid.New()
	h.employees.missing[owner] = true

	_, err := h.uc.CreateProject(context.Background(), actorIn(owner), validInput(owner))
	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
}

// TestCreateProjectOwnerDefaults khoá lại phân biệt giữa "không gửi trường
// chủ dự án" và "gửi một id rỗng".
//
// Vắng mặt (nil) thì người tạo làm chủ — mặc định đúng trong phần lớn trường
// hợp và tránh được dự án không có chủ. Còn uuid toàn số 0 là dữ liệu HỎNG và
// phải bị từ chối: lặng lẽ thay nó bằng người tạo sẽ che mất một lỗi ở client.
func TestCreateProjectOwnerDefaults(t *testing.T) {
	t.Run("không gửi thì người tạo làm chủ", func(t *testing.T) {
		h := newProjectHarness()
		actor := uuid.New()

		in := validInput(actor)
		in.OwnerID = nil

		p, err := h.uc.CreateProject(context.Background(), actorIn(actor), in)
		if err != nil {
			t.Fatalf("CreateProject lỗi: %v", err)
		}
		if p.OwnerID != actor {
			t.Errorf("chủ dự án = %s, muốn người tạo %s", p.OwnerID, actor)
		}
	})

	t.Run("uuid rỗng bị từ chối", func(t *testing.T) {
		h := newProjectHarness()
		actor := uuid.New()

		in := validInput(actor)
		nilID := uuid.Nil
		in.OwnerID = &nilID

		_, err := h.uc.CreateProject(context.Background(), actorIn(actor), in)
		if got := statusOf(err); got != http.StatusBadRequest {
			t.Errorf("mã lỗi = %d, muốn 400", got)
		}
	})
}

// TestCreateProjectRejectsDueBeforeStart: hạn hoàn thành trước ngày bắt đầu
// là dữ liệu vô nghĩa, và mọi báo cáo tiến độ dựa trên nó sẽ ra số âm.
func TestCreateProjectRejectsDueBeforeStart(t *testing.T) {
	h := newProjectHarness()
	owner := uuid.New()

	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	due := start.AddDate(0, 0, -1)

	in := validInput(owner)
	in.StartDate = &start
	in.DueDate = &due

	_, err := h.uc.CreateProject(context.Background(), actorIn(owner), in)
	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
}

func TestUpdateProjectRequiresManageRole(t *testing.T) {
	h := newProjectHarness()
	owner, member := uuid.New(), uuid.New()
	p := h.seedProject(owner)
	h.members.join(p.ID, member, domainproject.RoleMember)
	h.employees.names[owner] = "Chủ"

	// Thành viên thường: được sửa công việc nhưng KHÔNG được sửa dự án.
	_, err := h.uc.UpdateProject(context.Background(), actorIn(member), p.ID, validInput(owner))
	if got := statusOf(err); got != http.StatusForbidden {
		t.Errorf("thành viên thường sửa dự án: mã lỗi = %d, muốn 403", got)
	}

	if _, err := h.uc.UpdateProject(
		context.Background(), actorIn(owner), p.ID, validInput(owner)); err != nil {
		t.Errorf("chủ dự án phải sửa được: %v", err)
	}
}

func TestDeleteProjectRequiresManageRole(t *testing.T) {
	h := newProjectHarness()
	owner, member := uuid.New(), uuid.New()
	p := h.seedProject(owner)
	h.members.join(p.ID, member, domainproject.RoleMember)

	if err := h.uc.DeleteProject(
		context.Background(), actorIn(member), p.ID); statusOf(err) != http.StatusForbidden {
		t.Errorf("mã lỗi = %d, muốn 403", statusOf(err))
	}

	if err := h.uc.DeleteProject(context.Background(), actorIn(owner), p.ID); err != nil {
		t.Errorf("chủ dự án phải xoá được: %v", err)
	}
	if len(h.projects.deleted) != 1 {
		t.Error("dự án chưa bị xoá mềm")
	}
}

// =========================================================================
// THÀNH VIÊN
// =========================================================================

func TestAddMemberUpsertsRole(t *testing.T) {
	h := newProjectHarness()
	owner, newbie := uuid.New(), uuid.New()
	p := h.seedProject(owner)

	if _, err := h.uc.AddMember(context.Background(), actorIn(owner), p.ID,
		newbie, domainproject.RoleMember); err != nil {
		t.Fatalf("AddMember lỗi: %v", err)
	}

	// Thêm LẠI cùng người với vai trò khác phải CẬP NHẬT, không báo lỗi trùng.
	//
	// Giao diện có hai nút riêng cho "thêm thành viên" và "đổi vai trò" nhưng
	// người dùng hay nhầm, và kết quả họ mong đợi trong cả hai trường hợp đều
	// là "người này có vai trò như tôi vừa chọn".
	if _, err := h.uc.AddMember(context.Background(), actorIn(owner), p.ID,
		newbie, domainproject.RoleViewer); err != nil {
		t.Fatalf("thêm lại phải cập nhật vai trò: %v", err)
	}

	m, err := h.members.Get(context.Background(), p.ID, newbie)
	if err != nil {
		t.Fatalf("không tìm thấy thành viên: %v", err)
	}
	if m.Role != domainproject.RoleViewer {
		t.Errorf("vai trò = %q, muốn %q", m.Role, domainproject.RoleViewer)
	}
}

func TestAddMemberRejectsResignedEmployee(t *testing.T) {
	h := newProjectHarness()
	owner, gone := uuid.New(), uuid.New()
	p := h.seedProject(owner)
	h.employees.missing[gone] = true

	_, err := h.uc.AddMember(context.Background(), actorIn(owner), p.ID,
		gone, domainproject.RoleMember)
	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
}

// TestCannotDemoteLastOwner: hạ vai trò chủ dự án cuối cùng sẽ để lại dự án
// VÔ CHỦ, và khi đó không ai quản lý thành viên được nữa — kể cả để tự trao
// lại quyền cho mình.
func TestCannotDemoteLastOwner(t *testing.T) {
	h := newProjectHarness()
	owner := uuid.New()
	p := h.seedProject(owner)

	_, err := h.uc.UpdateMemberRole(context.Background(), actorIn(owner), p.ID,
		owner, domainproject.RoleMember)
	if got := statusOf(err); got != http.StatusConflict {
		t.Errorf("mã lỗi = %d, muốn 409", got)
	}
}

// TestCanDemoteOwnerWhenAnotherExists: có chủ dự án thứ hai thì hạ được.
//
// Cặp phép thử này khoá lại ĐÚNG ranh giới: điều kiện là "chủ dự án CUỐI
// CÙNG", không phải "là chủ dự án".
func TestCanDemoteOwnerWhenAnotherExists(t *testing.T) {
	h := newProjectHarness()
	owner, coOwner := uuid.New(), uuid.New()
	p := h.seedProject(owner)
	h.members.join(p.ID, coOwner, domainproject.RoleOwner)

	if _, err := h.uc.UpdateMemberRole(context.Background(), actorIn(owner), p.ID,
		owner, domainproject.RoleMember); err != nil {
		t.Errorf("có chủ dự án thứ hai thì phải hạ được: %v", err)
	}
}

func TestCannotRemoveLastOwner(t *testing.T) {
	h := newProjectHarness()
	owner := uuid.New()
	p := h.seedProject(owner)

	err := h.uc.RemoveMember(context.Background(), actorIn(owner), p.ID, owner)
	if got := statusOf(err); got != http.StatusConflict {
		t.Errorf("mã lỗi = %d, muốn 409", got)
	}
}

// TestMemberCanRemoveSelf: ai cũng tự rời dự án được, không cần quyền quản lý.
func TestMemberCanRemoveSelf(t *testing.T) {
	h := newProjectHarness()
	owner, member := uuid.New(), uuid.New()
	p := h.seedProject(owner)
	h.members.join(p.ID, member, domainproject.RoleMember)

	if err := h.uc.RemoveMember(
		context.Background(), actorIn(member), p.ID, member); err != nil {
		t.Errorf("tự rời dự án phải được: %v", err)
	}
	if _, err := h.members.Get(context.Background(), p.ID, member); err == nil {
		t.Error("vẫn còn là thành viên sau khi rời")
	}
}

// TestMemberCannotRemoveOthers: gỡ NGƯỜI KHÁC thì cần quyền quản lý.
//
// Đây là cặp còn lại của phép thử trên: cùng một endpoint, hai kết quả khác
// nhau tuỳ vào người bị gỡ là ai. Middleware không phân biệt được điều đó.
func TestMemberCannotRemoveOthers(t *testing.T) {
	h := newProjectHarness()
	owner, member, victim := uuid.New(), uuid.New(), uuid.New()
	p := h.seedProject(owner)
	h.members.join(p.ID, member, domainproject.RoleMember)
	h.members.join(p.ID, victim, domainproject.RoleMember)

	err := h.uc.RemoveMember(context.Background(), actorIn(member), p.ID, victim)
	if got := statusOf(err); got != http.StatusForbidden {
		t.Errorf("mã lỗi = %d, muốn 403", got)
	}
}

// =========================================================================
// @MENTION
// =========================================================================

// TestExtractMentions kiểm tra bộ phân tích cú pháp @[tên](uuid).
func TestExtractMentions(t *testing.T) {
	a := uuid.New()
	b := uuid.New()

	cases := []struct {
		name    string
		content string
		want    int
	}{
		{"không có gì", "chỉ là văn bản thường", 0},
		{"một người", "nhờ @[Trần Văn A](" + a.String() + ") xem giúp", 1},
		{
			"hai người",
			"@[A](" + a.String() + ") và @[B](" + b.String() + ") cùng xem",
			2,
		},
		{
			// Khử trùng: nhắc một người hai lần trong cùng bình luận vẫn chỉ
			// là một người nhận, không phải hai thông báo.
			"cùng người hai lần",
			"@[A](" + a.String() + ") ơi, @[A](" + a.String() + ") xem nhé",
			1,
		},
		{"uuid sai định dạng", "@[A](khong-phai-uuid)", 0},
		{"thiếu ngoặc", "@[A]" + a.String(), 0},
		{"chỉ có @ thường", "email cua toi la a@b.com", 0},
		{
			// Tên chứa ký tự lạ vẫn phải bóc đúng id: tên do người dùng nhập.
			"tên có dấu và khoảng trắng",
			"@[Nguyễn Thị Bình An](" + a.String() + ")",
			1,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := extractMentions(c.content)
			if len(got) != c.want {
				t.Errorf("bóc được %d id, muốn %d (nội dung: %q)",
					len(got), c.want, c.content)
			}
		})
	}
}

// TestExtractMentionsNeverReturnsNil: trả về slice rỗng chứ không nil.
//
// Giá trị này đi thẳng vào JSON. nil thành `null`, còn slice rỗng thành `[]` —
// và client lặp qua `null` thì lỗi lúc chạy.
func TestExtractMentionsNeverReturnsNil(t *testing.T) {
	if got := extractMentions("không có ai được nhắc"); got == nil {
		t.Error("phải trả về slice rỗng, không phải nil")
	}
}

// TestCommentMentionsOnlyProjectMembers là phép thử BẢO MẬT.
//
// Nhắc tên một người ngoài dự án sẽ gửi cho họ thông báo kèm đoạn trích nội
// dung bình luận — tức là rò rỉ nội dung của một dự án họ không được xem.
func TestCommentMentionsOnlyProjectMembers(t *testing.T) {
	h := newProjectHarness()
	owner, member, outsider := uuid.New(), uuid.New(), uuid.New()
	p := h.seedProject(owner)
	h.members.join(p.ID, member, domainproject.RoleMember)
	h.employees.names[owner] = "Chủ dự án"

	task := h.column(p.ID, domainproject.TaskTodo, 1000)[0]

	content := "nhờ @[Thành viên](" + member.String() + ") và " +
		"@[Người ngoài](" + outsider.String() + ") xem giúp"

	c, err := h.uc.CreateComment(context.Background(), actorIn(owner), task.ID, content)
	if err != nil {
		t.Fatalf("CreateComment lỗi: %v", err)
	}

	if len(c.MentionedIDs) != 1 {
		t.Fatalf("lưu %d người được nhắc, muốn 1", len(c.MentionedIDs))
	}
	if c.MentionedIDs[0] != member {
		t.Error("người được nhắc phải là thành viên dự án")
	}

	// Và sự kiện @mention không được gửi tới người ngoài.
	for _, e := range h.events.published {
		if e.Name != domainproject.JobTaskMentioned {
			continue
		}
		for _, r := range e.Recipients {
			if r == outsider {
				t.Error("phát sự kiện @mention tới người ngoài dự án")
			}
		}
	}
}

func TestCommentRejectsEmptyContent(t *testing.T) {
	h := newProjectHarness()
	owner := uuid.New()
	p := h.seedProject(owner)
	task := h.column(p.ID, domainproject.TaskTodo, 1000)[0]

	for _, content := range []string{"", "   ", "\n\t"} {
		_, err := h.uc.CreateComment(
			context.Background(), actorIn(owner), task.ID, content)
		if got := statusOf(err); got != http.StatusBadRequest {
			t.Errorf("nội dung %q: mã lỗi = %d, muốn 400", content, got)
		}
	}
}

// TestUpdateCommentOnlyByAuthor: sửa lời người khác rồi để nguyên tên họ là
// làm sai lệch hồ sơ thảo luận. Kể cả chủ dự án cũng không được.
func TestUpdateCommentOnlyByAuthor(t *testing.T) {
	h := newProjectHarness()
	owner, member := uuid.New(), uuid.New()
	p := h.seedProject(owner)
	h.members.join(p.ID, member, domainproject.RoleMember)
	h.employees.names[member] = "Thành viên"

	task := h.column(p.ID, domainproject.TaskTodo, 1000)[0]

	c, err := h.uc.CreateComment(
		context.Background(), actorIn(member), task.ID, "bình luận của tôi")
	if err != nil {
		t.Fatalf("CreateComment lỗi: %v", err)
	}

	// Chủ dự án KHÔNG sửa được bình luận của người khác.
	_, err = h.uc.UpdateComment(context.Background(), actorIn(owner), c.ID, "sửa trộm")
	if got := statusOf(err); got != http.StatusForbidden {
		t.Errorf("chủ dự án sửa bình luận người khác: mã lỗi = %d, muốn 403", got)
	}

	// Chính tác giả thì được.
	if _, err := h.uc.UpdateComment(
		context.Background(), actorIn(member), c.ID, "tôi tự sửa"); err != nil {
		t.Errorf("tác giả phải sửa được: %v", err)
	}
}

// TestCommentNotFoundForOutsider: người ngoài dự án nhận 404 khi chạm vào một
// bình luận, không phải 403 — cùng nguyên tắc với dự án và công việc.
func TestCommentNotFoundForOutsider(t *testing.T) {
	h := newProjectHarness()
	owner := uuid.New()
	p := h.seedProject(owner)
	h.employees.names[owner] = "Chủ"

	task := h.column(p.ID, domainproject.TaskTodo, 1000)[0]
	c, err := h.uc.CreateComment(
		context.Background(), actorIn(owner), task.ID, "nội dung nội bộ")
	if err != nil {
		t.Fatalf("CreateComment lỗi: %v", err)
	}

	outsider := actorIn(uuid.New())

	if _, err := h.uc.UpdateComment(
		context.Background(), outsider, c.ID, "sửa"); statusOf(err) != http.StatusNotFound {
		t.Errorf("sửa: mã lỗi = %d, muốn 404", statusOf(err))
	}
	if err := h.uc.DeleteComment(
		context.Background(), outsider, c.ID); statusOf(err) != http.StatusNotFound {
		t.Errorf("xoá: mã lỗi = %d, muốn 404", statusOf(err))
	}
}
