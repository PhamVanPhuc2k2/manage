// Package media là adapter tới máy chủ media: SFU (LiveKit) và TURN.
//
// Package này là chỗ DUY NHẤT trong hệ thống biết LiveKit tồn tại. Tầng
// nghiệp vụ chỉ thấy interface domaincall.MediaServer với ba method, nên
// đổi sang Janus hay mediasoup về sau chỉ phải viết lại đúng tệp này.
package media

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/livekit/protocol/auth"

	domaincall "github.com/PhamVanPhuc2k2/manage/internal/domain/call"
)

// Config gom mọi tham số của máy chủ media.
type Config struct {
	// APIKey và APISecret do LiveKit cấp. Chúng ký access token, nên lộ ra
	// là người ngoài tự tạo được token vào bất kỳ phòng nào.
	APIKey    string
	APISecret string

	// URL là địa chỉ HTTP của LiveKit để gọi API quản trị (đóng phòng).
	// Ví dụ http://livekit:7880 trong mạng nội bộ Docker.
	URL string

	// STUNURLs và TURNURLs gửi xuống trình duyệt cho đường P2P của gọi 1-1.
	STUNURLs []string
	TURNURLs []string

	// TURNSecret là bí mật chia sẻ với TURN server để sinh credential tạm
	// thời theo cơ chế REST (username = hạn:danh-tính, credential = HMAC).
	//
	// Để trống thì không cấp TURN, chỉ cấp STUN — hệ thống vẫn chạy nhưng
	// người sau NAT đối xứng và mạng chặn UDP sẽ không gọi được.
	TURNSecret string
}

// Enabled cho biết đã cấu hình đủ để gọi được chưa.
//
// Thiếu khoá thì trả về false thay vì báo lỗi lúc khởi động: hệ thống phải
// chạy được khi chưa cấu hình SFU, chỉ riêng chức năng gọi là không — giống
// cách module tệp xử lý khi chưa có R2.
func (c Config) Enabled() bool {
	return c.APIKey != "" && c.APISecret != "" && c.URL != ""
}

type LiveKit struct {
	cfg  Config
	http *http.Client
}

func New(cfg Config) *LiveKit {
	return &LiveKit{
		cfg: cfg,
		// Timeout ngắn: mọi lời gọi ở đây đều nằm trên đường người dùng
		// đang chờ, và một SFU treo không được kéo theo cả request HTTP.
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

// =========================================================================
// ACCESS TOKEN
// =========================================================================

// IssueToken cấp token vào một phòng.
//
// Token NHÚNG SẴN tên phòng và danh tính, nên client không tự chọn được
// phòng. Đây là chỗ chặn việc một người nghe lén cuộc họp mà họ biết id:
// backend chỉ ký token sau khi đã kiểm tra tư cách thành viên hội thoại.
func (l *LiveKit) IssueToken(
	_ context.Context,
	roomName, identity, displayName string,
	canPublish bool,
	ttl time.Duration,
) (string, error) {
	if !l.cfg.Enabled() {
		return "", fmt.Errorf("chưa cấu hình máy chủ media")
	}

	subscribe := true
	grant := &auth.VideoGrant{
		RoomJoin: true,
		Room:     roomName,

		// CanPublish và CanSubscribe là con trỏ trong SDK: nil nghĩa là
		// "mặc định của máy chủ", không phải false. Đặt tường minh để
		// quyền không đổi theo cấu hình phía LiveKit.
		CanPublish:     &canPublish,
		CanSubscribe:   &subscribe,
		CanPublishData: &canPublish,
	}

	at := auth.NewAccessToken(l.cfg.APIKey, l.cfg.APISecret)
	at.SetVideoGrant(grant).
		SetIdentity(identity).
		SetName(displayName).
		SetValidFor(ttl)

	token, err := at.ToJWT()
	if err != nil {
		return "", fmt.Errorf("ký access token: %w", err)
	}
	return token, nil
}

// adminToken cấp token quản trị để gọi API của LiveKit.
//
// Sống rất ngắn và không rời khỏi tiến trình này. Tách khỏi token người
// dùng vì nó mang quyền RoomAdmin: lọt xuống trình duyệt là ai cũng đóng
// được phòng của người khác.
func (l *LiveKit) adminToken() (string, error) {
	at := auth.NewAccessToken(l.cfg.APIKey, l.cfg.APISecret)
	at.SetVideoGrant(&auth.VideoGrant{RoomAdmin: true}).
		SetIdentity("manage-api").
		SetValidFor(time.Minute)
	return at.ToJWT()
}

// =========================================================================
// QUẢN TRỊ PHÒNG
// =========================================================================

// CloseRoom đóng phòng và đá mọi người còn lại ra.
//
// Gọi thẳng HTTP API (Twirp) của LiveKit thay vì kéo cả server SDK vào:
// SDK mang theo gRPC, protobuf và một cây phụ thuộc lớn cho đúng một lời
// gọi. Một POST với thân JSON làm được y hệt.
func (l *LiveKit) CloseRoom(ctx context.Context, roomName string) error {
	if !l.cfg.Enabled() {
		return nil // chưa cấu hình thì không có phòng nào để đóng
	}

	token, err := l.adminToken()
	if err != nil {
		return fmt.Errorf("tạo token quản trị: %w", err)
	}

	body, err := json.Marshal(map[string]string{"room": roomName})
	if err != nil {
		return err
	}

	url := strings.TrimRight(l.cfg.URL, "/") + "/twirp/livekit.RoomService/DeleteRoom"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url,
		bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	res, err := l.http.Do(req)
	if err != nil {
		return fmt.Errorf("gọi LiveKit: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return fmt.Errorf("LiveKit trả %d: %s", res.StatusCode, msg)
	}
	return nil
}

// =========================================================================
// ICE SERVERS
// =========================================================================

// ICEServers trả về danh sách STUN/TURN cho trình duyệt.
//
// Credential TURN sinh theo cơ chế REST: username là "hạn:danh-tính", còn
// credential là HMAC-SHA1 của username với bí mật chia sẻ, mã hoá base64.
// TURN server tự kiểm được mà không cần hỏi ai, nên không phải đồng bộ
// danh sách người dùng sang đó.
//
// KHÔNG dùng cặp user/pass tĩnh: lộ ra là bị dùng chùa băng thông, và
// không có cách nào thu hồi ngoài đổi bí mật cho tất cả mọi người.
func (l *LiveKit) ICEServers(
	_ context.Context,
	identity string,
	ttl time.Duration,
) ([]domaincall.ICEServer, error) {
	var out []domaincall.ICEServer

	if len(l.cfg.STUNURLs) > 0 {
		out = append(out, domaincall.ICEServer{URLs: l.cfg.STUNURLs})
	}

	if len(l.cfg.TURNURLs) == 0 || l.cfg.TURNSecret == "" {
		return out, nil
	}

	// Hạn là mốc thời gian tuyệt đối, không phải khoảng. TURN server so nó
	// với đồng hồ của chính nó, nên hai máy lệch giờ sẽ làm credential hết
	// hạn sớm hoặc sống lâu hơn dự định.
	expiry := time.Now().Add(ttl).Unix()
	username := strconv.FormatInt(expiry, 10) + ":" + identity

	mac := hmac.New(sha1.New, []byte(l.cfg.TURNSecret))
	mac.Write([]byte(username))
	credential := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	out = append(out, domaincall.ICEServer{
		URLs:       l.cfg.TURNURLs,
		Username:   username,
		Credential: credential,
	})
	return out, nil
}

// Ràng buộc kiểu: adapter phải khớp cổng ở tầng domain.
var _ domaincall.MediaServer = (*LiveKit)(nil)
