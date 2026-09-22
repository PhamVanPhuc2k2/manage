package chat

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	domainchat "github.com/PhamVanPhuc2k2/manage/internal/domain/chat"
)

// Bộ kiểm thử danh sách hội thoại và màn hình chi tiết.
//
// Điểm chung của cả hai: chúng hiển thị CHẤM MÀU trạng thái online, và
// thông tin đó đến từ Redis. Redis là thành phần được phép hỏng — mất nó
// thì chat chậm chứ không sai — nên mọi chỗ đọc presence phải suy giảm êm
// thay vì làm hỏng cả màn hình.

// seedGroup dựng một nhóm có sẵn thành viên, trả về hội thoại.
func seedGroup(h *chatHarness, name string, members ...uuid.UUID) *domainchat.Conversation {
	c := &domainchat.Conversation{
		ID:          uuid.New(),
		CompanyID:   h.company.id,
		Kind:        domainchat.KindGroup,
		Name:        name,
		DisplayName: name,
	}
	h.convs.put(c)
	for i, m := range members {
		h.convs.join(c.ID, m, i == 0)
	}
	return c
}

// seedDirect dựng một hội thoại 1-1 giữa hai người.
func seedDirect(h *chatHarness, a, b uuid.UUID) *domainchat.Conversation {
	peer := b
	c := &domainchat.Conversation{
		ID:          uuid.New(),
		CompanyID:   h.company.id,
		Kind:        domainchat.KindDirect,
		DirectKey:   domainchat.DirectKeyFor(a, b),
		DisplayName: "Đồng nghiệp",
		PeerID:      &peer,
	}
	h.convs.put(c)
	h.convs.join(c.ID, a, false)
	h.convs.join(c.ID, b, false)
	return c
}

// =========================================================================
// DANH SÁCH HỘI THOẠI
// =========================================================================

// TestListReturnsOnlyOwnConversations.
//
// Danh sách hội thoại là nơi lộ dữ liệu dễ nhất: nó gồm cả tên nhóm và tin
// nhắn cuối. Người ngoài không được thấy dù chỉ một dòng.
func TestListReturnsOnlyOwnConversations(t *testing.T) {
	me, other := uuid.New(), uuid.New()

	h := newChatHarness()
	seedGroup(h, "Nhóm của tôi", me, other)
	seedGroup(h, "Nhóm không liên quan", other)

	got, err := h.uc.List(context.Background(), actorOf(me), "")
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("số hội thoại = %d, muốn 1", len(got))
	}
	if got[0].DisplayName != "Nhóm của tôi" {
		t.Errorf("hội thoại = %q, muốn %q", got[0].DisplayName, "Nhóm của tôi")
	}
}

func TestListFiltersBySearch(t *testing.T) {
	me := uuid.New()

	h := newChatHarness()
	seedGroup(h, "Phòng Kỹ thuật", me)
	seedGroup(h, "Dự án Alpha", me)

	got, err := h.uc.List(context.Background(), actorOf(me), "kỹ thuật")
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("số kết quả = %d, muốn 1", len(got))
	}
}

// TestListFillsPeerStatus: chấm màu bên cạnh tên người trong danh sách chat
// 1-1 đến từ presence, không phải từ database.
func TestListFillsPeerStatus(t *testing.T) {
	me, peer := uuid.New(), uuid.New()

	h := newChatHarness()
	seedDirect(h, me, peer)
	h.presence.status = map[uuid.UUID]string{peer: "online"}

	got, err := h.uc.List(context.Background(), actorOf(me), "")
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("số hội thoại = %d, muốn 1", len(got))
	}
	if got[0].PeerStatus != "online" {
		t.Errorf("trạng thái = %q, muốn online", got[0].PeerStatus)
	}
}

// TestListWithoutPresenceStillWorks.
//
// Mất Redis thì mọi người hiện offline, và đó vẫn là một màn hình dùng
// được. Làm hỏng cả danh sách hội thoại vì không đọc được chấm xanh là đánh
// đổi sai — người dùng vào đây để nhắn tin, không phải để xem ai online.
func TestListWithoutPresenceStillWorks(t *testing.T) {
	me, peer := uuid.New(), uuid.New()

	h := newChatHarness()
	seedDirect(h, me, peer)
	h.uc.presence = nil

	got, err := h.uc.List(context.Background(), actorOf(me), "")
	if err != nil {
		t.Fatalf("mất presence không được làm hỏng danh sách: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("số hội thoại = %d, muốn 1", len(got))
	}
	if got[0].PeerStatus != "offline" {
		t.Errorf("trạng thái = %q, muốn offline", got[0].PeerStatus)
	}
}

// TestStatusOrOffline: ô trạng thái rỗng trông như lỗi giao diện.
func TestStatusOrOffline(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", "offline"},
		{"online", "online"},
		{"idle", "idle"},
		{"offline", "offline"},
	}

	for _, tc := range cases {
		if got := statusOrOffline(tc.in); got != tc.want {
			t.Errorf("statusOrOffline(%q) = %q, muốn %q", tc.in, got, tc.want)
		}
	}
}

// =========================================================================
// CHI TIẾT HỘI THOẠI
// =========================================================================

func TestGetConversationWithMembers(t *testing.T) {
	me, other := uuid.New(), uuid.New()

	h := newChatHarness()
	c := seedGroup(h, "Nhóm", me, other)
	h.presence.status = map[uuid.UUID]string{me: "online"}

	conv, members, err := h.uc.Get(context.Background(), actorOf(me), c.ID)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if conv.ID != c.ID {
		t.Errorf("hội thoại = %v, muốn %v", conv.ID, c.ID)
	}
	if len(members) != 2 {
		t.Fatalf("số thành viên = %d, muốn 2", len(members))
	}

	for _, m := range members {
		if m.Status == "" {
			t.Errorf("thành viên %v không có trạng thái", m.EmployeeID)
		}
	}
}

// TestGetConversationYouAreNotInReturns404.
//
// 404 chứ không 403: xác nhận "nhóm này có tồn tại nhưng bạn không ở trong
// đó" đã là rò rỉ — nó cho biết tên nhóm đoán được là có thật.
func TestGetConversationYouAreNotInReturns404(t *testing.T) {
	me, other := uuid.New(), uuid.New()

	h := newChatHarness()
	c := seedGroup(h, "Nhóm kín", other)

	_, _, err := h.uc.Get(context.Background(), actorOf(me), c.ID)
	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
}

func TestGetMissingConversationReturns404(t *testing.T) {
	h := newChatHarness()

	_, _, err := h.uc.Get(
		context.Background(), actorOf(uuid.New()), uuid.New())

	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
}

// TestGetDirectFillsPeerStatus: màn hình chat 1-1 cũng cần chấm màu của
// người đối diện, không chỉ danh sách.
func TestGetDirectFillsPeerStatus(t *testing.T) {
	me, peer := uuid.New(), uuid.New()

	h := newChatHarness()
	c := seedDirect(h, me, peer)
	h.presence.status = map[uuid.UUID]string{peer: "idle"}

	conv, _, err := h.uc.Get(context.Background(), actorOf(me), c.ID)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if conv.PeerStatus != "idle" {
		t.Errorf("trạng thái = %q, muốn idle", conv.PeerStatus)
	}
}

// =========================================================================
// HUY HIỆU CHƯA ĐỌC
// =========================================================================

func TestTotalUnread(t *testing.T) {
	h := newChatHarness()
	h.convs.unread = 7

	got, err := h.uc.TotalUnread(context.Background(), actorOf(uuid.New()))
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if got != 7 {
		t.Errorf("tổng chưa đọc = %d, muốn 7", got)
	}
}

// =========================================================================
// TIỆN ÍCH
// =========================================================================

// TestNotFoundOrHidesMembershipErrors.
//
// Cả "không tồn tại" lẫn "không phải thành viên" đều phải ra 404. Phân biệt
// được hai lỗi này là phân biệt được nhóm có thật với nhóm không có.
func TestNotFoundOrHidesMembershipErrors(t *testing.T) {
	for _, err := range []error{domainchat.ErrNotFound, domainchat.ErrNotMember} {
		if got := statusOf(notFoundOr(err)); got != http.StatusNotFound {
			t.Errorf("%v → mã lỗi %d, muốn 404", err, got)
		}
	}
}

// TestJoinNamesSkipsUnknownIds: tin nhắn hệ thống dựng câu từ hàm này, và
// một id không tra được tên sẽ thành chuỗi rỗng giữa hai dấu phẩy.
func TestJoinNames(t *testing.T) {
	a, b, ghost := uuid.New(), uuid.New(), uuid.New()
	names := map[uuid.UUID]string{a: "Nguyễn Văn A", b: "Trần Thị B"}

	cases := []struct {
		name string
		ids  []uuid.UUID
		want string
	}{
		{"một người", []uuid.UUID{a}, "Nguyễn Văn A"},
		{"hai người", []uuid.UUID{a, b}, "Nguyễn Văn A, Trần Thị B"},
		{"bỏ qua id không tra được tên", []uuid.UUID{a, ghost, b},
			"Nguyễn Văn A, Trần Thị B"},
		{"danh sách rỗng", nil, ""},
		{"toàn id lạ", []uuid.UUID{ghost}, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := joinNames(names, tc.ids); got != tc.want {
				t.Errorf("= %q, muốn %q", got, tc.want)
			}
		})
	}
}
