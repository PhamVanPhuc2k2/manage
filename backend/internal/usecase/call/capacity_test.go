package call

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	domaincall "github.com/PhamVanPhuc2k2/manage/internal/domain/call"
)

// groupCall dựng một cuộc gọi nhóm có đủ MaxParticipants người trong phòng
// (người gọi + những người đã bắt máy), cộng thêm `extra` người còn đang
// đổ chuông. Trả về id cuộc gọi, danh sách người trong phòng và người chờ.
func groupCall(t *testing.T, h *harness, extra int) (uuid.UUID, []uuid.UUID, []uuid.UUID) {
	t.Helper()
	members := make([]uuid.UUID, domaincall.MaxParticipants+extra)
	for i := range members {
		members[i] = uuid.New()
	}
	conv := h.conversation(members...)
	callID := h.startCall(t, members[0], conv)

	inRoom := members[:domaincall.MaxParticipants]
	for _, m := range inRoom[1:] {
		if _, err := h.uc.Accept(context.Background(), actorOf(m), callID); err != nil {
			t.Fatalf("người thứ %s chưa tới giới hạn mà bị chặn: %v", m, err)
		}
	}
	return callID, inRoom, members[domaincall.MaxParticipants:]
}

func TestAcceptWhenRoomFullIsConflictWithClearMessage(t *testing.T) {
	h := newHarness(t)
	callID, _, waiting := groupCall(t, h, 1)

	tokensBefore := len(h.media.tokens)
	_, err := h.uc.Accept(context.Background(), actorOf(waiting[0]), callID)
	if got := statusOf(err); got != http.StatusConflict {
		t.Fatalf("người thứ %d bắt máy: mã = %d, muốn 409", domaincall.MaxParticipants+1, got)
	}
	if !strings.Contains(err.Error(), "đã đủ 16 người") {
		t.Errorf("câu báo phải nói rõ phòng đã đủ người: %v", err)
	}
	// Bị chặn thì không để lại dấu vết: không token, không ghi là đã vào.
	if len(h.media.tokens) != tokensBefore {
		t.Error("phòng đầy mà vẫn phát token")
	}
	list, _ := h.participants.ListForCall(context.Background(), callID)
	for _, p := range list {
		if p.EmployeeID == waiting[0] && p.JoinedAt != nil {
			t.Error("phòng đầy mà vẫn ghi là đã vào")
		}
	}
}

func TestSomeoneLeavesThenWaitingPersonCanJoin(t *testing.T) {
	h := newHarness(t)
	callID, inRoom, waiting := groupCall(t, h, 1)

	if err := h.uc.Leave(context.Background(), actorOf(inRoom[5]), callID); err != nil {
		t.Fatalf("rời phòng: %v", err)
	}
	if _, err := h.uc.Accept(context.Background(), actorOf(waiting[0]), callID); err != nil {
		t.Fatalf("có người rời thì người chờ phải vào được: %v", err)
	}
}

// Người ĐANG trong phòng xin token mới (rớt mạng rồi vào lại) không được
// bị tính là người thứ 17 — không thì phòng đầy đá chính thành viên ra.
func TestMemberInFullRoomCanStillGetToken(t *testing.T) {
	h := newHarness(t)
	callID, inRoom, _ := groupCall(t, h, 0)

	if _, err := h.uc.Token(context.Background(), actorOf(inRoom[3]), callID); err != nil {
		t.Fatalf("thành viên trong phòng đầy xin lại token phải được: %v", err)
	}
}

// Con số ở backend phải khớp con số SFU thật sự áp. Lệch nhau thì hoặc
// backend chặn oan khi phòng còn chỗ, hoặc người dùng lại nhận lỗi kết nối
// mù mờ — đúng thứ phần kiểm tra này sinh ra để tránh.
func TestMaxParticipantsMatchesLiveKitConfig(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(here), "..", "..", "..", "..", "docker", "livekit", "livekit.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("không đọc được %s: %v", path, err)
	}
	m := regexp.MustCompile(`(?m)^\s*max_participants:\s*(\d+)`).FindSubmatch(raw)
	if m == nil {
		t.Fatal("livekit.yaml không có room.max_participants")
	}
	n, _ := strconv.Atoi(string(m[1]))
	if n != domaincall.MaxParticipants {
		t.Fatalf("livekit.yaml đặt %d, backend đặt %d — phải bằng nhau", n, domaincall.MaxParticipants)
	}
}
