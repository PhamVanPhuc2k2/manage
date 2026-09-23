package call

import (
	"context"
	"testing"

	"github.com/google/uuid"

	domaincall "github.com/PhamVanPhuc2k2/manage/internal/domain/call"
)

// liveCall dựng một cuộc gọi đã có người bắt máy, trả về id cuộc gọi và
// hai người trong đó.
func liveCall(t *testing.T, h *harness) (uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()

	a, b := uuid.New(), uuid.New()
	conv := h.conversation(a, b)

	res, err := h.uc.Start(context.Background(), actorOf(a), conv, domaincall.KindVideo)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := h.uc.Accept(context.Background(), actorOf(b), res.Call.ID); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	return res.Call.ID, a, b
}

func TestHandleMediaEventGhiNhanLuongManHinh(t *testing.T) {
	h := newHarness(t)
	callID, a, _ := liveCall(t, h)

	err := h.uc.HandleMediaEvent(context.Background(), domaincall.MediaEvent{
		Kind:     domaincall.MediaTrackPublished,
		RoomName: domaincall.RoomNameFor(callID),
		Identity: a.String(),
		Track:    domaincall.TrackScreen,
	})
	if err != nil {
		t.Fatalf("HandleMediaEvent: %v", err)
	}

	p := h.participants.rows[participantKey{callID, a}]
	if p == nil {
		t.Fatal("không tìm thấy người tham gia")
	}
	if !p.HadScreen {
		t.Error("had_screen chưa được bật")
	}
	// Chỉ bật đúng cờ được báo. Bật lây sang cờ khác thì dấu vết audit nói
	// sai về một việc người ta không làm.
	if p.HadAudio || p.HadVideo {
		t.Errorf("bật lây cờ khác: audio=%v video=%v", p.HadAudio, p.HadVideo)
	}
}

// Ba cờ chỉ BẬT, không bao giờ tắt: câu hỏi cần trả lời là "trong cuộc họp
// đó người này có chiếu màn hình không", không phải "lúc 10h03 có đang
// chiếu không".
func TestHandleMediaEventCongDonKhongGhiDe(t *testing.T) {
	h := newHarness(t)
	callID, a, _ := liveCall(t, h)

	for _, track := range []domaincall.TrackKind{
		domaincall.TrackAudio, domaincall.TrackVideo, domaincall.TrackScreen,
	} {
		if err := h.uc.HandleMediaEvent(context.Background(), domaincall.MediaEvent{
			Kind:     domaincall.MediaTrackPublished,
			RoomName: domaincall.RoomNameFor(callID),
			Identity: a.String(),
			Track:    track,
		}); err != nil {
			t.Fatalf("HandleMediaEvent(%s): %v", track, err)
		}
	}

	p := h.participants.rows[participantKey{callID, a}]
	if !p.HadAudio || !p.HadVideo || !p.HadScreen {
		t.Errorf("mất cờ sau ba lần báo: %+v", p)
	}
}

// Một máy chủ LiveKit có thể phục vụ nhiều ứng dụng. Webhook của ứng dụng
// khác không được làm hỏng gì ở đây, và cũng không được báo lỗi — báo lỗi
// thì SFU gửi lại mãi.
func TestHandleMediaEventBoQuaPhongLa(t *testing.T) {
	h := newHarness(t)
	callID, a, _ := liveCall(t, h)

	err := h.uc.HandleMediaEvent(context.Background(), domaincall.MediaEvent{
		Kind:     domaincall.MediaTrackPublished,
		RoomName: "ung-dung-khac-abc",
		Identity: a.String(),
		Track:    domaincall.TrackScreen,
	})
	if err != nil {
		t.Fatalf("muốn bỏ qua im lặng, nhận %v", err)
	}
	if p := h.participants.rows[participantKey{callID, a}]; p.HadScreen {
		t.Error("phòng lạ vẫn ghi được vào cuộc gọi của mình")
	}
}

func TestHandleMediaEventBoQuaDanhTinhLa(t *testing.T) {
	h := newHarness(t)
	callID, _, _ := liveCall(t, h)

	err := h.uc.HandleMediaEvent(context.Background(), domaincall.MediaEvent{
		Kind:     domaincall.MediaTrackPublished,
		RoomName: domaincall.RoomNameFor(callID),
		Identity: "khong-phai-uuid",
		Track:    domaincall.TrackAudio,
	})
	if err != nil {
		t.Fatalf("muốn bỏ qua im lặng, nhận %v", err)
	}
}

// room_finished là LƯỚI ĐỠ: cuộc gọi lẽ ra đã kết thúc trước khi phòng
// đóng. Nếu chưa, phải dọn — cuộc gọi kẹt ở 'active' sẽ chặn vĩnh viễn
// mọi cuộc gọi sau trong cùng hội thoại.
func TestHandleMediaEventPhongDongThiDonCuocGoiConSong(t *testing.T) {
	h := newHarness(t)
	callID, _, _ := liveCall(t, h)

	err := h.uc.HandleMediaEvent(context.Background(), domaincall.MediaEvent{
		Kind:     domaincall.MediaRoomFinished,
		RoomName: domaincall.RoomNameFor(callID),
	})
	if err != nil {
		t.Fatalf("HandleMediaEvent: %v", err)
	}

	c, err := h.calls.GetByID(context.Background(), callID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if !c.Status.Final() {
		t.Errorf("cuộc gọi vẫn còn sống: %s", c.Status)
	}
	if c.EndReason != domaincall.ReasonEmpty {
		t.Errorf("lý do = %q, muốn %q", c.EndReason, domaincall.ReasonEmpty)
	}
}

// Cuộc gọi đã kết thúc rồi thì room_finished không được đụng vào nữa —
// ghi đè lý do kết thúc là làm hỏng chính dữ liệu cần cho báo cáo.
func TestHandleMediaEventPhongDongKhongGhiDeCuocGoiDaXong(t *testing.T) {
	h := newHarness(t)
	callID, a, _ := liveCall(t, h)

	if err := h.uc.End(context.Background(), actorOf(a), callID); err != nil {
		t.Fatalf("End: %v", err)
	}

	if err := h.uc.HandleMediaEvent(context.Background(), domaincall.MediaEvent{
		Kind:     domaincall.MediaRoomFinished,
		RoomName: domaincall.RoomNameFor(callID),
	}); err != nil {
		t.Fatalf("HandleMediaEvent: %v", err)
	}

	c, _ := h.calls.GetByID(context.Background(), callID)
	if c.EndReason != domaincall.ReasonHangup {
		t.Errorf("lý do bị ghi đè thành %q", c.EndReason)
	}
}

// =========================================================================
// DẤU VẾT LUỒNG MEDIA
// =========================================================================

// Ba cờ had_* phải được ghi từ thứ SFU THẤY, không phải từ lời khai của
// client. Đây là phép thử canh đúng điều đó.
func TestFinishGhiDauVetLuongMediaTuSFU(t *testing.T) {
	h := newHarness(t)
	callID, a, b := liveCall(t, h)

	h.media.media = []domaincall.ParticipantMedia{
		{Identity: a.String(), HasAudio: true, HasVideo: true},
		{Identity: b.String(), HasAudio: true, HasScreen: true},
	}

	if err := h.uc.End(context.Background(), actorOf(a), callID); err != nil {
		t.Fatalf("End: %v", err)
	}

	pa := h.participants.rows[participantKey{callID, a}]
	if !pa.HadAudio || !pa.HadVideo || pa.HadScreen {
		t.Errorf("người gọi: %+v", pa)
	}

	pb := h.participants.rows[participantKey{callID, b}]
	if !pb.HadAudio || pb.HadVideo || !pb.HadScreen {
		t.Errorf("người nhận: %+v", pb)
	}
}

// THỨ TỰ là phần dễ sai nhất: hỏi sau khi đóng phòng thì SFU không còn ai
// để kể, và ba cờ im lặng ở lại false mãi mãi.
func TestFinishHoiLuongMediaTruocKhiDongPhong(t *testing.T) {
	h := newHarness(t)
	callID, a, _ := liveCall(t, h)

	if err := h.uc.End(context.Background(), actorOf(a), callID); err != nil {
		t.Fatalf("End: %v", err)
	}

	if !h.media.askedBeforeClose {
		t.Error("hỏi luồng media SAU khi đóng phòng — lúc đó đã không còn ai")
	}
	if len(h.media.closed) == 0 {
		t.Error("chưa đóng phòng")
	}
}

// Danh tính lạ không được làm hỏng gì: một máy chủ LiveKit có thể phục vụ
// nhiều ứng dụng, và token cấp ngoài hệ thống này cũng vào phòng được.
func TestFinishBoQuaDanhTinhKhongPhaiNhanVien(t *testing.T) {
	h := newHarness(t)
	callID, a, _ := liveCall(t, h)

	h.media.media = []domaincall.ParticipantMedia{
		{Identity: "bot-ghi-hinh", HasAudio: true},
		{Identity: a.String(), HasVideo: true},
	}

	if err := h.uc.End(context.Background(), actorOf(a), callID); err != nil {
		t.Fatalf("End: %v", err)
	}
	if !h.participants.rows[participantKey{callID, a}].HadVideo {
		t.Error("bỏ sót người hợp lệ đứng sau một danh tính lạ")
	}
}
