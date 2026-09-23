package media_test

import (
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/livekit/protocol/auth"

	domaincall "github.com/PhamVanPhuc2k2/manage/internal/domain/call"
	"github.com/PhamVanPhuc2k2/manage/internal/repository/media"
)

const sampleBody = `{"event":"track_published",
  "room":{"name":"manage-call-11111111-1111-1111-1111-111111111111"},
  "participant":{"identity":"nv-001"},
  "track":{"type":"VIDEO","source":"SCREEN_SHARE"}}`

// signed dựng một request GIỐNG HỆT cách LiveKit ký: JWT mang claim sha256
// của thân bản tin.
//
// Dựng lại chữ ký ở đây thay vì gọi hàm của LiveKit là có chủ ý — nếu một
// ngày adapter đổi cách kiểm, test này vẫn mô tả đúng thứ LiveKit thật sự
// gửi, nên nó bắt được sự lệch pha.
func signed(t *testing.T, body, secret string) *http.Request {
	t.Helper()

	sum := sha256.Sum256([]byte(body))
	at := auth.NewAccessToken(testKey, secret)
	at.SetSha256(base64.StdEncoding.EncodeToString(sum[:])).
		SetValidFor(5 * time.Minute)

	token, err := at.ToJWT()
	if err != nil {
		t.Fatalf("ký webhook: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/webhooks/livekit",
		strings.NewReader(body))
	req.Header.Set("Authorization", token)
	return req
}

func TestVerifyWebhookChapNhanBanTinHopLe(t *testing.T) {
	lk := media.New(cfg())

	ev, err := lk.VerifyWebhook(signed(t, sampleBody, testSecret))
	if err != nil {
		t.Fatalf("VerifyWebhook: %v", err)
	}

	// Kiểm bản đã DẪN sang kiểu của domain, không phải bản thô của LiveKit:
	// chính bảng dịch đó là thứ sẽ sai nếu LiveKit đổi tên hằng.
	if ev.Kind != domaincall.MediaTrackPublished {
		t.Errorf("Kind = %q", ev.Kind)
	}
	if ev.Identity != "nv-001" {
		t.Errorf("Identity = %q", ev.Identity)
	}
	if ev.Track != domaincall.TrackScreen {
		t.Errorf("Track = %q", ev.Track)
	}
	if ev.RoomName != "manage-call-11111111-1111-1111-1111-111111111111" {
		t.Errorf("RoomName = %q", ev.RoomName)
	}
}

// Chữ ký bằng bí mật KHÁC phải bị từ chối: đây là hàng rào duy nhất của
// endpoint này, vì nó không đi qua middleware xác thực nào.
func TestVerifyWebhookTuChoiChuKySai(t *testing.T) {
	lk := media.New(cfg())

	_, err := lk.VerifyWebhook(signed(t, sampleBody, "bi-mat-cua-nguoi-khac"))
	if err == nil {
		t.Fatal("muốn lỗi khi chữ ký ký bằng bí mật khác")
	}
}

// Chặn giữa đường một bản tin HỢP LỆ rồi đổi nội dung.
//
// Không kiểm sha256 thì token cũ vẫn dùng được cho thân mới, và người tấn
// công ghi được bất cứ gì họ muốn vào dấu vết audit của cuộc họp.
func TestVerifyWebhookTuChoiKhiNoiDungBiDoi(t *testing.T) {
	lk := media.New(cfg())

	req := signed(t, sampleBody, testSecret)
	// Giữ nguyên chữ ký, thay thân bản tin.
	req.Body = io.NopCloser(strings.NewReader(
		strings.Replace(sampleBody, "nv-001", "nv-999", 1)))

	if _, err := lk.VerifyWebhook(req); err == nil {
		t.Fatal("muốn lỗi khi thân bản tin bị đổi sau khi ký")
	}
}

func TestVerifyWebhookTuChoiKhiThieuHeader(t *testing.T) {
	lk := media.New(cfg())

	req := httptest.NewRequest(http.MethodPost, "/webhooks/livekit",
		strings.NewReader(sampleBody))

	if _, err := lk.VerifyWebhook(req); err == nil {
		t.Fatal("muốn lỗi khi thiếu header Authorization")
	}
}

// Chưa cấu hình thì từ chối, không im lặng chấp nhận.
func TestVerifyWebhookTuChoiKhiChuaCauHinh(t *testing.T) {
	req := signed(t, sampleBody, testSecret)

	if _, err := media.New(media.Config{}).VerifyWebhook(req); err == nil {
		t.Fatal("muốn lỗi khi chưa cấu hình")
	}
}

// Bảng dịch từ tên hằng của LiveKit sang hằng của domain.
//
// Đây là chỗ duy nhất trong hệ thống biết "SCREEN_SHARE" nghĩa là gì. Gõ
// lệch một chữ thì dấu vết audit im lặng thiếu, không báo lỗi gì cả.
func TestVerifyWebhookDichDungNguonTrack(t *testing.T) {
	lk := media.New(cfg())

	for _, tc := range []struct {
		source string
		muon   domaincall.TrackKind
	}{
		{"MICROPHONE", domaincall.TrackAudio},
		{"CAMERA", domaincall.TrackVideo},
		{"SCREEN_SHARE", domaincall.TrackScreen},
		{"SCREEN_SHARE_AUDIO", domaincall.TrackOther},
		{"", domaincall.TrackOther},
	} {
		t.Run(tc.source, func(t *testing.T) {
			body := strings.Replace(sampleBody, "SCREEN_SHARE", tc.source, 1)
			ev, err := lk.VerifyWebhook(signed(t, body, testSecret))
			if err != nil {
				t.Fatalf("VerifyWebhook: %v", err)
			}
			if ev.Track != tc.muon {
				t.Errorf("Track = %q, muốn %q", ev.Track, tc.muon)
			}
		})
	}
}

// Sự kiện lạ không được làm hỏng gì: LiveKit thêm sự kiện mới theo từng
// phiên bản, và nâng cấp SFU không được biến thành một loạt lỗi 500.
func TestVerifyWebhookSuKienLaTraVeOther(t *testing.T) {
	lk := media.New(cfg())

	body := strings.Replace(sampleBody, "track_published", "egress_started", 1)
	ev, err := lk.VerifyWebhook(signed(t, body, testSecret))
	if err != nil {
		t.Fatalf("VerifyWebhook: %v", err)
	}
	if ev.Kind != domaincall.MediaOther {
		t.Errorf("Kind = %q, muốn rỗng", ev.Kind)
	}
}
