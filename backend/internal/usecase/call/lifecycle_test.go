package call

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	domaincall "github.com/PhamVanPhuc2k2/manage/internal/domain/call"
	domainrealtime "github.com/PhamVanPhuc2k2/manage/internal/domain/realtime"
)

// Bộ kiểm thử vòng đời cuộc gọi.
//
// Trọng tâm là những luật mà sai thì KHÔNG có lỗi nào được báo: chuông
// không tắt ở thiết bị khác, cuộc gọi treo vĩnh viễn ở trạng thái đang
// chạy, phòng rác còn lại trên SFU. Cả ba đều im lặng cho tới lúc người
// dùng phàn nàn.

// =========================================================================
// BẮT ĐẦU
// =========================================================================

func TestStartCreatesRingingCall(t *testing.T) {
	h := newHarness(t)
	caller, peer := uuid.New(), uuid.New()
	conv := h.conversation(caller, peer)

	got, err := h.uc.Start(context.Background(), actorOf(caller), conv,
		domaincall.KindVideo)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if got.Call.Status != domaincall.StatusRinging {
		t.Errorf("trạng thái = %q, muốn %q", got.Call.Status, domaincall.StatusRinging)
	}
	if got.Token == "" {
		t.Error("người gọi phải nhận token vào phòng ngay, không phải gọi thêm lượt nữa")
	}
	if got.Call.RoomName != domaincall.RoomNameFor(got.Call.ID) {
		t.Errorf("tên phòng = %q, phải sinh từ id cuộc gọi", got.Call.RoomName)
	}
}

// TestStartRingsEveryoneExceptCaller: người bấm gọi không tự đổ chuông cho
// mình.
func TestStartRingsEveryoneExceptCaller(t *testing.T) {
	h := newHarness(t)
	caller, b, c := uuid.New(), uuid.New(), uuid.New()
	conv := h.conversation(caller, b, c)

	got, err := h.uc.Start(context.Background(), actorOf(caller), conv,
		domaincall.KindVideo)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if len(got.Ringing) != 2 {
		t.Fatalf("số người đổ chuông = %d, muốn 2", len(got.Ringing))
	}
	if slices.Contains(got.Ringing, caller) {
		t.Error("người gọi bị đổ chuông cho chính mình")
	}
	if !slices.Contains(h.signal.eventsTo(b), domaincall.EventIncoming) {
		t.Error("thành viên B không nhận được call.incoming")
	}
}

// TestStartSkipsBusyPeople khoá lại quyết định "từ chối ngay khi bận".
//
// Người đang họp không bị làm phiền, và người gọi biết ngay thay vì chờ 45
// giây rồi tự hiểu.
func TestStartSkipsBusyPeople(t *testing.T) {
	h := newHarness(t)
	caller, busy, free := uuid.New(), uuid.New(), uuid.New()
	conv := h.conversation(caller, busy, free)

	// Dựng sẵn một cuộc gọi khác mà `busy` đang tham gia.
	other := h.calls.add(&domaincall.Call{
		ConversationID: uuid.New(),
		Kind:           domaincall.KindVideo,
		Status:         domaincall.StatusActive,
		StartedAt:      h.now,
	})
	liveMembers[busy] = other.ID

	got, err := h.uc.Start(context.Background(), actorOf(caller), conv,
		domaincall.KindVideo)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if !slices.Contains(got.Busy, busy) {
		t.Errorf("danh sách bận = %v, phải có %v", got.Busy, busy)
	}
	if slices.Contains(got.Ringing, busy) {
		t.Error("người đang bận vẫn bị đổ chuông")
	}
	if !slices.Contains(got.Ringing, free) {
		t.Error("người rảnh không được đổ chuông")
	}
	if slices.Contains(h.signal.eventsTo(busy), domaincall.EventIncoming) {
		t.Error("đã gửi call.incoming cho người đang bận")
	}
}

// TestStartTreatsLookupFailureAsFree.
//
// Redis chớp một nhịp không được biến thành "cả công ty đang bận". Thà làm
// phiền một người đang họp còn hơn im lặng không gọi được cho ai.
func TestStartTreatsLookupFailureAsFree(t *testing.T) {
	h := newHarness(t)
	caller, peer := uuid.New(), uuid.New()
	conv := h.conversation(caller, peer)
	h.calls.liveForErr = errors.New("redis không phản hồi")

	got, err := h.uc.Start(context.Background(), actorOf(caller), conv,
		domaincall.KindVideo)
	if err != nil {
		t.Fatalf("lỗi tra cứu không được chặn cuộc gọi: %v", err)
	}
	if !slices.Contains(got.Ringing, peer) {
		t.Errorf("người nhận = %v, phải được đổ chuông dù tra cứu hỏng", got.Ringing)
	}
}

// TestStartJoinsExistingCall: bấm gọi khi đã có cuộc gọi đang chạy thì vào
// cuộc đó, không mở cuộc mới.
func TestStartJoinsExistingCall(t *testing.T) {
	h := newHarness(t)
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	conv := h.conversation(a, b, c)

	first, err := h.uc.Start(context.Background(), actorOf(a), conv,
		domaincall.KindVideo)
	if err != nil {
		t.Fatalf("cuộc gọi đầu lỗi: %v", err)
	}

	second, err := h.uc.Start(context.Background(), actorOf(c), conv,
		domaincall.KindVideo)
	if err != nil {
		t.Fatalf("tham gia cuộc đang chạy lỗi: %v", err)
	}

	if second.Call.ID != first.Call.ID {
		t.Errorf("đã mở cuộc gọi thứ hai (%v) thay vì tham gia cuộc đang chạy (%v)",
			second.Call.ID, first.Call.ID)
	}
	if len(h.calls.created) != 1 {
		t.Errorf("số cuộc gọi đã tạo = %d, muốn 1", len(h.calls.created))
	}
}

func TestStartRejectsOutsiders(t *testing.T) {
	h := newHarness(t)
	a, b := uuid.New(), uuid.New()
	conv := h.conversation(a, b)

	_, err := h.uc.Start(context.Background(), actorOf(uuid.New()), conv,
		domaincall.KindVideo)

	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404 (403 sẽ xác nhận hội thoại tồn tại)", got)
	}
	if len(h.calls.created) != 0 {
		t.Error("người ngoài đã mở được cuộc gọi")
	}
}

func TestStartRejectsBadKind(t *testing.T) {
	h := newHarness(t)
	a, b := uuid.New(), uuid.New()
	conv := h.conversation(a, b)

	_, err := h.uc.Start(context.Background(), actorOf(a), conv, "hologram")
	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
}

// TestStartRecordsInviteBeforeRinging: một cuộc gọi nhỡ vẫn phải hiện ra
// với đúng những người đã bị gọi, kể cả khi không ai bắt máy.
func TestStartRecordsInviteBeforeRinging(t *testing.T) {
	h := newHarness(t)
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	conv := h.conversation(a, b, c)

	got, err := h.uc.Start(context.Background(), actorOf(a), conv,
		domaincall.KindAudio)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	rows, _ := h.participants.ListForCall(context.Background(), got.Call.ID)
	if len(rows) != 3 {
		t.Errorf("số dòng người tham gia = %d, muốn 3 (cả người chưa bắt máy)",
			len(rows))
	}
}

// =========================================================================
// BẮT MÁY
// =========================================================================

// startCall dựng sẵn một cuộc gọi đang đổ chuông, trả về id cuộc gọi.
func (h *harness) startCall(t *testing.T, caller uuid.UUID, conv uuid.UUID) uuid.UUID {
	t.Helper()
	got, err := h.uc.Start(context.Background(), actorOf(caller), conv,
		domaincall.KindVideo)
	if err != nil {
		t.Fatalf("dựng cuộc gọi lỗi: %v", err)
	}
	h.signal.sent = nil // bỏ qua các sự kiện của bước dựng
	return got.Call.ID
}

func TestAcceptMovesToActive(t *testing.T) {
	h := newHarness(t)
	a, b := uuid.New(), uuid.New()
	conv := h.conversation(a, b)
	callID := h.startCall(t, a, conv)

	got, err := h.uc.Accept(context.Background(), actorOf(b), callID)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if got.Call.Status != domaincall.StatusActive {
		t.Errorf("trạng thái = %q, muốn %q", got.Call.Status, domaincall.StatusActive)
	}
	if got.Token == "" {
		t.Error("người bắt máy phải nhận token vào phòng")
	}
}

// TestAcceptSilencesOtherDevices là phần hay bị quên nhất của tính năng gọi.
//
// Không có nó thì điện thoại vẫn reo trong túi sau khi người ta đã nghe
// trên máy tính. Hub gửi tới MỌI kết nối của một nhân viên, nên bản tin
// call.cancelled phải đi về chính người vừa bắt máy.
func TestAcceptSilencesOtherDevices(t *testing.T) {
	h := newHarness(t)
	a, b := uuid.New(), uuid.New()
	conv := h.conversation(a, b)
	callID := h.startCall(t, a, conv)

	if _, err := h.uc.Accept(context.Background(), actorOf(b), callID); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if !slices.Contains(h.signal.eventsTo(b), domaincall.EventCancelled) {
		t.Errorf("sự kiện gửi tới người bắt máy = %v, phải có %q để tắt chuông "+
			"ở các thiết bị còn lại", h.signal.eventsTo(b), domaincall.EventCancelled)
	}
}

func TestAcceptNotifiesCaller(t *testing.T) {
	h := newHarness(t)
	a, b := uuid.New(), uuid.New()
	conv := h.conversation(a, b)
	callID := h.startCall(t, a, conv)

	if _, err := h.uc.Accept(context.Background(), actorOf(b), callID); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if !slices.Contains(h.signal.eventsTo(a), domaincall.EventAccepted) {
		t.Error("người gọi không được báo là đã có người nghe")
	}
}

// TestSecondAcceptInGroupCallIsFine: trong cuộc gọi nhóm, người thứ hai bắt
// máy không làm gì sai cả.
func TestSecondAcceptInGroupCallIsFine(t *testing.T) {
	h := newHarness(t)
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	conv := h.conversation(a, b, c)
	callID := h.startCall(t, a, conv)

	if _, err := h.uc.Accept(context.Background(), actorOf(b), callID); err != nil {
		t.Fatalf("người thứ nhất lỗi: %v", err)
	}
	if _, err := h.uc.Accept(context.Background(), actorOf(c), callID); err != nil {
		t.Errorf("người thứ hai bắt máy phải thành công: %v", err)
	}
}

func TestAcceptEndedCallIsConflict(t *testing.T) {
	h := newHarness(t)
	a, b := uuid.New(), uuid.New()
	conv := h.conversation(a, b)
	callID := h.startCall(t, a, conv)

	if err := h.uc.End(context.Background(), actorOf(a), callID); err != nil {
		t.Fatalf("kết thúc lỗi: %v", err)
	}

	_, err := h.uc.Accept(context.Background(), actorOf(b), callID)
	if got := statusOf(err); got != http.StatusConflict {
		t.Errorf("mã lỗi = %d, muốn 409", got)
	}
}

func TestAcceptByOutsiderReturns404(t *testing.T) {
	h := newHarness(t)
	a, b := uuid.New(), uuid.New()
	conv := h.conversation(a, b)
	callID := h.startCall(t, a, conv)

	_, err := h.uc.Accept(context.Background(), actorOf(uuid.New()), callID)
	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
}

// =========================================================================
// TỪ CHỐI
// =========================================================================

// TestRejectInDirectCallEndsIt: hội thoại 1-1, người kia từ chối là hết.
func TestRejectInDirectCallEndsIt(t *testing.T) {
	h := newHarness(t)
	a, b := uuid.New(), uuid.New()
	conv := h.conversation(a, b)
	callID := h.startCall(t, a, conv)

	if err := h.uc.Reject(context.Background(), actorOf(b), callID); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if got := h.calls.byID[callID].Status; got != domaincall.StatusRejected {
		t.Errorf("trạng thái = %q, muốn %q", got, domaincall.StatusRejected)
	}
}

// TestRejectInGroupCallKeepsItAlive: trong nhóm ba người, một người từ chối
// KHÔNG được làm cuộc gọi tắt — những người khác vẫn đang đổ chuông.
func TestRejectInGroupCallKeepsItAlive(t *testing.T) {
	h := newHarness(t)
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	conv := h.conversation(a, b, c)
	callID := h.startCall(t, a, conv)

	if err := h.uc.Reject(context.Background(), actorOf(b), callID); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if got := h.calls.byID[callID].Status; got != domaincall.StatusRinging {
		t.Errorf("trạng thái = %q, muốn vẫn %q — người gọi còn trong phòng",
			got, domaincall.StatusRinging)
	}
}

func TestRejectSilencesOtherDevices(t *testing.T) {
	h := newHarness(t)
	a, b := uuid.New(), uuid.New()
	conv := h.conversation(a, b)
	callID := h.startCall(t, a, conv)

	if err := h.uc.Reject(context.Background(), actorOf(b), callID); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if !slices.Contains(h.signal.eventsTo(b), domaincall.EventCancelled) {
		t.Error("chuông không được tắt ở các thiết bị còn lại của người từ chối")
	}
}

// TestRejectAfterEndIsNoOp: bấm từ chối khi cuộc gọi đã tắt không phải lỗi.
func TestRejectAfterEndIsNoOp(t *testing.T) {
	h := newHarness(t)
	a, b := uuid.New(), uuid.New()
	conv := h.conversation(a, b)
	callID := h.startCall(t, a, conv)

	if err := h.uc.End(context.Background(), actorOf(a), callID); err != nil {
		t.Fatalf("kết thúc lỗi: %v", err)
	}
	if err := h.uc.Reject(context.Background(), actorOf(b), callID); err != nil {
		t.Errorf("từ chối sau khi đã kết thúc không phải lỗi: %v", err)
	}
}

// =========================================================================
// RỜI PHÒNG VÀ KẾT THÚC
// =========================================================================

// TestLeaveInGroupCallDoesNotEndIt.
//
// Trong cuộc họp ba người, một người tắt máy không được làm hai người còn
// lại văng ra.
func TestLeaveInGroupCallDoesNotEndIt(t *testing.T) {
	h := newHarness(t)
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	conv := h.conversation(a, b, c)
	callID := h.startCall(t, a, conv)

	if _, err := h.uc.Accept(context.Background(), actorOf(b), callID); err != nil {
		t.Fatalf("b bắt máy lỗi: %v", err)
	}
	if _, err := h.uc.Accept(context.Background(), actorOf(c), callID); err != nil {
		t.Fatalf("c bắt máy lỗi: %v", err)
	}

	if err := h.uc.Leave(context.Background(), actorOf(c), callID); err != nil {
		t.Fatalf("rời phòng lỗi: %v", err)
	}

	if got := h.calls.byID[callID].Status; got != domaincall.StatusActive {
		t.Errorf("trạng thái = %q, muốn vẫn %q — còn hai người trong phòng",
			got, domaincall.StatusActive)
	}
}

// TestLastLeaveEndsCall: người cuối cùng rời đi thì cuộc gọi kết thúc.
func TestLastLeaveEndsCall(t *testing.T) {
	h := newHarness(t)
	a, b := uuid.New(), uuid.New()
	conv := h.conversation(a, b)
	callID := h.startCall(t, a, conv)

	if _, err := h.uc.Accept(context.Background(), actorOf(b), callID); err != nil {
		t.Fatalf("bắt máy lỗi: %v", err)
	}
	if err := h.uc.Leave(context.Background(), actorOf(b), callID); err != nil {
		t.Fatalf("b rời lỗi: %v", err)
	}
	if err := h.uc.Leave(context.Background(), actorOf(a), callID); err != nil {
		t.Fatalf("a rời lỗi: %v", err)
	}

	if got := h.calls.byID[callID].Status; got != domaincall.StatusEnded {
		t.Errorf("trạng thái = %q, muốn %q", got, domaincall.StatusEnded)
	}
}

// TestOnlyInitiatorCanEndForEveryone.
func TestOnlyInitiatorCanEndForEveryone(t *testing.T) {
	h := newHarness(t)
	a, b := uuid.New(), uuid.New()
	conv := h.conversation(a, b)
	callID := h.startCall(t, a, conv)

	err := h.uc.End(context.Background(), actorOf(b), callID)
	if got := statusOf(err); got != http.StatusForbidden {
		t.Errorf("mã lỗi = %d, muốn 403", got)
	}
	if h.calls.byID[callID].Status.Final() {
		t.Error("người không phải chủ cuộc gọi đã kết thúc được cho tất cả")
	}
}

// TestEndBeforeAnswerIsCancelled: chưa ai bắt máy mà người gọi cúp thì đó
// là HUỶ, không phải kết thúc. Phân biệt này đi thẳng vào câu hiển thị
// trong khung chat.
func TestEndBeforeAnswerIsCancelled(t *testing.T) {
	h := newHarness(t)
	a, b := uuid.New(), uuid.New()
	conv := h.conversation(a, b)
	callID := h.startCall(t, a, conv)

	if err := h.uc.End(context.Background(), actorOf(a), callID); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if got := h.calls.byID[callID].Status; got != domaincall.StatusCancelled {
		t.Errorf("trạng thái = %q, muốn %q", got, domaincall.StatusCancelled)
	}
}

// TestEndClosesRoomOnSFU.
//
// Phòng rác trên SFU không gây lỗi nào — nó chỉ âm thầm ăn tài nguyên, nên
// việc quên đóng sẽ không ai phát hiện cho tới khi máy chủ hết chỗ.
func TestEndClosesRoomOnSFU(t *testing.T) {
	h := newHarness(t)
	a, b := uuid.New(), uuid.New()
	conv := h.conversation(a, b)
	callID := h.startCall(t, a, conv)
	room := h.calls.byID[callID].RoomName

	if err := h.uc.End(context.Background(), actorOf(a), callID); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if !slices.Contains(h.media.closed, room) {
		t.Errorf("phòng đã đóng = %v, phải có %q", h.media.closed, room)
	}
}

// TestEndWritesSystemMessage: khung chat phải có dòng tóm tắt cuộc gọi.
func TestEndWritesSystemMessage(t *testing.T) {
	h := newHarness(t)
	a, b := uuid.New(), uuid.New()
	conv := h.conversation(a, b)
	callID := h.startCall(t, a, conv)

	if _, err := h.uc.Accept(context.Background(), actorOf(b), callID); err != nil {
		t.Fatalf("bắt máy lỗi: %v", err)
	}
	if err := h.uc.End(context.Background(), actorOf(a), callID); err != nil {
		t.Fatalf("kết thúc lỗi: %v", err)
	}

	if len(h.messenger.posted) != 1 {
		t.Fatalf("số tin nhắn hệ thống = %d, muốn 1", len(h.messenger.posted))
	}
	if !strings.Contains(h.messenger.posted[0], "Cuộc gọi video") {
		t.Errorf("nội dung = %q, phải nói rõ loại cuộc gọi", h.messenger.posted[0])
	}
}

// TestEndSurvivesSFUFailure: cuộc gọi đã kết thúc về mặt nghiệp vụ, và
// LiveKit tự dọn phòng rỗng. Báo lỗi lúc này chỉ khiến người dùng tưởng
// thao tác thất bại.
func TestEndSurvivesSFUFailure(t *testing.T) {
	h := newHarness(t)
	a, b := uuid.New(), uuid.New()
	conv := h.conversation(a, b)
	callID := h.startCall(t, a, conv)
	h.media.closeErr = errors.New("SFU không phản hồi")

	if err := h.uc.End(context.Background(), actorOf(a), callID); err != nil {
		t.Errorf("lỗi đóng phòng không được chặn việc kết thúc: %v", err)
	}
	if !h.calls.byID[callID].Status.Final() {
		t.Error("cuộc gọi phải được đánh dấu kết thúc")
	}
}

// TestDoubleEndIsNotAnError: hai người cùng bấm cúp máy là chuyện bình
// thường, và lời gọi thua không được ghi đè lý do kết thúc của lời gọi thắng.
func TestDoubleEndIsNotAnError(t *testing.T) {
	h := newHarness(t)
	a, b := uuid.New(), uuid.New()
	conv := h.conversation(a, b)
	callID := h.startCall(t, a, conv)

	if err := h.uc.End(context.Background(), actorOf(a), callID); err != nil {
		t.Fatalf("lần một lỗi: %v", err)
	}
	if err := h.uc.End(context.Background(), actorOf(a), callID); err != nil {
		t.Errorf("lần hai không được báo lỗi: %v", err)
	}
	if n := len(h.calls.transitions); n != 1 {
		t.Errorf("số lần đổi trạng thái = %d, muốn 1: %v", n, h.calls.transitions)
	}
}

// =========================================================================
// HẾT GIỜ ĐỔ CHUÔNG
// =========================================================================

// TestExpireRingingMarksMissed.
//
// Cần thiết dù client tự tắt chuông theo ExpiresAt: client có thể đã đóng
// tab, và một cuộc gọi kẹt ở 'ringing' sẽ chặn MỌI cuộc gọi sau trong cùng
// hội thoại vì chỉ mục một phần không cho hai cuộc cùng sống.
func TestExpireRingingMarksMissed(t *testing.T) {
	h := newHarness(t)
	a, b := uuid.New(), uuid.New()
	conv := h.conversation(a, b)
	callID := h.startCall(t, a, conv)

	// Đẩy đồng hồ qua thời hạn đổ chuông.
	h.uc.SetClock(fixedClock{now: h.now.Add(domaincall.RingTimeout + time.Second)})

	n, err := h.uc.ExpireRinging(context.Background())
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if n != 1 {
		t.Fatalf("số cuộc gọi hết hạn = %d, muốn 1", n)
	}
	if got := h.calls.byID[callID].Status; got != domaincall.StatusMissed {
		t.Errorf("trạng thái = %q, muốn %q", got, domaincall.StatusMissed)
	}
}

// TestExpireRingingFreesConversation: sau khi dọn, hội thoại phải gọi được
// lại. Đây mới là hậu quả thật của việc quên dọn.
func TestExpireRingingFreesConversation(t *testing.T) {
	h := newHarness(t)
	a, b := uuid.New(), uuid.New()
	conv := h.conversation(a, b)
	h.startCall(t, a, conv)

	h.uc.SetClock(fixedClock{now: h.now.Add(domaincall.RingTimeout + time.Second)})
	if _, err := h.uc.ExpireRinging(context.Background()); err != nil {
		t.Fatalf("dọn lỗi: %v", err)
	}

	got, err := h.uc.Start(context.Background(), actorOf(a), conv,
		domaincall.KindVideo)
	if err != nil {
		t.Fatalf("gọi lại sau khi dọn phải được: %v", err)
	}
	if got.Call.Status != domaincall.StatusRinging {
		t.Errorf("trạng thái cuộc gọi mới = %q", got.Call.Status)
	}
}

func TestExpireRingingLeavesActiveCallsAlone(t *testing.T) {
	h := newHarness(t)
	a, b := uuid.New(), uuid.New()
	conv := h.conversation(a, b)
	callID := h.startCall(t, a, conv)

	if _, err := h.uc.Accept(context.Background(), actorOf(b), callID); err != nil {
		t.Fatalf("bắt máy lỗi: %v", err)
	}

	h.uc.SetClock(fixedClock{now: h.now.Add(time.Hour)})
	n, err := h.uc.ExpireRinging(context.Background())
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if n != 0 {
		t.Errorf("đã dọn %d cuộc gọi đang diễn ra", n)
	}
}

// =========================================================================
// MẤT KẾT NỐI
// =========================================================================

// TestDisconnectEndsAbandonedCall.
//
// Không có bước này thì một người đóng laptop giữa cuộc gọi 1-1 sẽ để cuộc
// gọi treo mãi ở 'active', và hội thoại đó không gọi được nữa.
func TestDisconnectEndsAbandonedCall(t *testing.T) {
	h := newHarness(t)
	a, b := uuid.New(), uuid.New()
	conv := h.conversation(a, b)
	callID := h.startCall(t, a, conv)

	if _, err := h.uc.Accept(context.Background(), actorOf(b), callID); err != nil {
		t.Fatalf("bắt máy lỗi: %v", err)
	}
	liveMembers[a] = callID
	liveMembers[b] = callID

	h.uc.HandleDisconnect(context.Background(), b)
	h.uc.HandleDisconnect(context.Background(), a)

	if got := h.calls.byID[callID].Status; got != domaincall.StatusEnded {
		t.Errorf("trạng thái = %q, muốn %q", got, domaincall.StatusEnded)
	}
	if got := h.calls.byID[callID].EndReason; got != domaincall.ReasonNetwork {
		t.Errorf("lý do = %q, muốn %q", got, domaincall.ReasonNetwork)
	}
}

// TestDisconnectOfOneKeepsGroupCall: một người rớt mạng không làm cả cuộc
// họp tắt.
func TestDisconnectOfOneKeepsGroupCall(t *testing.T) {
	h := newHarness(t)
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	conv := h.conversation(a, b, c)
	callID := h.startCall(t, a, conv)

	for _, id := range []uuid.UUID{b, c} {
		if _, err := h.uc.Accept(context.Background(), actorOf(id), callID); err != nil {
			t.Fatalf("bắt máy lỗi: %v", err)
		}
	}
	liveMembers[c] = callID

	h.uc.HandleDisconnect(context.Background(), c)

	if got := h.calls.byID[callID].Status; got != domaincall.StatusActive {
		t.Errorf("trạng thái = %q, muốn vẫn %q", got, domaincall.StatusActive)
	}
}

// TestDisconnectOfSomeoneNotInCallIsHarmless.
func TestDisconnectOfSomeoneNotInCallIsHarmless(t *testing.T) {
	h := newHarness(t)

	// Không panic, không gửi gì. Chỉ cần chạy êm.
	h.uc.HandleDisconnect(context.Background(), uuid.New())

	if len(h.signal.sent) != 0 {
		t.Errorf("đã gửi %d bản tin cho người không trong cuộc gọi nào",
			len(h.signal.sent))
	}
}

// =========================================================================
// CHUYỂN TIẾP SIGNALING
// =========================================================================

// TestRelayOverwritesFrom là phép thử chống mạo danh.
//
// Tin vào trường From do client gửi lên là để một thành viên hợp lệ giả làm
// người khác trong lúc thương lượng kết nối.
func TestRelayOverwritesFrom(t *testing.T) {
	h := newHarness(t)
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	conv := h.conversation(a, b, c)
	callID := h.startCall(t, a, conv)

	err := h.uc.RelaySignal(context.Background(), actorOf(b),
		domainrealtime.TypeCallSDP, domainrealtime.CallSignalPayload{
			CallID: callID,
			To:     c,
			From:   a, // mạo danh
			Data:   []byte(`{"type":"offer"}`),
		})
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if len(h.signal.sent) != 1 {
		t.Fatalf("số bản tin đã gửi = %d, muốn 1", len(h.signal.sent))
	}
	got, ok := h.signal.sent[0].payload.(domainrealtime.CallSignalPayload)
	if !ok {
		t.Fatalf("payload sai kiểu: %T", h.signal.sent[0].payload)
	}
	if got.From != b {
		t.Errorf("From = %v, muốn %v — giá trị client gửi lên đã lọt qua", got.From, b)
	}
}

// TestRelayRejectsOutsideRecipient: một thành viên hợp lệ không được dùng
// đường này để bắn bản tin tới người ngoài cuộc.
func TestRelayRejectsOutsideRecipient(t *testing.T) {
	h := newHarness(t)
	a, b := uuid.New(), uuid.New()
	conv := h.conversation(a, b)
	callID := h.startCall(t, a, conv)

	err := h.uc.RelaySignal(context.Background(), actorOf(a),
		domainrealtime.TypeCallICE, domainrealtime.CallSignalPayload{
			CallID: callID,
			To:     uuid.New(), // người ngoài
			Data:   []byte(`{}`),
		})

	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
	if len(h.signal.sent) != 0 {
		t.Error("đã gửi bản tin tới người ngoài cuộc")
	}
}

func TestRelayRejectsOutsideSender(t *testing.T) {
	h := newHarness(t)
	a, b := uuid.New(), uuid.New()
	conv := h.conversation(a, b)
	callID := h.startCall(t, a, conv)

	err := h.uc.RelaySignal(context.Background(), actorOf(uuid.New()),
		domainrealtime.TypeCallSDP, domainrealtime.CallSignalPayload{
			CallID: callID, To: b, Data: []byte(`{}`),
		})

	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
}

func TestRelayRejectsUnknownType(t *testing.T) {
	h := newHarness(t)
	a, b := uuid.New(), uuid.New()
	conv := h.conversation(a, b)
	callID := h.startCall(t, a, conv)

	err := h.uc.RelaySignal(context.Background(), actorOf(a),
		"call.something", domainrealtime.CallSignalPayload{
			CallID: callID, To: b, Data: []byte(`{}`),
		})

	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
}

func TestRelayRejectsMissingRecipient(t *testing.T) {
	h := newHarness(t)
	a, b := uuid.New(), uuid.New()
	conv := h.conversation(a, b)
	callID := h.startCall(t, a, conv)

	err := h.uc.RelaySignal(context.Background(), actorOf(a),
		domainrealtime.TypeCallSDP, domainrealtime.CallSignalPayload{
			CallID: callID, Data: []byte(`{}`),
		})

	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
}

// TestRelayRejectsEndedCall: không thương lượng kết nối cho một cuộc gọi
// đã tắt.
func TestRelayRejectsEndedCall(t *testing.T) {
	h := newHarness(t)
	a, b := uuid.New(), uuid.New()
	conv := h.conversation(a, b)
	callID := h.startCall(t, a, conv)

	if err := h.uc.End(context.Background(), actorOf(a), callID); err != nil {
		t.Fatalf("kết thúc lỗi: %v", err)
	}

	err := h.uc.RelaySignal(context.Background(), actorOf(a),
		domainrealtime.TypeCallSDP, domainrealtime.CallSignalPayload{
			CallID: callID, To: b, Data: []byte(`{}`),
		})

	if got := statusOf(err); got != http.StatusConflict {
		t.Errorf("mã lỗi = %d, muốn 409", got)
	}
}

// =========================================================================
// ĐỌC
// =========================================================================

// TestLiveReturnsNilNotError: "hội thoại này đang không có cuộc gọi" là câu
// trả lời hợp lệ, không phải lỗi. Giao diện gọi hàm này mỗi lần mở hội
// thoại.
func TestLiveReturnsNilNotError(t *testing.T) {
	h := newHarness(t)
	a, b := uuid.New(), uuid.New()
	conv := h.conversation(a, b)

	got, err := h.uc.Live(context.Background(), actorOf(a), conv)
	if err != nil {
		t.Fatalf("không có cuộc gọi không phải lỗi: %v", err)
	}
	if got != nil {
		t.Errorf("kết quả = %v, muốn nil", got)
	}
}

func TestLiveFindsRunningCall(t *testing.T) {
	h := newHarness(t)
	a, b := uuid.New(), uuid.New()
	conv := h.conversation(a, b)
	callID := h.startCall(t, a, conv)

	got, err := h.uc.Live(context.Background(), actorOf(b), conv)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if got == nil || got.ID != callID {
		t.Errorf("kết quả = %v, muốn cuộc gọi %v", got, callID)
	}
}

func TestLiveForOutsiderReturns404(t *testing.T) {
	h := newHarness(t)
	a, b := uuid.New(), uuid.New()
	conv := h.conversation(a, b)
	h.startCall(t, a, conv)

	_, err := h.uc.Live(context.Background(), actorOf(uuid.New()), conv)
	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
}

// TestICEServersNeedAuth: credential TURN cho phép relay băng thông thật
// qua máy chủ, nên endpoint này không được mở công khai.
func TestICEServersNeedAuth(t *testing.T) {
	h := newHarness(t)

	_, err := h.uc.ICEServers(context.Background(), nil)
	if got := statusOf(err); got != http.StatusUnauthorized {
		t.Errorf("mã lỗi = %d, muốn 401", got)
	}
}

func TestICEServersReturnsTurn(t *testing.T) {
	h := newHarness(t)

	got, err := h.uc.ICEServers(context.Background(), actorOf(uuid.New()))
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if len(got) < 2 {
		t.Fatalf("số máy chủ ICE = %d, muốn ít nhất STUN và TURN", len(got))
	}

	hasTurn := false
	for _, s := range got {
		for _, u := range s.URLs {
			if strings.HasPrefix(u, "turn:") {
				hasTurn = true
			}
		}
	}
	if !hasTurn {
		t.Error("thiếu TURN — mạng chặn UDP sẽ không gọi được")
	}
}

// TestTokenRejectsEndedCall: token cho phép publish media vào phòng, nên
// không cấp cho cuộc gọi đã tắt.
func TestTokenRejectsEndedCall(t *testing.T) {
	h := newHarness(t)
	a, b := uuid.New(), uuid.New()
	conv := h.conversation(a, b)
	callID := h.startCall(t, a, conv)

	if err := h.uc.End(context.Background(), actorOf(a), callID); err != nil {
		t.Fatalf("kết thúc lỗi: %v", err)
	}

	_, err := h.uc.Token(context.Background(), actorOf(a), callID)
	if got := statusOf(err); got != http.StatusConflict {
		t.Errorf("mã lỗi = %d, muốn 409", got)
	}
}

// TestNoMediaServerIsUnprocessable: hệ thống chạy được khi chưa cấu hình
// SFU, chỉ riêng chức năng gọi là không — giống cách module tệp xử lý khi
// chưa có R2.
func TestNoMediaServerIsUnprocessable(t *testing.T) {
	h := newHarness(t)
	h.uc.media = nil

	a, b := uuid.New(), uuid.New()
	conv := h.conversation(a, b)

	_, err := h.uc.Start(context.Background(), actorOf(a), conv,
		domaincall.KindVideo)
	if got := statusOf(err); got != http.StatusUnprocessableEntity {
		t.Errorf("mã lỗi = %d, muốn 422", got)
	}
}
