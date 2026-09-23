package media_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/livekit/protocol/auth"

	"github.com/PhamVanPhuc2k2/manage/internal/repository/media"
)

const (
	testKey    = "APIabc123"
	testSecret = "bi-mat-du-dai-de-ky-hmac-sha256-cho-test"
)

func cfg() media.Config {
	return media.Config{
		APIKey:    testKey,
		APISecret: testSecret,
		URL:       "http://livekit:7880",
	}
}

// grantsOf giải mã token bằng chính bí mật đã ký.
//
// Kiểm tra qua đường giải mã thật chứ không so chuỗi: chuỗi JWT đổi theo
// thời gian và thứ tự trường, còn quyền nhúng bên trong mới là thứ quyết
// định người dùng làm được gì trên SFU.
func grantsOf(t *testing.T, token string) *auth.ClaimGrants {
	t.Helper()
	v, err := auth.ParseAPIToken(token)
	if err != nil {
		t.Fatalf("đọc token: %v", err)
	}
	if v.APIKey() != testKey {
		t.Fatalf("APIKey = %q, muốn %q", v.APIKey(), testKey)
	}
	_, grants, err := v.Verify(testSecret)
	if err != nil {
		t.Fatalf("xác thực chữ ký: %v", err)
	}
	return grants
}

// =========================================================================
// ACCESS TOKEN
// =========================================================================

func TestIssueTokenNhungPhongVaDanhTinh(t *testing.T) {
	lk := media.New(cfg())

	token, err := lk.IssueToken(context.Background(),
		"manage-call-abc", "nv-001", "Nguyễn Văn A", true, 10*time.Minute)
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}

	g := grantsOf(t, token)
	if g.Identity != "nv-001" {
		t.Errorf("Identity = %q, muốn nv-001", g.Identity)
	}
	if g.Name != "Nguyễn Văn A" {
		t.Errorf("Name = %q", g.Name)
	}
	if g.Video.Room != "manage-call-abc" {
		t.Errorf("Room = %q", g.Video.Room)
	}
	if !g.Video.RoomJoin {
		t.Error("RoomJoin phải bật, nếu không token vô dụng")
	}
}

// Quyền publish phải được đặt TƯỜNG MINH cả khi bật lẫn khi tắt: nil trong
// SDK nghĩa là "theo mặc định của máy chủ", và mặc định đó có thể đổi ở
// phía LiveKit mà backend không hay biết.
func TestIssueTokenDatQuyenTuongMinh(t *testing.T) {
	lk := media.New(cfg())

	for _, tc := range []struct {
		ten     string
		publish bool
	}{
		{"cho phát", true},
		{"chỉ xem", false},
	} {
		t.Run(tc.ten, func(t *testing.T) {
			token, err := lk.IssueToken(context.Background(),
				"r1", "u1", "U", tc.publish, time.Minute)
			if err != nil {
				t.Fatalf("IssueToken: %v", err)
			}
			g := grantsOf(t, token)

			if g.Video.CanPublish == nil {
				t.Fatal("CanPublish là nil — phải đặt tường minh")
			}
			if *g.Video.CanPublish != tc.publish {
				t.Errorf("CanPublish = %v, muốn %v", *g.Video.CanPublish, tc.publish)
			}

			// Người chỉ xem vẫn phải nhận được hình và tiếng của người khác.
			if g.Video.CanSubscribe == nil || !*g.Video.CanSubscribe {
				t.Error("CanSubscribe phải luôn bật")
			}
		})
	}
}

// Token hết hạn nhanh để kẻ nhặt được nó trên đường truyền không dùng lại
// vào hôm sau.
func TestIssueTokenTonTrongTTL(t *testing.T) {
	lk := media.New(cfg())
	truoc := time.Now()

	token, err := lk.IssueToken(context.Background(),
		"r1", "u1", "U", true, 10*time.Minute)
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}

	v, err := auth.ParseAPIToken(token)
	if err != nil {
		t.Fatalf("đọc token: %v", err)
	}
	claims, _, err := v.Verify(testSecret)
	if err != nil {
		t.Fatalf("xác thực: %v", err)
	}

	han := claims.ExpiresAt.Time
	if han.Before(truoc.Add(9*time.Minute)) || han.After(truoc.Add(11*time.Minute)) {
		t.Errorf("hạn = %v, muốn khoảng 10 phút kể từ %v", han, truoc)
	}
}

// Chưa cấu hình thì phải báo lỗi rõ ràng, không trả token rỗng: một token
// rỗng sẽ đi tiếp xuống trình duyệt và thất bại ở chỗ khó lần ra hơn nhiều.
func TestIssueTokenChuaCauHinhThiBaoLoi(t *testing.T) {
	lk := media.New(media.Config{})

	if _, err := lk.IssueToken(context.Background(),
		"r1", "u1", "U", true, time.Minute); err == nil {
		t.Fatal("muốn lỗi khi chưa cấu hình")
	}
}

func TestEnabledCanDuKhoaVaURL(t *testing.T) {
	full := cfg()
	for _, tc := range []struct {
		ten  string
		c    media.Config
		muon bool
	}{
		{"đủ ba thứ", full, true},
		{"thiếu khoá", media.Config{APISecret: testSecret, URL: full.URL}, false},
		{"thiếu bí mật", media.Config{APIKey: testKey, URL: full.URL}, false},
		{"thiếu URL", media.Config{APIKey: testKey, APISecret: testSecret}, false},
		{"rỗng", media.Config{}, false},
	} {
		t.Run(tc.ten, func(t *testing.T) {
			if got := tc.c.Enabled(); got != tc.muon {
				t.Errorf("Enabled() = %v, muốn %v", got, tc.muon)
			}
		})
	}
}

// =========================================================================
// ICE SERVERS
// =========================================================================

// Không có TURN thì vẫn phải trả STUN: gọi trong cùng mạng công ty chạy
// được mà không cần relay, nên thiếu TURN không phải là lỗi.
func TestICEServersChiSTUNKhiThieuTURN(t *testing.T) {
	c := cfg()
	c.STUNURLs = []string{"stun:stun.l.google.com:19302"}

	got, err := media.New(c).ICEServers(context.Background(), "nv-001", 30*time.Minute)
	if err != nil {
		t.Fatalf("ICEServers: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("có %d mục, muốn 1", len(got))
	}
	if got[0].Username != "" || got[0].Credential != "" {
		t.Error("STUN không được kèm credential")
	}
}

// Thiếu bí mật chia sẻ thì KHÔNG được trả URL TURN kèm credential rỗng:
// trình duyệt sẽ thử relay, thất bại lặng lẽ, và cuộc gọi hỏng theo kiểu
// rất khó chẩn đoán.
func TestICEServersBoTURNKhiThieuBiMat(t *testing.T) {
	c := cfg()
	c.STUNURLs = []string{"stun:a:3478"}
	c.TURNURLs = []string{"turn:a:3478?transport=udp"}
	// TURNSecret để trống

	got, err := media.New(c).ICEServers(context.Background(), "nv-001", time.Minute)
	if err != nil {
		t.Fatalf("ICEServers: %v", err)
	}
	for _, s := range got {
		for _, u := range s.URLs {
			if strings.HasPrefix(u, "turn") {
				t.Errorf("vẫn trả TURN %q dù chưa có bí mật", u)
			}
		}
	}
}

// Credential phải khớp đúng thuật toán mà TURN server dùng để tự kiểm.
// Tính lại độc lập trong test thay vì gọi cùng một hàm: nếu adapter đổi
// thuật toán, credential sẽ hết hiệu lực trên TURN server thật.
func TestICEServersCredentialTheoHMACThoiGian(t *testing.T) {
	c := cfg()
	c.TURNURLs = []string{"turn:turn.abc.vn:3478?transport=udp"}
	c.TURNSecret = "turn-shared-secret"

	truoc := time.Now()
	got, err := media.New(c).ICEServers(context.Background(), "nv-001", 30*time.Minute)
	if err != nil {
		t.Fatalf("ICEServers: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("có %d mục, muốn 1 (chưa cấu hình STUN)", len(got))
	}

	phan := strings.SplitN(got[0].Username, ":", 2)
	if len(phan) != 2 {
		t.Fatalf("username %q không theo dạng hạn:danh-tính", got[0].Username)
	}
	if phan[1] != "nv-001" {
		t.Errorf("danh tính trong username = %q", phan[1])
	}

	han, err := strconv.ParseInt(phan[0], 10, 64)
	if err != nil {
		t.Fatalf("hạn không phải số: %v", err)
	}
	if han < truoc.Add(29*time.Minute).Unix() || han > truoc.Add(31*time.Minute).Unix() {
		t.Errorf("hạn = %d, muốn khoảng 30 phút kể từ %d", han, truoc.Unix())
	}

	mac := hmac.New(sha1.New, []byte(c.TURNSecret))
	mac.Write([]byte(got[0].Username))
	muon := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if got[0].Credential != muon {
		t.Errorf("credential = %q, muốn %q", got[0].Credential, muon)
	}
}

// Hai người khác nhau phải nhận credential khác nhau, nếu không thì thu hồi
// của một người là thu hồi của tất cả.
func TestICEServersMoiNguoiMotCredential(t *testing.T) {
	c := cfg()
	c.TURNURLs = []string{"turn:a:3478"}
	c.TURNSecret = "s"
	lk := media.New(c)

	a, _ := lk.ICEServers(context.Background(), "nv-001", time.Minute)
	b, _ := lk.ICEServers(context.Background(), "nv-002", time.Minute)

	if a[0].Credential == b[0].Credential {
		t.Error("hai người nhận cùng credential")
	}
}

// =========================================================================
// ĐÓNG PHÒNG
// =========================================================================

func TestCloseRoomGoiDungAPIVaMangQuyenQuanTri(t *testing.T) {
	var (
		duongDan string
		than     map[string]string
		token    string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		duongDan = r.URL.Path
		token = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &than)
		w.Write([]byte("{}"))
	}))
	defer srv.Close()

	c := cfg()
	c.URL = srv.URL + "/" // dấu / thừa phải được cắt, không thành "//twirp"
	if err := media.New(c).CloseRoom(context.Background(), "manage-call-abc"); err != nil {
		t.Fatalf("CloseRoom: %v", err)
	}

	if duongDan != "/twirp/livekit.RoomService/DeleteRoom" {
		t.Errorf("đường dẫn = %q", duongDan)
	}
	if than["room"] != "manage-call-abc" {
		t.Errorf("thân = %v", than)
	}

	// Quyền phải là RoomCreate. RoomAdmin nghe có vẻ đúng hơn nhưng ở
	// LiveKit nó là quyền thao tác BÊN TRONG một phòng đã biết tên; xoá
	// cả phòng cần RoomCreate. Đặt nhầm thì DeleteRoom trả 401, và vì lỗi
	// đó chỉ được ghi log nên triệu chứng duy nhất là phòng rỗng nằm lại
	// trên SFU — đã xảy ra thật một lần.
	g := grantsOf(t, token)
	if !g.Video.RoomCreate {
		t.Error("thiếu quyền RoomCreate — DeleteRoom sẽ trả 401")
	}

	// Token quản trị KHÔNG được kèm quyền vào phòng: nó chỉ để gọi API.
	if g.Video.RoomJoin {
		t.Error("token quản trị không được mang cả quyền vào phòng")
	}
}

// Lỗi từ LiveKit phải nổi lên kèm thân phản hồi: "LiveKit trả 401" một
// mình không đủ để biết là sai khoá hay sai tên phòng.
func TestCloseRoomBaoLoiKemThanPhanHoi(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "invalid API key", http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := cfg()
	c.URL = srv.URL
	err := media.New(c).CloseRoom(context.Background(), "r1")
	if err == nil {
		t.Fatal("muốn lỗi")
	}
	if !strings.Contains(err.Error(), "401") ||
		!strings.Contains(err.Error(), "invalid API key") {
		t.Errorf("lỗi = %q, thiếu mã hoặc thân phản hồi", err)
	}
}

// Phòng không tồn tại KHÔNG phải lỗi: LiveKit chỉ tạo phòng khi có người
// thật sự vào, nên mọi cuộc gọi bị từ chối hay hết giờ đổ chuông đều rơi
// vào nhánh này. Coi là lỗi thì nhật ký đầy cảnh báo vô hại, và nhật ký
// đầy cảnh báo vô hại là cách nhanh nhất để không ai đọc nhật ký nữa.
func TestCloseRoomPhongKhongTonTaiThiKhongLoi(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"code":"not_found","msg":"requested room does not exist"}`))
	}))
	defer srv.Close()

	c := cfg()
	c.URL = srv.URL
	if err := media.New(c).CloseRoom(context.Background(), "r1"); err != nil {
		t.Errorf("muốn nil, nhận %v", err)
	}
}

// Chưa cấu hình thì không có phòng nào để đóng — im lặng cho qua, vì đường
// gọi tới đây nằm trong lúc kết thúc cuộc gọi và không được làm hỏng nó.
func TestCloseRoomChuaCauHinhThiKhongLoi(t *testing.T) {
	if err := media.New(media.Config{}).CloseRoom(context.Background(), "r1"); err != nil {
		t.Errorf("muốn nil, nhận %v", err)
	}
}
