package chat

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	domainchat "github.com/PhamVanPhuc2k2/manage/internal/domain/chat"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
)

func actorOf(id uuid.UUID) *domainauth.Actor {
	return &domainauth.Actor{
		EmployeeID:  id,
		Permissions: map[string]struct{}{domainauth.PermChatRead: {}, domainauth.PermChatCreate: {}},
		Scope:       domainauth.ScopeAll,
	}
}

// statusOf trả mã HTTP mà một lỗi nghiệp vụ sẽ biến thành.
//
// Kiểm tra mã HTTP chứ không kiểm loại lỗi cụ thể: điều quan trọng với người
// gọi API là họ nhận 404 hay 403, và đó cũng chính là thứ dễ hồi quy nhất.
func statusOf(err error) int {
	code, _ := apperror.HTTPStatus(err)
	return code
}

// =========================================================================
// KIỂM SOÁT TRUY CẬP
// =========================================================================

// TestNonMemberGets404 khoá lại quyết định bảo mật quan trọng nhất của module.
//
// 403 xác nhận rằng hội thoại đó CÓ TỒN TẠI, và với chat thì chính sự tồn tại
// của một cuộc trò chuyện đã là thông tin không nên rò ra.
func TestNonMemberGets404(t *testing.T) {
	h := newChatHarness()

	convID := uuid.New()
	h.convs.put(&domainchat.Conversation{ID: convID, Kind: domainchat.KindGroup, Name: "Nhóm kín"})
	h.convs.join(convID, uuid.New(), true) // một người khác là thành viên

	outsider := actorOf(uuid.New())

	t.Run("gửi tin", func(t *testing.T) {
		_, err := h.uc.Send(context.Background(), outsider, SendInput{
			ConversationID: convID, Content: "lẻn vào",
		})
		if got := statusOf(err); got != http.StatusNotFound {
			t.Errorf("mã lỗi = %d, muốn 404", got)
		}
	})

	t.Run("đọc lịch sử", func(t *testing.T) {
		_, err := h.uc.History(context.Background(), outsider, convID, nil, "", 0)
		if got := statusOf(err); got != http.StatusNotFound {
			t.Errorf("mã lỗi = %d, muốn 404", got)
		}
	})

	t.Run("báo đã đọc", func(t *testing.T) {
		_, err := h.uc.MarkRead(context.Background(), outsider, convID, uuid.New())
		if got := statusOf(err); got != http.StatusNotFound {
			t.Errorf("mã lỗi = %d, muốn 404", got)
		}
	})

	t.Run("chỉ báo đang nhập", func(t *testing.T) {
		err := h.uc.Typing(context.Background(), outsider, convID)
		if got := statusOf(err); got != http.StatusNotFound {
			t.Errorf("mã lỗi = %d, muốn 404", got)
		}
	})

	t.Run("xin URL tải tệp", func(t *testing.T) {
		_, _, err := h.uc.PresignUpload(
			context.Background(), outsider, convID, "a.png", "image/png", 100)
		if got := statusOf(err); got != http.StatusNotFound {
			t.Errorf("mã lỗi = %d, muốn 404", got)
		}
	})
}

// =========================================================================
// GỬI TIN
// =========================================================================

func sendSetup() (*chatHarness, uuid.UUID, *domainauth.Actor) {
	h := newChatHarness()
	convID := uuid.New()
	me := uuid.New()

	h.convs.put(&domainchat.Conversation{
		ID: convID, Kind: domainchat.KindDirect,
	})
	h.convs.join(convID, me, false)
	h.lookup.names[me] = "Tôi"

	return h, convID, actorOf(me)
}

func TestSendRejectsEmptyContent(t *testing.T) {
	h, convID, me := sendSetup()

	for _, content := range []string{"", "   ", "\n\t "} {
		_, err := h.uc.Send(context.Background(), me, SendInput{
			ConversationID: convID, Content: content,
		})
		if got := statusOf(err); got != http.StatusBadRequest {
			t.Errorf("nội dung %q: mã lỗi = %d, muốn 400", content, got)
		}
	}
	if len(h.msgs.created) != 0 {
		t.Error("không được ghi tin rỗng")
	}
}

func TestSendRejectsTooLongContent(t *testing.T) {
	h, convID, me := sendSetup()

	_, err := h.uc.Send(context.Background(), me, SendInput{
		ConversationID: convID,
		Content:        strings.Repeat("a", domainchat.MaxMessageLength+1),
	})
	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}

	// Đúng ngưỡng thì PHẢI gửi được — nếu không, giới hạn lệch một ký tự.
	if _, err := h.uc.Send(context.Background(), me, SendInput{
		ConversationID: convID,
		Content:        strings.Repeat("a", domainchat.MaxMessageLength),
	}); err != nil {
		t.Errorf("tin dài đúng ngưỡng phải gửi được: %v", err)
	}
}

// TestSendCountsRunesNotBytes: giới hạn tính theo KÝ TỰ, không theo byte.
//
// Tiếng Việt mỗi ký tự có dấu chiếm 2–3 byte, nên đếm byte sẽ cắt tin nhắn
// tiếng Việt ở khoảng một phần ba độ dài cho phép.
func TestSendCountsRunesNotBytes(t *testing.T) {
	h, convID, me := sendSetup()

	// 2000 ký tự "ằ" — vượt 4000 byte nhưng chưa tới 4000 ký tự.
	content := strings.Repeat("ằ", 2000)
	if len(content) <= domainchat.MaxMessageLength {
		t.Fatalf("mốc kiểm thử sai: chuỗi phải dài hơn %d byte", domainchat.MaxMessageLength)
	}

	if _, err := h.uc.Send(context.Background(), me, SendInput{
		ConversationID: convID, Content: content,
	}); err != nil {
		t.Errorf("tin tiếng Việt 2000 ký tự phải gửi được: %v", err)
	}
}

// TestSendRejectsSystemKind: cho client gửi loại này là để nó giả được những
// dòng "X đã rời nhóm" mà không ai phân biệt nổi.
func TestSendRejectsSystemKind(t *testing.T) {
	h, convID, me := sendSetup()

	_, err := h.uc.Send(context.Background(), me, SendInput{
		ConversationID: convID,
		Content:        "Ai đó đã rời nhóm",
		Kind:           domainchat.MessageSystem,
	})
	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
	if len(h.msgs.created) != 0 {
		t.Error("không được ghi tin hệ thống do client gửi")
	}
}

func TestSendRejectsUnknownKind(t *testing.T) {
	h, convID, me := sendSetup()

	_, err := h.uc.Send(context.Background(), me, SendInput{
		ConversationID: convID, Content: "x", Kind: domainchat.MessageKind("sticker"),
	})
	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
}

// TestSendDeduplicatesByClientID là phép thử chống tin nhắn đôi.
//
// Mất mạng giữa chừng, client không biết tin đã tới hay chưa nên gửi lại.
// Phải trả về ĐÚNG tin cũ, không ghi thêm bản sao — người nhận thấy tin đôi
// là lỗi người dùng nhớ rất lâu.
func TestSendDeduplicatesByClientID(t *testing.T) {
	h, convID, me := sendSetup()

	in := SendInput{
		ConversationID:  convID,
		Content:         "gửi một lần",
		ClientMessageID: "cid-1",
	}

	first, err := h.uc.Send(context.Background(), me, in)
	if err != nil {
		t.Fatalf("lần gửi đầu lỗi: %v", err)
	}

	second, err := h.uc.Send(context.Background(), me, in)
	if err != nil {
		t.Fatalf("lần gửi lại lỗi: %v", err)
	}

	if first.ID != second.ID {
		t.Errorf("gửi lại tạo tin mới: %s vs %s", first.ID, second.ID)
	}
	if len(h.msgs.created) != 1 {
		t.Errorf("ghi %d tin, muốn 1", len(h.msgs.created))
	}
}

// TestSendWithoutClientIDDoesNotDeduplicate: không có mã thì hai lần gửi là
// hai tin thật. Gộp chúng lại sẽ làm mất tin khi ai đó nhắn cùng một câu hai
// lần một cách có ý.
func TestSendWithoutClientIDDoesNotDeduplicate(t *testing.T) {
	h, convID, me := sendSetup()

	in := SendInput{ConversationID: convID, Content: "ok"}
	if _, err := h.uc.Send(context.Background(), me, in); err != nil {
		t.Fatalf("lỗi: %v", err)
	}
	if _, err := h.uc.Send(context.Background(), me, in); err != nil {
		t.Fatalf("lỗi: %v", err)
	}

	if len(h.msgs.created) != 2 {
		t.Errorf("ghi %d tin, muốn 2", len(h.msgs.created))
	}
}

// TestSendRejectsReplyFromAnotherConversation chặn việc trích dẫn tin nhắn
// của một hội thoại mà người gửi không được đọc — nếu không, trích dẫn trở
// thành đường rò nội dung.
func TestSendRejectsReplyFromAnotherConversation(t *testing.T) {
	h, convID, me := sendSetup()

	foreign := &domainchat.Message{
		ID:             uuid.New(),
		ConversationID: uuid.New(), // hội thoại KHÁC
		Content:        "nội dung của phòng khác",
	}
	h.msgs.byID[foreign.ID] = foreign

	_, err := h.uc.Send(context.Background(), me, SendInput{
		ConversationID: convID, Content: "trích dẫn trộm", ReplyToID: &foreign.ID,
	})
	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
}

// TestSendPushesViewNotEntity khoá lại lỗi đã gặp ở Phase 5: đẩy thẳng entity
// domain (không có json tag) khiến client nhận {"ID":...} thay vì {"id":...}
// và im lặng bỏ qua.
func TestSendPushesViewNotEntity(t *testing.T) {
	h, convID, me := sendSetup()

	if _, err := h.uc.Send(context.Background(), me, SendInput{
		ConversationID: convID, Content: "xin chào",
	}); err != nil {
		t.Fatalf("Send lỗi: %v", err)
	}

	if len(h.pusher.payloads) == 0 {
		t.Fatal("không đẩy bản tin nào")
	}
	if _, ok := h.pusher.payloads[0].(*MessageView); !ok {
		t.Errorf("payload kiểu %T, muốn *MessageView", h.pusher.payloads[0])
	}
	if h.pusher.events[0] != domainchat.EventMessageNew {
		t.Errorf("loại sự kiện = %q, muốn %q",
			h.pusher.events[0], domainchat.EventMessageNew)
	}
}

// TestSendNotifiesOnlyOfflineMembers: người đang online đã thấy tin nhắn trên
// màn hình; một thông báo kèm theo chỉ là tiếng chuông thừa cho thứ họ vừa đọc.
func TestSendNotifiesOnlyOfflineMembers(t *testing.T) {
	h := newChatHarness()

	convID := uuid.New()
	me, online, offline := uuid.New(), uuid.New(), uuid.New()

	h.convs.put(&domainchat.Conversation{
		ID: convID, Kind: domainchat.KindGroup, Name: "Nhóm ba người",
	})
	h.convs.join(convID, me, true)
	h.convs.join(convID, online, false)
	h.convs.join(convID, offline, false)

	h.presence.status = map[uuid.UUID]string{
		me:      "online",
		online:  "online",
		offline: "offline",
	}

	if _, err := h.uc.Send(context.Background(), actorOf(me), SendInput{
		ConversationID: convID, Content: "chào cả nhóm",
	}); err != nil {
		t.Fatalf("Send lỗi: %v", err)
	}

	if h.notifier.calls != 1 {
		t.Fatalf("gọi notifier %d lần, muốn 1", h.notifier.calls)
	}
	if len(h.notifier.recipients) != 1 || h.notifier.recipients[0] != offline {
		t.Errorf("người nhận thông báo = %v, muốn chỉ người offline", h.notifier.recipients)
	}
}

// TestSendSkipsNotificationForMutedMembers: tắt thông báo hội thoại nghĩa là
// không muốn bị đánh động — nhưng vẫn phải NHẬN được tin nhắn realtime.
func TestSendSkipsNotificationForMutedMembers(t *testing.T) {
	h := newChatHarness()

	convID := uuid.New()
	me, muted := uuid.New(), uuid.New()

	h.convs.put(&domainchat.Conversation{
		ID: convID, Kind: domainchat.KindGroup, Name: "Nhóm",
	})
	h.convs.join(convID, me, true)
	h.convs.join(convID, muted, false)
	h.convs.members[convID][muted].IsMuted = true

	h.presence.status = map[uuid.UUID]string{me: "online", muted: "offline"}

	if _, err := h.uc.Send(context.Background(), actorOf(me), SendInput{
		ConversationID: convID, Content: "chào",
	}); err != nil {
		t.Fatalf("Send lỗi: %v", err)
	}

	if h.notifier.calls != 0 {
		t.Error("người đã tắt thông báo hội thoại không được nhận thông báo")
	}

	// Nhưng bản tin realtime vẫn phải gửi cho cả hai.
	if len(h.pusher.to) == 0 || len(h.pusher.to[0]) != 2 {
		t.Error("tin nhắn realtime vẫn phải đẩy cho mọi thành viên, kể cả người đã tắt")
	}
}

func TestSendRejectsTooManyAttachments(t *testing.T) {
	h, convID, me := sendSetup()

	atts := make([]AttachmentInput, maxAttachmentsPerMessage+1)
	for i := range atts {
		atts[i] = AttachmentInput{
			StorageKey: "k", FileName: "f.png", ContentType: "image/png", SizeBytes: 10,
		}
	}

	_, err := h.uc.Send(context.Background(), me, SendInput{
		ConversationID: convID, Content: "nhiều tệp", Attachments: atts,
	})
	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
}

// TestSendAllowsAttachmentWithoutText: gửi một tấm ảnh không kèm lời nào là
// việc bình thường nhất trong chat.
func TestSendAllowsAttachmentWithoutText(t *testing.T) {
	h, convID, me := sendSetup()

	key := "chat/" + convID.String() + "/abc/a.png"
	h.storage.stored[key] = storedObject{size: 1024, detected: "image/png"}

	m, err := h.uc.Send(context.Background(), me, SendInput{
		ConversationID: convID,
		Kind:           domainchat.MessageImage,
		Attachments: []AttachmentInput{{
			StorageKey: key, FileName: "a.png",
			ContentType: "image/png", SizeBytes: 1024,
		}},
	})
	if err != nil {
		t.Fatalf("Send lỗi: %v", err)
	}
	if m == nil {
		t.Fatal("không trả về tin nhắn")
	}
	if len(h.msgs.atts) != 1 {
		t.Errorf("ghi %d tệp đính kèm, muốn 1", len(h.msgs.atts))
	}
}

// =========================================================================
// SỬA VÀ THU HỒI
// =========================================================================

func TestEditOnlyBySender(t *testing.T) {
	h := newChatHarness()

	convID := uuid.New()
	sender, other := uuid.New(), uuid.New()

	h.convs.put(&domainchat.Conversation{ID: convID, Kind: domainchat.KindGroup, Name: "Nhóm"})
	h.convs.join(convID, sender, false)
	h.convs.join(convID, other, false)

	msg := &domainchat.Message{
		ID: uuid.New(), ConversationID: convID, SenderID: &sender,
		Kind: domainchat.MessageText, Content: "của tôi",
	}
	h.msgs.byID[msg.ID] = msg

	_, err := h.uc.Edit(context.Background(), actorOf(other), msg.ID, "sửa trộm")
	if got := statusOf(err); got != http.StatusForbidden {
		t.Errorf("người khác sửa: mã lỗi = %d, muốn 403", got)
	}

	if _, err := h.uc.Edit(
		context.Background(), actorOf(sender), msg.ID, "tôi tự sửa"); err != nil {
		t.Errorf("người gửi phải sửa được: %v", err)
	}
}

func TestEditRejectsDeletedMessage(t *testing.T) {
	h := newChatHarness()

	convID := uuid.New()
	sender := uuid.New()
	h.convs.put(&domainchat.Conversation{ID: convID, Kind: domainchat.KindDirect})
	h.convs.join(convID, sender, false)

	msg := &domainchat.Message{
		ID: uuid.New(), ConversationID: convID, SenderID: &sender, Content: "x",
	}
	h.msgs.byID[msg.ID] = msg
	if err := h.msgs.SoftDelete(context.Background(), msg.ID); err != nil {
		t.Fatalf("dựng bối cảnh lỗi: %v", err)
	}

	_, err := h.uc.Edit(context.Background(), actorOf(sender), msg.ID, "hồi sinh")
	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
}

// TestDeleteByGroupAdmin: nội dung không phù hợp trong một nhóm phải có người
// gỡ được; chờ chính người gửi tự gỡ thì không phải lúc nào cũng xảy ra.
func TestDeleteByGroupAdmin(t *testing.T) {
	h := newChatHarness()

	convID := uuid.New()
	admin, sender := uuid.New(), uuid.New()

	h.convs.put(&domainchat.Conversation{ID: convID, Kind: domainchat.KindGroup, Name: "Nhóm"})
	h.convs.join(convID, admin, true)
	h.convs.join(convID, sender, false)

	msg := &domainchat.Message{
		ID: uuid.New(), ConversationID: convID, SenderID: &sender, Content: "nội dung xấu",
	}
	h.msgs.byID[msg.ID] = msg

	if err := h.uc.Delete(context.Background(), actorOf(admin), msg.ID); err != nil {
		t.Fatalf("quản trị nhóm phải thu hồi được: %v", err)
	}
	if len(h.msgs.deleted) != 1 {
		t.Error("tin nhắn chưa bị thu hồi")
	}
}

func TestDeleteRejectedForOrdinaryMember(t *testing.T) {
	h := newChatHarness()

	convID := uuid.New()
	member, sender := uuid.New(), uuid.New()

	h.convs.put(&domainchat.Conversation{ID: convID, Kind: domainchat.KindGroup, Name: "Nhóm"})
	h.convs.join(convID, member, false)
	h.convs.join(convID, sender, false)

	msg := &domainchat.Message{
		ID: uuid.New(), ConversationID: convID, SenderID: &sender, Content: "x",
	}
	h.msgs.byID[msg.ID] = msg

	err := h.uc.Delete(context.Background(), actorOf(member), msg.ID)
	if got := statusOf(err); got != http.StatusForbidden {
		t.Errorf("mã lỗi = %d, muốn 403", got)
	}
}

// TestDeletedMessageHidesContent là phép thử BẢO MẬT, không phải giao diện.
//
// Xoá mềm là để giữ dấu vết cho quản trị, không phải để nội dung vẫn rò ra qua
// một đường đọc nào đó quên kiểm tra cờ.
func TestDeletedMessageHidesContent(t *testing.T) {
	h := newChatHarness()

	convID := uuid.New()
	me := uuid.New()
	h.convs.put(&domainchat.Conversation{ID: convID, Kind: domainchat.KindDirect})
	h.convs.join(convID, me, false)

	secret := "MAT-KHAU-BI-MAT"
	msg := &domainchat.Message{
		ID: uuid.New(), ConversationID: convID, SenderID: &me, Content: secret,
	}
	h.msgs.byID[msg.ID] = msg
	h.msgs.list = []*domainchat.Message{msg}

	if err := h.uc.Delete(context.Background(), actorOf(me), msg.ID); err != nil {
		t.Fatalf("Delete lỗi: %v", err)
	}

	items, err := h.uc.History(context.Background(), actorOf(me), convID, nil, "", 0)
	if err != nil {
		t.Fatalf("History lỗi: %v", err)
	}
	for _, v := range items {
		if strings.Contains(v.Content, secret) {
			t.Error("lịch sử vẫn trả về nội dung của tin đã thu hồi")
		}
		if !v.Deleted {
			t.Error("tin đã thu hồi phải có cờ deleted")
		}
	}
}

// =========================================================================
// TỆP ĐÍNH KÈM
// =========================================================================

// TestPresignUploadKeyIsUnique: hai người gửi cùng tên tệp trong cùng một hội
// thoại không được ghi đè lên nhau.
func TestPresignUploadKeyIsUnique(t *testing.T) {
	h, convID, me := sendSetup()

	key1, url1, err := h.uc.PresignUpload(
		context.Background(), me, convID, "bao-cao.pdf", "application/pdf", 2048)
	if err != nil {
		t.Fatalf("PresignUpload lỗi: %v", err)
	}
	key2, _, err := h.uc.PresignUpload(
		context.Background(), me, convID, "bao-cao.pdf", "application/pdf", 2048)
	if err != nil {
		t.Fatalf("PresignUpload lỗi: %v", err)
	}

	if key1 == key2 {
		t.Error("hai lần xin URL cho cùng tên tệp phải ra hai khoá khác nhau")
	}
	if url1 == "" {
		t.Error("không trả về URL tải lên")
	}
	if !strings.HasPrefix(key1, "chat/"+convID.String()+"/") {
		t.Errorf("khoá %q không nằm trong thư mục của hội thoại", key1)
	}
}

// TestPresignUploadSanitizesFileName chặn tên tệp phá cấu trúc khoá object.
func TestPresignUploadSanitizesFileName(t *testing.T) {
	h, convID, me := sendSetup()

	key, _, err := h.uc.PresignUpload(
		context.Background(), me, convID, "../../etc/passwd", "text/plain", 10)
	if err != nil {
		t.Fatalf("PresignUpload lỗi: %v", err)
	}

	// Tên tệp đã được làm sạch, nên khoá chỉ còn đúng ba đoạn thư mục của
	// chính hệ thống: chat/<conv>/<uuid>/<tên>.
	if strings.Contains(key, "..") {
		t.Errorf("khoá %q còn chứa đoạn đi lên thư mục cha", key)
	}
	if n := strings.Count(key, "/"); n != 3 {
		t.Errorf("khoá %q có %d dấu gạch chéo, muốn 3", key, n)
	}
}

func TestPresignUploadRejectsBadSize(t *testing.T) {
	h, convID, me := sendSetup()

	for _, size := range []int64{0, -1, maxAttachmentSize + 1} {
		_, _, err := h.uc.PresignUpload(
			context.Background(), me, convID, "a.bin", "application/octet-stream", size)
		if got := statusOf(err); got != http.StatusBadRequest {
			t.Errorf("kích thước %d: mã lỗi = %d, muốn 400", size, got)
		}
	}
}

// =========================================================================
// ĐOẠN TRÍCH TRONG THÔNG BÁO
// =========================================================================

func TestPreview(t *testing.T) {
	cases := []struct {
		name string
		m    *MessageView
		want string
	}{
		{
			name: "tin ngắn giữ nguyên",
			m:    &MessageView{Kind: domainchat.MessageText, Content: "chào bạn"},
			want: "chào bạn",
		},
		{
			// Ảnh và tệp KHÔNG hiện tên tệp trong thông báo: tên tệp có thể
			// mang thông tin nhạy cảm, mà thông báo thì hiện cả trên màn hình
			// khoá của điện thoại.
			name: "ảnh",
			m:    &MessageView{Kind: domainchat.MessageImage, Content: "anh-nhay-cam.png"},
			want: "[Hình ảnh]",
		},
		{
			name: "tệp",
			m:    &MessageView{Kind: domainchat.MessageFile, Content: "bang-luong.xlsx"},
			want: "[Tệp đính kèm]",
		},
	}

	for _, c := range cases {
		if got := preview(c.m); got != c.want {
			t.Errorf("%s: preview = %q, muốn %q", c.name, got, c.want)
		}
	}
}

func TestPreviewTruncatesByRune(t *testing.T) {
	// Cắt theo byte sẽ chặt đôi một ký tự tiếng Việt và sinh ra ký tự rác.
	long := strings.Repeat("ằ", 300)
	got := preview(&MessageView{Kind: domainchat.MessageText, Content: long})

	if !strings.HasSuffix(got, "…") {
		t.Error("đoạn trích dài phải có dấu lược ở cuối")
	}
	trimmed := strings.TrimSuffix(got, "…")
	if n := len([]rune(trimmed)); n != previewLength {
		t.Errorf("đoạn trích có %d ký tự, muốn %d", n, previewLength)
	}
	// Không có ký tự thay thế nào (U+FFFD) nghĩa là không cắt giữa ký tự.
	if strings.ContainsRune(got, '�') {
		t.Error("đoạn trích bị cắt giữa một ký tự nhiều byte")
	}
}
