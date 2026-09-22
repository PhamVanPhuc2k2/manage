package router

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Đối chiếu doc/openapi.yaml với router thật.
//
// Tài liệu API rời khỏi thực tế là chuyện xảy ra âm thầm và không ai phát
// hiện: thêm một endpoint thì nhớ, nhưng cập nhật một file YAML 3800 dòng thì
// quên. Sau vài tháng tài liệu nói một đằng hệ thống làm một nẻo, và nó tệ
// hơn không có tài liệu — người đọc tin vào nó.
//
// Bộ kiểm thử này biến việc đó thành lỗi CI. Cùng cách nghĩ với bộ rà soát
// phân quyền trong router_test.go: thứ gì phải đúng thì để máy canh, không để
// người nhớ.

// specInfraPaths là ba endpoint cố ý KHÔNG có trong đặc tả.
//
// Đặc tả khai `servers` là `/api/v1`, còn ba endpoint này nằm ngoài tiền tố
// đó. Đưa chúng vào sẽ phải khai một server thứ hai chỉ để mô tả ba đường dẫn
// mà không client nghiệp vụ nào gọi tới.
var specInfraPaths = map[string]struct{}{
	"GET /health":  {},
	"GET /ready":   {},
	"GET /metrics": {},
}

type openAPISpec struct {
	Paths map[string]map[string]yaml.Node `yaml:"paths"`
}

func loadSpec(t *testing.T) *openAPISpec {
	t.Helper()

	// Từ internal/delivery/http/router đi ngược lên gốc kho mã.
	path := filepath.Join("..", "..", "..", "..", "..", "doc", "openapi.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("không đọc được %s: %v", path, err)
	}

	var spec openAPISpec
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("doc/openapi.yaml không phải YAML hợp lệ: %v", err)
	}
	if len(spec.Paths) == 0 {
		t.Fatal("đặc tả không có path nào — có vẻ việc đọc đã hỏng")
	}
	return &spec
}

// specOperations liệt kê mọi operation trong đặc tả, dạng "METHOD /đường/dẫn"
// đã gắn tiền tố /api/v1.
func specOperations(t *testing.T) map[string]struct{} {
	t.Helper()

	methods := map[string]bool{
		"get": true, "post": true, "put": true, "patch": true, "delete": true,
	}

	out := map[string]struct{}{}
	for path, ops := range loadSpec(t).Paths {
		for method := range ops {
			if !methods[method] {
				continue // "parameters", "summary", ...
			}
			out[strings.ToUpper(method)+" /api/v1"+path] = struct{}{}
		}
	}
	return out
}

// TestOpenAPICoversEveryRoute: thêm endpoint mà quên cập nhật đặc tả là lỗi.
func TestOpenAPICoversEveryRoute(t *testing.T) {
	h, _ := buildRouter(t)
	spec := specOperations(t)

	var missing []string
	for _, r := range walkRoutes(t, h) {
		if _, ok := specInfraPaths[r.name]; ok {
			continue
		}
		if _, ok := spec[r.name]; !ok {
			missing = append(missing, r.name)
		}
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("%d endpoint có trong router nhưng thiếu trong doc/openapi.yaml:\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
}

// TestOpenAPIHasNoGhostRoutes là chiều ngược lại: đặc tả mô tả một endpoint
// không còn tồn tại.
//
// Cũng tệ ngang việc thiếu: người viết client sẽ gọi nó và nhận 404 mà không
// hiểu vì sao.
func TestOpenAPIHasNoGhostRoutes(t *testing.T) {
	h, _ := buildRouter(t)

	live := map[string]struct{}{}
	for _, r := range walkRoutes(t, h) {
		live[r.name] = struct{}{}
	}

	var ghosts []string
	for op := range specOperations(t) {
		if _, ok := live[op]; !ok {
			ghosts = append(ghosts, op)
		}
	}

	if len(ghosts) > 0 {
		sort.Strings(ghosts)
		t.Errorf("%d endpoint có trong doc/openapi.yaml nhưng router không có:\n  %s",
			len(ghosts), strings.Join(ghosts, "\n  "))
	}
}

// TestOpenAPIPublicRoutesHaveNoSecurity: endpoint công khai phải khai
// `security: []` trong đặc tả.
//
// Thiếu dòng đó thì đặc tả kế thừa `security` mức gốc và nói rằng
// `/auth/login` cần token — một mâu thuẫn tự thân mà trình sinh client sẽ
// tuân theo, và người dùng nó sẽ không đăng nhập được.
func TestOpenAPIPublicRoutesHaveNoSecurity(t *testing.T) {
	spec := loadSpec(t)

	for route := range publicRoutes {
		method, path, _ := strings.Cut(route, " ")
		specPath := strings.TrimPrefix(path, "/api/v1")

		ops, ok := spec.Paths[specPath]
		if !ok {
			t.Errorf("đặc tả thiếu path công khai %q", specPath)
			continue
		}

		op, ok := ops[strings.ToLower(method)]
		if !ok {
			t.Errorf("đặc tả thiếu %s cho %q", method, specPath)
			continue
		}

		var decoded struct {
			Security *[]map[string][]string `yaml:"security"`
		}
		if err := op.Decode(&decoded); err != nil {
			t.Errorf("%s: không đọc được operation: %v", route, err)
			continue
		}
		if decoded.Security == nil {
			t.Errorf("%s là endpoint công khai nhưng đặc tả không khai `security: []`", route)
			continue
		}
		if len(*decoded.Security) != 0 {
			t.Errorf("%s: `security` phải là danh sách rỗng", route)
		}
	}
}
