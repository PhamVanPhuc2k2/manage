package chat

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	domainchat "github.com/PhamVanPhuc2k2/manage/internal/domain/chat"
)

// =========================================================================
// HỘI THOẠI 1-1
// =========================================================================

// TestOpenDirectReusesExisting là điểm mấu chốt của chat 1-1.
//
// Hai người bấm "nhắn tin" cho nhau ở hai thời điểm khác nhau phải rơi vào
// CÙNG một hội thoại, nếu không mỗi người thấy một nửa lịch sử và không ai
// hiểu tin nhắn kia đi đâu.
func TestOpenDirectReusesExisting(t *testing.T) {
	h := newChatHarness()

	me, peer := uuid.New(), uuid.New()
	existing := &domainchat.Conversation{
		ID:        uuid.New(),
		Kind:      domainchat.KindDirect,
		DirectKey: domainchat.DirectKeyFor(me, peer),
	}
	h.convs.put(existing)
	h.convs.byDirect = func(key string) *domainchat.Conversation {
		if key == existing.DirectKey {
			return existing
		}
		return nil
	}

	got, err := h.uc.OpenDirect(context.Background(), actorOf(me), peer)
	if err != nil {
		t.Fatalf("OpenDirect lỗi: %v", err)
	}
	if got.ID != existing.ID {
		t.Errorf("trả hội thoại %s, muốn %s", got.ID, existing.ID)
	}
	if len(h.convs.created) != 0 {
		t.Error("không được tạo hội thoại mới khi đã có")
	}
}

// TestOpenDirectIsSymmetric: chiều ngược lại phải ra cùng một khoá.
func TestOpenDirectIsSymmetric(t *testing.T) {
	h := newChatHarness()

	me, peer := uuid.New(), uuid.New()

	// Lần đầu: chưa có gì, tạo mới.
	first, err := h.uc.OpenDirect(context.Background(), actorOf(me), peer)
	if err != nil {
		t.Fatalf("OpenDirect lỗi: %v", err)
	}

	// Cho phép tra theo khoá từ lần này.
	created := h.convs.created[0]
	h.convs.byDirect = func(key string) *domainchat.Conversation {
		if key == created.DirectKey {
			return created
		}
		return nil
	}

	// Người kia mở theo chiều ngược lại.
	second, err := h.uc.OpenDirect(context.Background(), actorOf(peer), me)
	if err != nil {
		t.Fatalf("OpenDirect chiều ngược lỗi: %v", err)
	}

	if first.ID != second.ID {
		t.Errorf("hai chiều ra hai hội thoại: %s vs %s", first.ID, second.ID)
	}
	if len(h.convs.created) != 1 {
		t.Errorf("tạo %d hội thoại, muốn 1", len(h.convs.created))
	}
}

// TestOpenDirectAddsBothMembers: thiếu bước này thì hội thoại tồn tại nhưng
// không ai nhìn thấy nó, và cũng không tạo mới được vì direct_key đã bị chiếm.
func TestOpenDirectAddsBothMembers(t *testing.T) {
	h := newChatHarness()

	me, peer := uuid.New(), uuid.New()
	if _, err := h.uc.OpenDirect(context.Background(), actorOf(me), peer); err != nil {
		t.Fatalf("OpenDirect lỗi: %v", err)
	}

	convID := h.convs.created[0].ID
	if len(h.convs.members[convID]) != 2 {
		t.Errorf("hội thoại có %d thành viên, muốn 2", len(h.convs.members[convID]))
	}
}

func TestOpenDirectRejectsSelf(t *testing.T) {
	h := newChatHarness()

	me := uuid.New()
	_, err := h.uc.OpenDirect(context.Background(), actorOf(me), me)
	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
}

// TestOpenDirectRejectsOutsider chặn mở hội thoại với người ngoài công ty.
func TestOpenDirectRejectsOutsider(t *testing.T) {
	h := newChatHarness()
	h.lookup.sameCompany = false

	_, err := h.uc.OpenDirect(context.Background(), actorOf(uuid.New()), uuid.New())
	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
}

// =========================================================================
// NHÓM
// =========================================================================

func TestCreateGroupMakesCreatorAdmin(t *testing.T) {
	h := newChatHarness()

	me, other := uuid.New(), uuid.New()
	h.lookup.names[me] = "Người tạo"

	c, err := h.uc.CreateGroup(context.Background(), actorOf(me), CreateGroupInput{
		Name:      "Nhóm kiểm thử",
		MemberIDs: []uuid.UUID{other},
	})
	if err != nil {
		t.Fatalf("CreateGroup lỗi: %v", err)
	}

	if !c.IsAdmin {
		t.Error("người tạo nhóm phải là quản trị nhóm")
	}

	// Những người còn lại thì KHÔNG. Gộp một lượt thêm thành viên sẽ khiến
	// mọi người cùng thành quản trị, vì cờ is_admin là tham số chung cho cả lượt.
	if m := h.convs.members[c.ID][other]; m == nil || m.IsAdmin {
		t.Error("thành viên được thêm không được tự động thành quản trị")
	}
}

func TestCreateGroupRejectsBlankName(t *testing.T) {
	h := newChatHarness()

	for _, name := range []string{"", "   "} {
		_, err := h.uc.CreateGroup(context.Background(), actorOf(uuid.New()),
			CreateGroupInput{Name: name})
		if got := statusOf(err); got != http.StatusBadRequest {
			t.Errorf("tên %q: mã lỗi = %d, muốn 400", name, got)
		}
	}
}

func TestCreateGroupRejectsTooManyMembers(t *testing.T) {
	h := newChatHarness()

	ids := make([]uuid.UUID, maxGroupMembers+1)
	for i := range ids {
		ids[i] = uuid.New()
	}

	_, err := h.uc.CreateGroup(context.Background(), actorOf(uuid.New()),
		CreateGroupInput{Name: "Nhóm khổng lồ", MemberIDs: ids})
	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
}

func TestCreateGroupWritesSystemMessage(t *testing.T) {
	h := newChatHarness()

	me := uuid.New()
	h.lookup.names[me] = "Trần Văn A"

	if _, err := h.uc.CreateGroup(context.Background(), actorOf(me),
		CreateGroupInput{Name: "Nhóm mới"}); err != nil {
		t.Fatalf("CreateGroup lỗi: %v", err)
	}

	if len(h.msgs.created) != 1 {
		t.Fatalf("ghi %d tin hệ thống, muốn 1", len(h.msgs.created))
	}
	m := h.msgs.created[0]
	if m.Kind != domainchat.MessageSystem {
		t.Errorf("loại tin = %q, muốn %q", m.Kind, domainchat.MessageSystem)
	}
	if m.SenderID != nil {
		t.Error("tin hệ thống không được có người gửi")
	}
}

// TestManagedGroupRejectsMemberEdits: nhóm phòng ban và dự án lấy thành viên
// từ chính phòng ban / dự án đó. Cho sửa tay sẽ tạo ra hai nguồn sự thật, và
// người vừa chuyển phòng vẫn đọc được tin nhắn của phòng cũ.
func TestManagedGroupRejectsMemberEdits(t *testing.T) {
	for _, kind := range []domainchat.Kind{
		domainchat.KindDepartment, domainchat.KindProject,
	} {
		t.Run(string(kind), func(t *testing.T) {
			h := newChatHarness()

			convID := uuid.New()
			admin := uuid.New()
			h.convs.put(&domainchat.Conversation{
				ID: convID, Kind: kind, Name: "Nhóm tự động",
			})
			h.convs.join(convID, admin, true)

			a := actorOf(admin)

			if err := h.uc.AddMembers(
				context.Background(), a, convID, []uuid.UUID{uuid.New()}); statusOf(err) != http.StatusBadRequest {
				t.Errorf("thêm thành viên: mã lỗi = %d, muốn 400", statusOf(err))
			}
			if err := h.uc.RemoveMember(
				context.Background(), a, convID, uuid.New()); statusOf(err) != http.StatusBadRequest {
				t.Errorf("gỡ thành viên: mã lỗi = %d, muốn 400", statusOf(err))
			}
			if _, err := h.uc.Rename(
				context.Background(), a, convID, "Tên mới"); statusOf(err) != http.StatusBadRequest {
				t.Errorf("đổi tên: mã lỗi = %d, muốn 400", statusOf(err))
			}
			if err := h.uc.Leave(
				context.Background(), a, convID); statusOf(err) != http.StatusBadRequest {
				t.Errorf("rời nhóm: mã lỗi = %d, muốn 400", statusOf(err))
			}
		})
	}
}

func TestOrdinaryMemberCannotManageGroup(t *testing.T) {
	h := newChatHarness()

	convID := uuid.New()
	member := uuid.New()
	h.convs.put(&domainchat.Conversation{
		ID: convID, Kind: domainchat.KindGroup, Name: "Nhóm",
	})
	h.convs.join(convID, member, false)

	a := actorOf(member)

	if _, err := h.uc.Rename(context.Background(), a, convID, "Đổi trộm"); statusOf(err) != http.StatusForbidden {
		t.Errorf("đổi tên: mã lỗi = %d, muốn 403", statusOf(err))
	}
	if err := h.uc.AddMembers(
		context.Background(), a, convID, []uuid.UUID{uuid.New()}); statusOf(err) != http.StatusForbidden {
		t.Errorf("thêm thành viên: mã lỗi = %d, muốn 403", statusOf(err))
	}
}

// TestAdminCannotDemoteSelf: tự bỏ quyền có thể để lại nhóm không còn quản
// trị viên nào, và khi đó không ai thêm/gỡ được ai nữa.
func TestAdminCannotDemoteSelf(t *testing.T) {
	h := newChatHarness()

	convID := uuid.New()
	admin := uuid.New()
	h.convs.put(&domainchat.Conversation{
		ID: convID, Kind: domainchat.KindGroup, Name: "Nhóm",
	})
	h.convs.join(convID, admin, true)

	err := h.uc.SetAdmin(context.Background(), actorOf(admin), convID, admin, false)
	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}

	// Nhưng trao quyền cho người khác thì được.
	other := uuid.New()
	h.convs.join(convID, other, false)
	if err := h.uc.SetAdmin(
		context.Background(), actorOf(admin), convID, other, true); err != nil {
		t.Errorf("trao quyền cho người khác phải được: %v", err)
	}
}

// TestRemoveMemberUsesLeaveForSelf: gỡ chính mình phải đi qua chức năng rời
// nhóm, vì đó là hai thao tác có ý nghĩa khác nhau với người dùng.
func TestRemoveMemberUsesLeaveForSelf(t *testing.T) {
	h := newChatHarness()

	convID := uuid.New()
	admin := uuid.New()
	h.convs.put(&domainchat.Conversation{
		ID: convID, Kind: domainchat.KindGroup, Name: "Nhóm",
	})
	h.convs.join(convID, admin, true)

	err := h.uc.RemoveMember(context.Background(), actorOf(admin), convID, admin)
	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
}

func TestLeaveRejectedForDirect(t *testing.T) {
	h := newChatHarness()

	convID := uuid.New()
	me := uuid.New()
	h.convs.put(&domainchat.Conversation{ID: convID, Kind: domainchat.KindDirect})
	h.convs.join(convID, me, false)

	err := h.uc.Leave(context.Background(), actorOf(me), convID)
	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
}

// TestLeaveAnnouncesBeforeRemoving: sau khi rời thì người này không còn trong
// danh sách người nhận, và chính họ sẽ không thấy dòng thông báo mình vừa rời.
func TestLeaveAnnouncesBeforeRemoving(t *testing.T) {
	h := newChatHarness()

	convID := uuid.New()
	me, other := uuid.New(), uuid.New()
	h.convs.put(&domainchat.Conversation{
		ID: convID, Kind: domainchat.KindGroup, Name: "Nhóm",
	})
	h.convs.join(convID, me, false)
	h.convs.join(convID, other, true)
	h.lookup.names[me] = "Người rời"

	if err := h.uc.Leave(context.Background(), actorOf(me), convID); err != nil {
		t.Fatalf("Leave lỗi: %v", err)
	}

	if len(h.pusher.to) == 0 {
		t.Fatal("không phát bản tin nào")
	}
	// Bản tin đầu tiên (thông báo rời nhóm) phải tới cả hai người.
	if len(h.pusher.to[0]) != 2 {
		t.Errorf("thông báo rời nhóm tới %d người, muốn 2 (gồm cả người rời)",
			len(h.pusher.to[0]))
	}
	if h.convs.members[convID][me] != nil {
		t.Error("sau khi rời thì không còn là thành viên")
	}
}

// =========================================================================
// GHIM VÀ TẮT THÔNG BÁO
// =========================================================================

// TestFlagsWorkOnManagedGroups: ghim và tắt thông báo là tuỳ chọn RIÊNG của
// người xem, nên dùng được cả với nhóm do hệ thống quản lý.
func TestFlagsWorkOnManagedGroups(t *testing.T) {
	h := newChatHarness()

	convID := uuid.New()
	me := uuid.New()
	h.convs.put(&domainchat.Conversation{
		ID: convID, Kind: domainchat.KindDepartment, Name: "Phòng: Kỹ thuật",
	})
	h.convs.join(convID, me, false)

	a := actorOf(me)
	if err := h.uc.SetPinned(context.Background(), a, convID, true); err != nil {
		t.Errorf("ghim nhóm tự động phải được: %v", err)
	}
	if err := h.uc.SetMuted(context.Background(), a, convID, true); err != nil {
		t.Errorf("tắt thông báo nhóm tự động phải được: %v", err)
	}

	m := h.convs.members[convID][me]
	if !m.IsPinned || !m.IsMuted {
		t.Error("cờ ghim/tắt chưa được lưu")
	}
}

func TestFlagsRejectedForNonMember(t *testing.T) {
	h := newChatHarness()

	convID := uuid.New()
	h.convs.put(&domainchat.Conversation{
		ID: convID, Kind: domainchat.KindGroup, Name: "Nhóm",
	})

	err := h.uc.SetPinned(context.Background(), actorOf(uuid.New()), convID, true)
	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
}
