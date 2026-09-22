package hr

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	domainhr "github.com/PhamVanPhuc2k2/manage/internal/domain/hr"
)

// Bộ kiểm thử chức vụ.
//
// Chức vụ đơn giản hơn phòng ban — không có cây, không có vòng lặp. Thứ đáng
// giữ ở đây là khoảng lương và luật không cho xoá chức vụ đang có người giữ.

func validPositionInput() PositionInput {
	return PositionInput{Code: "TP", Name: "Trưởng phòng"}
}

func money(v float64) *float64 { return &v }

// =========================================================================
// KHOẢNG LƯƠNG
// =========================================================================

// TestPositionSalaryRange: min lớn hơn max là một khoảng rỗng — không lương
// nào lọt vào, nên mọi kiểm tra dựa trên nó về sau đều sai lặng lẽ.
func TestPositionSalaryRange(t *testing.T) {
	cases := []struct {
		name string
		min  *float64
		max  *float64
		want int
	}{
		{"không khai khoảng", nil, nil, http.StatusOK},
		{"khoảng hợp lệ", money(10_000_000), money(20_000_000), http.StatusOK},
		{"min bằng max", money(15_000_000), money(15_000_000), http.StatusOK},
		{"chỉ có min", money(10_000_000), nil, http.StatusOK},
		{"chỉ có max", nil, money(20_000_000), http.StatusOK},
		{"min âm", money(-1), nil, http.StatusBadRequest},
		{"min lớn hơn max", money(30_000_000), money(20_000_000), http.StatusBadRequest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHRHarness()
			in := validPositionInput()
			in.SalaryMin, in.SalaryMax = tc.min, tc.max

			_, err := h.uc.CreatePosition(context.Background(), in)
			if got := statusOf(err); got != tc.want {
				t.Errorf("mã lỗi = %d, muốn %d (%v)", got, tc.want, err)
			}
		})
	}
}

// =========================================================================
// KIỂM TRA ĐẦU VÀO
// =========================================================================

func TestCreatePositionRejectsBlankFields(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*PositionInput)
	}{
		{"thiếu mã", func(in *PositionInput) { in.Code = "  " }},
		{"thiếu tên", func(in *PositionInput) { in.Name = "" }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHRHarness()
			in := validPositionInput()
			tc.mutate(&in)

			_, err := h.uc.CreatePosition(context.Background(), in)
			if got := statusOf(err); got != http.StatusBadRequest {
				t.Errorf("mã lỗi = %d, muốn 400", got)
			}
		})
	}
}

func TestCreatePositionRejectsDuplicateCode(t *testing.T) {
	h := newHRHarness()
	h.positions.codes["TP"] = true

	_, err := h.uc.CreatePosition(context.Background(), validPositionInput())
	if got := statusOf(err); got != http.StatusConflict {
		t.Errorf("mã lỗi = %d, muốn 409", got)
	}
}

func TestCreatePositionTrimsWhitespace(t *testing.T) {
	h := newHRHarness()
	in := PositionInput{Code: "  TP  ", Name: "  Trưởng phòng  "}

	got, err := h.uc.CreatePosition(context.Background(), in)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if got.Code != "TP" || got.Name != "Trưởng phòng" {
		t.Errorf("khoảng trắng chưa được cắt: %q / %q", got.Code, got.Name)
	}
}

// =========================================================================
// SỬA VÀ XOÁ
// =========================================================================

func TestUpdatePosition(t *testing.T) {
	h := newHRHarness()
	p := h.positions.add(&domainhr.Position{
		CompanyID: h.company.ID, Code: "NV", Name: "Nhân viên",
	})

	in := PositionInput{Code: "CV", Name: "Chuyên viên", SalaryMax: money(25_000_000)}

	got, err := h.uc.UpdatePosition(context.Background(), p.ID, in)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if got.Code != "CV" || got.Name != "Chuyên viên" {
		t.Errorf("chưa cập nhật: %q / %q", got.Code, got.Name)
	}
	if got.SalaryMax == nil || *got.SalaryMax != 25_000_000 {
		t.Errorf("lương tối đa = %v, muốn 25000000", got.SalaryMax)
	}
}

func TestUpdateMissingPositionReturns404(t *testing.T) {
	h := newHRHarness()

	_, err := h.uc.UpdatePosition(
		context.Background(), uuid.New(), validPositionInput())

	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
}

// TestCannotDeletePositionInUse: xoá một chức vụ đang có người giữ sẽ để lại
// các hồ sơ trỏ vào một bản ghi không còn nữa.
//
// Thông báo lỗi nêu SỐ NGƯỜI, vì "chức vụ đang được sử dụng" không cho người
// quản trị biết phải đi sửa bao nhiêu hồ sơ trước khi thử lại.
func TestCannotDeletePositionInUse(t *testing.T) {
	h := newHRHarness()
	p := h.positions.add(&domainhr.Position{
		CompanyID: h.company.ID, Code: "NV", Name: "Nhân viên",
	})
	h.positions.counts[p.ID] = 7

	err := h.uc.DeletePosition(context.Background(), p.ID)
	if got := statusOf(err); got != http.StatusConflict {
		t.Fatalf("mã lỗi = %d, muốn 409", got)
	}
	if !strings.Contains(err.Error(), "7") {
		t.Errorf("thông báo lỗi phải nêu số người: %q", err.Error())
	}
	if len(h.positions.deleted) != 0 {
		t.Error("chức vụ đang dùng đã bị xoá")
	}
}

func TestDeleteEmptyPosition(t *testing.T) {
	h := newHRHarness()
	p := h.positions.add(&domainhr.Position{
		CompanyID: h.company.ID, Code: "NV", Name: "Nhân viên",
	})

	if err := h.uc.DeletePosition(context.Background(), p.ID); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if len(h.positions.deleted) != 1 {
		t.Error("chức vụ chưa bị xoá")
	}
}

func TestDeleteMissingPositionReturns404(t *testing.T) {
	h := newHRHarness()

	err := h.uc.DeletePosition(context.Background(), uuid.New())
	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
}

// =========================================================================
// ĐỌC
// =========================================================================

func TestListAndGetPosition(t *testing.T) {
	h := newHRHarness()
	p := h.positions.add(&domainhr.Position{
		CompanyID: h.company.ID, Code: "NV", Name: "Nhân viên",
	})

	list, err := h.uc.ListPositions(context.Background())
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("số chức vụ = %d, muốn 1", len(list))
	}

	got, err := h.uc.GetPosition(context.Background(), p.ID)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if got.ID != p.ID {
		t.Errorf("trả về %v, muốn %v", got.ID, p.ID)
	}
}

func TestGetMissingPositionReturns404(t *testing.T) {
	h := newHRHarness()

	_, err := h.uc.GetPosition(context.Background(), uuid.New())
	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
}

// =========================================================================
// itoa
// =========================================================================

// TestItoa kiểm tra bản chuyển số sang chuỗi viết tay trong position.go.
//
// Số 0 là trường hợp mà vòng lặp `for n > 0` bỏ qua hoàn toàn, trả về chuỗi
// rỗng nếu không có nhánh riêng — và nó chính là con số hay gặp nhất trong
// thông báo lỗi đếm nhân viên.
func TestItoa(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{0, "0"},
		{1, "1"},
		{9, "9"},
		{10, "10"},
		{7, "7"},
		{1234567890, "1234567890"},
	}

	for _, tc := range cases {
		if got := itoa(tc.in); got != tc.want {
			t.Errorf("itoa(%d) = %q, muốn %q", tc.in, got, tc.want)
		}
	}
}
