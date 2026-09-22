// Rà soát phân quyền của TOÀN BỘ endpoint.
//
// Đây là phép thử bảo mật kiểu "hàng rào": nó không kiểm tra một luồng nghiệp
// vụ nào cả, mà liệt kê mọi route chi đang phục vụ rồi bắn request thật vào
// từng cái. Thêm một route mà quên gắn RequireAuth hoặc RequirePermission là
// làm bộ test này đỏ ngay — không phải chờ tới lúc ai đó rà lại bằng mắt.
//
// Cách làm này bắt được thứ mà đọc code không bắt được: route đăng ký trong
// một nhóm r.Group lồng nhau rất dễ trông như đã có middleware trong khi thực
// tế thì không.
package router

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	"github.com/PhamVanPhuc2k2/manage/pkg/config"
	"github.com/PhamVanPhuc2k2/manage/pkg/jwt"
)

// =========================================================================
// DANH SÁCH TRẮNG
// =========================================================================

// publicRoutes là những endpoint CỐ Ý không cần đăng nhập.
//
// Danh sách này phải ngắn và mỗi dòng phải giải thích được. Thêm một dòng vào
// đây là một quyết định bảo mật, và việc nó nằm trong một tệp test khiến quyết
// định đó lộ ra trong code review thay vì trôi qua.
var publicRoutes = map[string]string{
	"POST /api/v1/auth/login": "bước một của đăng nhập; người gọi chưa có token",
	"POST /api/v1/auth/verify-otp": "bước hai; thứ bảo vệ nó là id thử thách " +
		"ngẫu nhiên, bộ đếm 5 lần sai và giới hạn tốc độ ở nginx",
	"POST /api/v1/auth/resend-otp":      "gửi lại mã; cùng lý do với verify-otp",
	"POST /api/v1/auth/refresh":         "xác thực bằng cookie httpOnly, không bằng access token",
	"POST /api/v1/auth/forgot-password": "người dùng đang không đăng nhập được",
	"POST /api/v1/auth/reset-password":  "xác thực bằng token dùng một lần trong email",
	"GET /api/v1/ping":                  "endpoint nghiệm thu hạ tầng, không trả dữ liệu nghiệp vụ",
}

// permissionFreeRoutes là những endpoint cần ĐĂNG NHẬP nhưng cố ý không có
// middleware kiểm tra quyền.
//
// Hai nhóm, hai lý do khác nhau:
//
//   - Thông tin của CHÍNH người đang đăng nhập (hồ sơ, phiên, mật khẩu, thông
//     báo). Không có quyền nào để cấp ở đây: usecase khoá cứng theo actor, và
//     thêm một mã quyền chỉ tạo ảo giác rằng có thể cấp nó cho người khác.
//   - Thao tác trên chính tài khoản mình (đăng xuất, đổi mật khẩu).
var permissionFreeRoutes = map[string]string{
	"GET /api/v1/auth/me":               "hồ sơ của chính mình",
	"POST /api/v1/auth/logout":          "đăng xuất phiên của chính mình",
	"POST /api/v1/auth/logout-all":      "đăng xuất mọi phiên của chính mình",
	"GET /api/v1/auth/sessions":         "danh sách phiên của chính mình",
	"POST /api/v1/auth/change-password": "đổi mật khẩu của chính mình",

	"GET /api/v1/notifications":             "hộp thư cá nhân, khoá cứng theo actor",
	"GET /api/v1/notifications/summary":     "số chưa đọc của chính mình",
	"POST /api/v1/notifications/read":       "đánh dấu đã đọc; điều kiện employee_id nằm trong câu UPDATE",
	"POST /api/v1/notifications/read-all":   "đánh dấu đã đọc tất cả của chính mình",
	"GET /api/v1/notifications/preferences": "cấu hình nhận thông báo của chính mình",
	"PUT /api/v1/notifications/preferences": "sửa cấu hình của chính mình",
}

// infraRoutes nằm ngoài /api/v1 và cố ý công khai: hạ tầng cần gọi được chúng
// mà không có token.
var infraRoutes = map[string]struct{}{
	"GET /health": {},
	"GET /ready":  {},
	"GET /ws":     {}, // tự xác thực bằng token trên query string, xem ws/handler.go

	// /metrics: Prometheus scrape bằng HTTP thuần, không có token. Thứ bảo vệ
	// nó KHÔNG phải xác thực ở tầng ứng dụng mà là nginx — chỉ mạng nội bộ
	// Docker gọi tới được, xem docker/nginx/conf.d/app.conf.
	"GET /metrics": {},
}

// =========================================================================
// DỰNG ROUTER
// =========================================================================

// fakeSessions trả về một phiên luôn còn sống.
type fakeSessions struct{ session *domainauth.Session }

func (f *fakeSessions) Create(context.Context, domainauth.Session, time.Duration) error {
	return nil
}

func (f *fakeSessions) Get(
	context.Context, uuid.UUID,
) (*domainauth.Session, error) {
	return f.session, nil
}

func (f *fakeSessions) Touch(context.Context, uuid.UUID) error           { return nil }
func (f *fakeSessions) Delete(context.Context, uuid.UUID) error          { return nil }
func (f *fakeSessions) DeleteAllOfUser(context.Context, uuid.UUID) error { return nil }

func (f *fakeSessions) ListOfUser(
	context.Context, uuid.UUID,
) ([]domainauth.Session, error) {
	return nil, nil
}

type fakeAuthReader struct{}

func (fakeAuthReader) Load(
	context.Context, uuid.UUID, uuid.UUID,
) (*domainauth.Authorization, error) {
	return &domainauth.Authorization{}, nil
}

const testSecret = "khoa-bi-mat-chi-dung-trong-test-dai-hon-32-ky-tu"

// buildRouter dựng router với handler là nil.
//
// Nil là CỐ Ý: bộ test này chỉ đi tới tầng middleware rồi dừng (401 hoặc 403),
// nên không handler nào được gọi tới. Route nào lọt qua middleware sẽ panic ở
// handler nil — và chimw.Recoverer biến nó thành 500, tức là "khác 403", tức
// là bộ test báo đỏ. Đó chính là hành vi mong muốn.
func buildRouter(t *testing.T) (http.Handler, *jwt.Manager) {
	t.Helper()

	jwtMgr, err := jwt.NewManager(testSecret, 15*time.Minute, "manage-test")
	if err != nil {
		t.Fatalf("dựng jwt.Manager lỗi: %v", err)
	}

	sessionID := uuid.New()
	h := New(Deps{
		Config: &config.Config{
			CORSAllowedOrigins: []string{"http://localhost:3000"},
		},
		Logger:  zerolog.Nop(),
		Version: "test",

		JWT: jwtMgr,
		Sessions: &fakeSessions{session: &domainauth.Session{
			ID: sessionID, UserID: uuid.New(),
		}},
		AuthReader: fakeAuthReader{},

		// WS để nil: route /ws khi đó không tồn tại, nên bộ test không phải
		// dựng cả hub chỉ để liệt kê route.
		WS: nil,
	})
	return h, jwtMgr
}

// route là một endpoint đã liệt kê được.
//
// Tách name và path vì chúng KHÁC nhau: chi đăng ký route chỉ mục của một nhóm
// lồng nhau với dấu gạch chéo ở cuối ("/api/v1/departments/"), và request phải
// gửi đúng dạng đó mới khớp. Nhưng dạng đó khó đọc trong danh sách trắng, nên
// tên dùng để đối chiếu là dạng đã chuẩn hoá.
type route struct {
	method string
	path   string // dạng gọi được thật, có thể có "/" ở cuối
	name   string // "METHOD /duong/dan" đã bỏ "/" ở cuối, dùng cho danh sách trắng
}

// walkRoutes liệt kê mọi route chi đang phục vụ.
func walkRoutes(t *testing.T, h http.Handler) []route {
	t.Helper()

	r, ok := h.(chi.Routes)
	if !ok {
		t.Fatal("router không hiện thực chi.Routes — không liệt kê được route")
	}

	var routes []route
	err := chi.Walk(r, func(
		method, pattern string, _ http.Handler, _ ...func(http.Handler) http.Handler,
	) error {
		// chi thêm "/*" ở cuối route của các nhóm lồng nhau.
		pattern = strings.TrimSuffix(pattern, "/*")
		if pattern == "" {
			pattern = "/"
		}

		name := pattern
		if len(name) > 1 {
			name = strings.TrimSuffix(name, "/")
		}

		routes = append(routes, route{
			method: method,
			path:   pattern,
			name:   method + " " + name,
		})
		return nil
	})
	if err != nil {
		t.Fatalf("chi.Walk lỗi: %v", err)
	}

	sort.Slice(routes, func(i, j int) bool { return routes[i].name < routes[j].name })
	return routes
}

// requestPath thay tham số đường dẫn bằng một uuid thật.
//
// Phải là uuid HỢP LỆ: nhiều handler phân tích tham số trước khi làm gì khác,
// và một chuỗi rác sẽ cho 400 — che mất việc route đó có kiểm tra quyền hay
// không.
func requestPath(route string) string {
	parts := strings.Split(route, "/")
	for i, p := range parts {
		if strings.HasPrefix(p, "{") {
			parts[i] = uuid.NewString()
		}
	}
	return strings.Join(parts, "/")
}

// =========================================================================
// PHÉP THỬ
// =========================================================================

// TestEveryRouteRequiresAuth: mọi endpoint dưới /api/v1, trừ danh sách trắng,
// phải trả 401 khi không có token.
func TestEveryRouteRequiresAuth(t *testing.T) {
	h, _ := buildRouter(t)
	routes := walkRoutes(t, h)

	if len(routes) < 50 {
		t.Fatalf("chỉ liệt kê được %d route — có vẻ việc liệt kê đã hỏng", len(routes))
	}

	checked := 0
	for _, r := range routes {
		if _, ok := infraRoutes[r.name]; ok {
			continue
		}
		if _, ok := publicRoutes[r.name]; ok {
			continue
		}
		if !strings.HasPrefix(r.path, "/api/v1") {
			t.Errorf("route %q nằm ngoài /api/v1 mà không có trong danh sách hạ tầng", r.name)
			continue
		}

		req := httptest.NewRequest(r.method, requestPath(r.path), strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: không token → %d, muốn 401 (thiếu RequireAuth?)",
				r.name, rec.Code)
		}
		checked++
	}

	t.Logf("đã rà %d endpoint cần đăng nhập", checked)
}

// TestEveryRouteChecksPermission: với token hợp lệ nhưng KHÔNG có quyền nào,
// mọi endpoint phải trả 403 — trừ những route cố ý không có kiểm tra quyền.
//
// Đây là phép thử chống lỗ hổng "route mới quên gắn RequirePermission". Nó bắt
// được cả trường hợp gắn sai mã quyền, vì actor trong bộ test không có quyền nào.
func TestEveryRouteChecksPermission(t *testing.T) {
	h, jwtMgr := buildRouter(t)
	routes := walkRoutes(t, h)

	token, _, err := jwtMgr.Issue(jwt.Claims{
		UserID:     uuid.New(),
		EmployeeID: uuid.New(),
		SessionID:  uuid.New(),
		Roles:      []string{},
		// Danh sách quyền RỖNG có chủ ý.
		Permissions: []string{},
		Scope:       string(domainauth.ScopeSelf),
	})
	if err != nil {
		t.Fatalf("phát hành token lỗi: %v", err)
	}

	var missing []string
	checked := 0

	for _, r := range routes {
		if _, ok := infraRoutes[r.name]; ok {
			continue
		}
		if _, ok := publicRoutes[r.name]; ok {
			continue
		}
		if _, ok := permissionFreeRoutes[r.name]; ok {
			continue
		}

		req := httptest.NewRequest(r.method, requestPath(r.path), strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			missing = append(missing, fmt.Sprintf("  %-70s → %d", r.name, rec.Code))
		}
		checked++
	}

	if len(missing) > 0 {
		t.Errorf("%d endpoint không trả 403 cho actor không có quyền nào.\n"+
			"Thiếu RequirePermission, hoặc là route cố ý không cần quyền và "+
			"phải thêm vào permissionFreeRoutes kèm lý do:\n%s",
			len(missing), strings.Join(missing, "\n"))
	}

	t.Logf("đã rà %d endpoint có kiểm tra quyền", checked)
}

// TestWhitelistsHaveNoStaleEntries: route bị xoá khỏi router mà vẫn còn trong
// danh sách trắng là một ngoại lệ bảo mật không còn ai để ý.
func TestWhitelistsHaveNoStaleEntries(t *testing.T) {
	h, _ := buildRouter(t)

	live := map[string]struct{}{}
	for _, r := range walkRoutes(t, h) {
		live[r.name] = struct{}{}
	}

	for route := range publicRoutes {
		if _, ok := live[route]; !ok {
			t.Errorf("publicRoutes còn %q nhưng router không có route đó nữa", route)
		}
	}
	for route := range permissionFreeRoutes {
		if _, ok := live[route]; !ok {
			t.Errorf("permissionFreeRoutes còn %q nhưng router không có route đó nữa", route)
		}
	}
}

// TestGarbageTokenIsRejected: token rác phải bị từ chối trước khi tới handler.
func TestGarbageTokenIsRejected(t *testing.T) {
	h, _ := buildRouter(t)

	for _, token := range []string{
		"rac",
		"Bearer",
		// Token ký bằng khoá khác — đây là phép thử chống giả mạo JWT.
		"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1aWQiOiJ4In0.sai-chu-ky",
		// alg=none: một lớp tấn công kinh điển vào thư viện JWT cấu hình lỏng.
		"eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJ1aWQiOiJ4In0.",
	} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/employees", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("token %q → %d, muốn 401", token, rec.Code)
		}
	}
}

// TestHealthAndReadyNeedNoToken: hạ tầng phải gọi được hai endpoint này mà
// không có token, nếu không healthcheck của Docker luôn báo đỏ.
func TestHealthNeedsNoToken(t *testing.T) {
	h, _ := buildRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("/health → %d, muốn 200", rec.Code)
	}
}
