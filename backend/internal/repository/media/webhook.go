package media

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/livekit/protocol/auth"

	domaincall "github.com/PhamVanPhuc2k2/manage/internal/domain/call"
)

// Kích thước tối đa của một bản tin webhook.
//
// LiveKit gửi vài KB. Đọc không giới hạn từ một endpoint KHÔNG có middleware
// xác thực đứng trước là mời người lạ bơm vài GB vào bộ nhớ của api.
const maxWebhookBody = 64 << 10

var (
	ErrWebhookNoAuth   = errors.New("webhook thiếu header xác thực")
	ErrWebhookBadToken = errors.New("webhook có chữ ký không hợp lệ")
	ErrWebhookBadHash  = errors.New("nội dung webhook không khớp chữ ký")
)

// rawEvent là phần bản tin webhook mà hệ thống dùng tới.
//
// Khai báo tay thay vì dùng kiểu protobuf của LiveKit: kiểu đó kéo theo cả
// cây sinh mã protobuf cho đúng năm trường, và mỗi lần LiveKit đổi phiên
// bản là một lần phải xem lại toàn bộ. Bỏ qua trường lạ là hành vi mong
// muốn — thêm sự kiện mới ở phía LiveKit không được làm hỏng gì ở đây.
type rawEvent struct {
	Event string `json:"event"`

	Room struct {
		Name string `json:"name"`
	} `json:"room"`

	Participant struct {
		Identity string `json:"identity"`
	} `json:"participant"`

	Track struct {
		Type   string `json:"type"`
		Source string `json:"source"`
	} `json:"track"`
}

// Tên sự kiện và nguồn track, theo đúng cách LiveKit viết trong JSON.
//
// Chỉ dùng trong tệp này: phần còn lại của hệ thống nói bằng hằng của tầng
// domain, nên đổi SFU về sau chỉ phải sửa bảng dịch bên dưới.
const (
	lkTrackPublished = "track_published"
	lkRoomFinished   = "room_finished"

	lkSourceCamera      = "CAMERA"
	lkSourceMicrophone  = "MICROPHONE"
	lkSourceScreenShare = "SCREEN_SHARE"
)

// VerifyWebhook đọc và XÁC THỰC một bản tin webhook từ LiveKit.
//
// Endpoint webhook không đi qua middleware xác thực của hệ thống — LiveKit
// không có access token của người dùng nào cả. Nó tự ký bản tin bằng chính
// cặp khoá API, và hàm này là hàng rào DUY NHẤT.
//
// Hai bước, thiếu bước nào cũng vô nghĩa:
//
//  1. Chữ ký JWT trong header `Authorization` phải ký được bằng bí mật của
//     mình. Chặn người lạ bịa ra bản tin.
//  2. sha256 của THÂN bản tin phải khớp claim trong token đó. Chặn việc
//     chặn giữa đường một bản tin hợp lệ rồi đổi nội dung — không có bước
//     này thì token cũ dùng lại được cho thân mới.
//
// Tự làm thay vì gọi webhook.ReceiveWebhookEvent của LiveKit vì hàm đó kéo
// theo cả package protobuf `livekit`, cùng lý do với việc gọi thẳng HTTP
// API thay vì dùng server SDK.
func (l *LiveKit) VerifyWebhook(r *http.Request) (domaincall.MediaEvent, error) {
	var out domaincall.MediaEvent

	if !l.cfg.Enabled() {
		return out, fmt.Errorf("chưa cấu hình máy chủ media")
	}

	token := r.Header.Get("Authorization")
	if token == "" {
		return out, ErrWebhookNoAuth
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody))
	if err != nil {
		return out, fmt.Errorf("đọc thân webhook: %w", err)
	}

	v, err := auth.ParseAPIToken(token)
	if err != nil || v.APIKey() != l.cfg.APIKey {
		return out, ErrWebhookBadToken
	}

	_, grants, err := v.Verify(l.cfg.APISecret)
	if err != nil {
		return out, ErrWebhookBadToken
	}

	sum := sha256.Sum256(body)
	want := base64.StdEncoding.EncodeToString(sum[:])

	// So sánh theo thời gian hằng: so bằng == làm rò rỉ độ dài tiền tố
	// khớp qua thời gian chạy, và đó là đủ để dò từng ký tự một.
	if subtle.ConstantTimeCompare([]byte(grants.Sha256), []byte(want)) != 1 {
		return out, ErrWebhookBadHash
	}

	var ev rawEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		return out, fmt.Errorf("giải mã webhook: %w", err)
	}
	return translate(ev), nil
}

// translate đổi bản tin của LiveKit sang kiểu của tầng domain.
//
// Sự kiện và nguồn track không nhận ra được trả về hằng "other" thay vì
// lỗi: LiveKit thêm sự kiện mới theo từng phiên bản, và một bản tin lạ
// không phải chuyện đáng báo động.
func translate(ev rawEvent) domaincall.MediaEvent {
	out := domaincall.MediaEvent{
		RoomName: ev.Room.Name,
		Identity: ev.Participant.Identity,
		Kind:     domaincall.MediaOther,
		Track:    domaincall.TrackOther,
	}

	switch ev.Event {
	case lkTrackPublished:
		out.Kind = domaincall.MediaTrackPublished
	case lkRoomFinished:
		out.Kind = domaincall.MediaRoomFinished
	}

	switch ev.Track.Source {
	case lkSourceMicrophone:
		out.Track = domaincall.TrackAudio
	case lkSourceCamera:
		out.Track = domaincall.TrackVideo
	case lkSourceScreenShare:
		out.Track = domaincall.TrackScreen
	}

	return out
}
