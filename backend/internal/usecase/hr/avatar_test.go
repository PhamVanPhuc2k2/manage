package hr

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	domainhr "github.com/PhamVanPhuc2k2/manage/internal/domain/hr"
)

// Bộ kiểm thử ảnh đại diện.
//
// Luồng tải ảnh đi THẲNG từ trình duyệt lên R2 bằng presigned URL, nên máy
// chủ không nhìn thấy byte nào trong lúc tải. Toàn bộ việc kiểm tra dồn vào
// bước xác nhận sau đó, và bước ấy là hàng rào duy nhất giữa bucket của công
// ty với một tệp thực thi được đặt tên là ảnh.

// =========================================================================
// GIẢ LẬP LƯU TRỮ
// =========================================================================

type storedObject struct {
	size        int64
	contentType string // kiểu THẬT, suy từ nội dung
}

type fakeStorage struct {
	objects map[string]storedObject
	deleted []string

	presignPutErr error
	presignGetErr error
	detectErr     error
}

func newFakeStorage() *fakeStorage {
	return &fakeStorage{objects: map[string]storedObject{}}
}

func (f *fakeStorage) put(key string, size int64, contentType string) {
	f.objects[key] = storedObject{size: size, contentType: contentType}
}

func (f *fakeStorage) PresignPut(
	_ context.Context, key, _ string, _ time.Duration,
) (string, error) {
	if f.presignPutErr != nil {
		return "", f.presignPutErr
	}
	return "https://r2.example/" + key + "?sig=put", nil
}

func (f *fakeStorage) PresignGet(
	_ context.Context, key string, _ time.Duration,
) (string, error) {
	if f.presignGetErr != nil {
		return "", f.presignGetErr
	}
	return "https://r2.example/" + key + "?sig=get", nil
}

func (f *fakeStorage) Stat(
	_ context.Context, key string,
) (int64, string, error) {
	o, ok := f.objects[key]
	if !ok {
		return 0, "", errors.New("không có object")
	}
	return o.size, o.contentType, nil
}

func (f *fakeStorage) DetectContentType(
	_ context.Context, key string,
) (string, error) {
	if f.detectErr != nil {
		return "", f.detectErr
	}
	o, ok := f.objects[key]
	if !ok {
		return "", errors.New("không có object")
	}
	return o.contentType, nil
}

func (f *fakeStorage) Delete(_ context.Context, key string) error {
	f.deleted = append(f.deleted, key)
	delete(f.objects, key)
	return nil
}

// withStorage nối một bản lưu trữ giả vào harness.
//
// Mặc định harness để storage là nil, vì hệ thống phải chạy được khi chưa
// cấu hình R2 — chỉ riêng chức năng tệp là không.
func (h *hrHarness) withStorage() *fakeStorage {
	s := newFakeStorage()
	h.uc.storage = s
	return s
}

// =========================================================================
// XIN URL TẢI LÊN
// =========================================================================

func TestRequestAvatarUploadRejectsNonImageTypes(t *testing.T) {
	cases := []struct {
		contentType string
		want        int
	}{
		{"image/jpeg", http.StatusOK},
		{"image/png", http.StatusOK},
		{"image/webp", http.StatusOK},
		{"IMAGE/JPEG", http.StatusOK}, // hoa thường không được làm đổi kết quả
		{"  image/png  ", http.StatusOK},
		{"image/svg+xml", http.StatusBadRequest}, // SVG chứa script được
		{"text/html", http.StatusBadRequest},
		{"application/octet-stream", http.StatusBadRequest},
		{"", http.StatusBadRequest},
	}

	for _, tc := range cases {
		t.Run(tc.contentType, func(t *testing.T) {
			h := newHRHarness()
			h.withStorage()
			emp := h.employee("NV002", nil)

			_, err := h.uc.RequestAvatarUpload(
				context.Background(), actorAll(), emp.ID, tc.contentType)

			if got := statusOf(err); got != tc.want {
				t.Errorf("mã lỗi = %d, muốn %d", got, tc.want)
			}
		})
	}
}

// TestRequestAvatarUploadKeyIsScopedToEmployee: khoá phải nằm trong thư mục
// riêng của nhân viên, vì bước xác nhận dựa vào chính tiền tố đó để biết tệp
// thuộc về ai.
func TestRequestAvatarUploadKeyIsScopedToEmployee(t *testing.T) {
	h := newHRHarness()
	h.withStorage()
	emp := h.employee("NV002", nil)

	got, err := h.uc.RequestAvatarUpload(
		context.Background(), actorAll(), emp.ID, "image/png")
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	want := "avatars/" + emp.ID.String() + "/"
	if !strings.HasPrefix(got.Key, want) {
		t.Errorf("khoá = %q, phải bắt đầu bằng %q", got.Key, want)
	}
	if !strings.HasSuffix(got.Key, ".png") {
		t.Errorf("khoá = %q, phải kết thúc bằng .png", got.Key)
	}
}

// TestRequestAvatarUploadKeyIsUnique: khoá có thành phần ngẫu nhiên nên lần
// tải mới không ghi đè lần cũ ngay lập tức. Nhờ vậy bước xác nhận lỗi thì
// ảnh cũ vẫn còn nguyên.
func TestRequestAvatarUploadKeyIsUnique(t *testing.T) {
	h := newHRHarness()
	h.withStorage()
	emp := h.employee("NV002", nil)

	first, _ := h.uc.RequestAvatarUpload(
		context.Background(), actorAll(), emp.ID, "image/png")
	second, _ := h.uc.RequestAvatarUpload(
		context.Background(), actorAll(), emp.ID, "image/png")

	if first.Key == second.Key {
		t.Error("hai lần xin URL cho ra cùng một khoá — lần sau sẽ ghi đè lần trước")
	}
}

// TestRequestAvatarUploadValidatesInputBeforeConfig.
//
// Kiểm tra dữ liệu vào TRƯỚC khi kiểm tra cấu hình hệ thống. Ngược lại thì
// người gửi sai kiểu tệp nhận được thông báo "chưa cấu hình" — không liên
// quan gì tới lỗi thật của họ, và họ sẽ đi báo bộ phận kỹ thuật.
func TestRequestAvatarUploadValidatesInputBeforeConfig(t *testing.T) {
	h := newHRHarness() // storage = nil
	emp := h.employee("NV002", nil)

	_, err := h.uc.RequestAvatarUpload(
		context.Background(), actorAll(), emp.ID, "text/html")

	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400 (lỗi kiểu tệp, không phải lỗi cấu hình)", got)
	}
}

func TestRequestAvatarUploadWithoutStorageIsUnprocessable(t *testing.T) {
	h := newHRHarness() // storage = nil
	emp := h.employee("NV002", nil)

	_, err := h.uc.RequestAvatarUpload(
		context.Background(), actorAll(), emp.ID, "image/png")

	if got := statusOf(err); got != http.StatusUnprocessableEntity {
		t.Errorf("mã lỗi = %d, muốn 422", got)
	}
}

func TestRequestAvatarUploadOutsideScopeReturns404(t *testing.T) {
	h := newHRHarness()
	h.withStorage()
	other := h.employee("NV002", nil)

	_, err := h.uc.RequestAvatarUpload(
		context.Background(), actorSelf(uuid.New()), other.ID, "image/png")

	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
}

// =========================================================================
// XÁC NHẬN
// =========================================================================

// TestConfirmAvatarRejectsOtherPeoplesKey.
//
// Đây là lỗ hổng thật nếu thiếu: gọi confirm với khoá của người khác sẽ gán
// ảnh của họ vào hồ sơ mình — hoặc trỏ vào một object bất kỳ trong bucket.
// Presigned PUT chỉ cho ghi vào đúng khoá đã ký, nhưng confirm thì nhận khoá
// từ client, nên nó phải tự kiểm tra.
func TestConfirmAvatarRejectsOtherPeoplesKey(t *testing.T) {
	h := newHRHarness()
	store := h.withStorage()
	me := h.employee("NV001", nil)
	victim := h.employee("NV002", nil)

	victimKey := "avatars/" + victim.ID.String() + "/anh.png"
	store.put(victimKey, 1024, "image/png")

	_, err := h.uc.ConfirmAvatar(
		context.Background(), actorAll(), me.ID, victimKey)

	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
	if _, ok := h.employees.avatars[me.ID]; ok {
		t.Error("đã gán ảnh của người khác vào hồ sơ mình")
	}
}

// TestConfirmAvatarRejectsDisguisedFile là lý do tồn tại của cả bước xác nhận.
//
// Client khai "image/png" lúc xin URL, rồi PUT lên một tệp HTML. Chỉ việc
// đọc nội dung thật mới phát hiện được, và tệp bị từ chối phải bị XOÁ —
// để lại trong bucket là để sẵn một URL phục vụ nội dung do người ngoài
// kiểm soát.
func TestConfirmAvatarRejectsDisguisedFile(t *testing.T) {
	h := newHRHarness()
	store := h.withStorage()
	emp := h.employee("NV002", nil)

	key := "avatars/" + emp.ID.String() + "/tra-hinh.png"
	store.put(key, 1024, "text/html")

	_, err := h.uc.ConfirmAvatar(context.Background(), actorAll(), emp.ID, key)

	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
	if len(store.deleted) != 1 || store.deleted[0] != key {
		t.Errorf("tệp bị từ chối phải được xoá, đã xoá = %v", store.deleted)
	}
}

// TestConfirmAvatarIgnoresCharsetSuffix: kết quả đoán kiểu có thể kèm
// charset, ví dụ "image/png; charset=binary". So khớp cả chuỗi sẽ từ chối
// một ảnh hoàn toàn hợp lệ.
func TestConfirmAvatarIgnoresCharsetSuffix(t *testing.T) {
	h := newHRHarness()
	store := h.withStorage()
	emp := h.employee("NV002", nil)

	key := "avatars/" + emp.ID.String() + "/anh.png"
	store.put(key, 1024, "image/png; charset=binary")

	if _, err := h.uc.ConfirmAvatar(
		context.Background(), actorAll(), emp.ID, key,
	); err != nil {
		t.Fatalf("ảnh hợp lệ bị từ chối vì phần charset: %v", err)
	}
}

func TestConfirmAvatarRejectsOversizeAndEmpty(t *testing.T) {
	cases := []struct {
		name string
		size int64
	}{
		{"vượt 2MB", maxAvatarBytes + 1},
		{"tệp rỗng", 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHRHarness()
			store := h.withStorage()
			emp := h.employee("NV002", nil)

			key := "avatars/" + emp.ID.String() + "/anh.png"
			store.put(key, tc.size, "image/png")

			_, err := h.uc.ConfirmAvatar(
				context.Background(), actorAll(), emp.ID, key)

			if got := statusOf(err); got != http.StatusBadRequest {
				t.Errorf("mã lỗi = %d, muốn 400", got)
			}
			if len(store.deleted) != 1 {
				t.Errorf("tệp bị từ chối phải được xoá, đã xoá = %v", store.deleted)
			}
		})
	}
}

func TestConfirmAvatarRejectsMissingFile(t *testing.T) {
	h := newHRHarness()
	h.withStorage()
	emp := h.employee("NV002", nil)

	_, err := h.uc.ConfirmAvatar(context.Background(), actorAll(), emp.ID,
		"avatars/"+emp.ID.String()+"/khong-co.png")

	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
}

// TestConfirmAvatarCleansUpOldImage: ảnh cũ bị xoá SAU KHI database đã ghi
// thành công. Xoá trước mà ghi lỗi thì nhân viên mất ảnh mà không được gì.
func TestConfirmAvatarCleansUpOldImage(t *testing.T) {
	h := newHRHarness()
	store := h.withStorage()
	emp := h.employee("NV002", nil)

	oldKey := "avatars/" + emp.ID.String() + "/cu.png"
	emp.AvatarKey = oldKey
	store.put(oldKey, 512, "image/png")

	newKey := "avatars/" + emp.ID.String() + "/moi.png"
	store.put(newKey, 1024, "image/png")

	url, err := h.uc.ConfirmAvatar(
		context.Background(), actorAll(), emp.ID, newKey)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if h.employees.avatars[emp.ID] != newKey {
		t.Errorf("khoá đã ghi = %q, muốn %q", h.employees.avatars[emp.ID], newKey)
	}
	if len(store.deleted) != 1 || store.deleted[0] != oldKey {
		t.Errorf("ảnh cũ chưa được dọn, đã xoá = %v", store.deleted)
	}
	if url == "" {
		t.Error("phải trả về URL xem ảnh mới")
	}
}

func TestConfirmAvatarOutsideScopeReturns404(t *testing.T) {
	h := newHRHarness()
	store := h.withStorage()
	other := h.employee("NV002", nil)

	key := "avatars/" + other.ID.String() + "/anh.png"
	store.put(key, 1024, "image/png")

	_, err := h.uc.ConfirmAvatar(
		context.Background(), actorSelf(uuid.New()), other.ID, key)

	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
}

// =========================================================================
// SINH URL XEM
// =========================================================================

// TestAvatarURLDegradesQuietly.
//
// Nuốt lỗi ở đây là CÓ CHỦ Ý: không ký được URL của một ảnh thì hiển thị ảnh
// mặc định, còn hơn làm hỏng cả trang danh sách nhân viên vì một ảnh. Phép
// thử này giữ cho ai đó sau này không "sửa" nó thành trả lỗi.
func TestAvatarURLDegradesQuietly(t *testing.T) {
	h := newHRHarness()
	store := h.withStorage()
	store.presignGetErr = errors.New("R2 sập")

	got, err := h.uc.AvatarURL(context.Background(), "avatars/x/y.png")
	if err != nil {
		t.Errorf("phải suy giảm êm, không trả lỗi: %v", err)
	}
	if got != "" {
		t.Errorf("URL = %q, muốn rỗng", got)
	}
}

func TestAvatarURLEmptyCases(t *testing.T) {
	t.Run("khoá rỗng", func(t *testing.T) {
		h := newHRHarness()
		h.withStorage()

		got, err := h.uc.AvatarURL(context.Background(), "")
		if err != nil || got != "" {
			t.Errorf("= %q, %v; muốn \"\", nil", got, err)
		}
	})

	t.Run("chưa cấu hình lưu trữ", func(t *testing.T) {
		h := newHRHarness()

		got, err := h.uc.AvatarURL(context.Background(), "avatars/x/y.png")
		if err != nil || got != "" {
			t.Errorf("= %q, %v; muốn \"\", nil", got, err)
		}
	})
}

// =========================================================================
// GỠ ẢNH
// =========================================================================

func TestRemoveAvatar(t *testing.T) {
	h := newHRHarness()
	store := h.withStorage()
	emp := h.employee("NV002", nil)

	key := "avatars/" + emp.ID.String() + "/anh.png"
	emp.AvatarKey = key
	store.put(key, 1024, "image/png")

	if err := h.uc.RemoveAvatar(
		context.Background(), actorAll(), emp.ID,
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if h.employees.avatars[emp.ID] != "" {
		t.Error("khoá ảnh chưa được xoá khỏi hồ sơ")
	}
	if len(store.deleted) != 1 || store.deleted[0] != key {
		t.Errorf("tệp chưa được dọn, đã xoá = %v", store.deleted)
	}
}

// TestRemoveAvatarWithoutAvatarIsNoOp: gỡ ảnh khi chưa có ảnh không phải lỗi.
// Người dùng bấm hai lần không có lý do gì để nhận thông báo lỗi.
func TestRemoveAvatarWithoutAvatarIsNoOp(t *testing.T) {
	h := newHRHarness()
	store := h.withStorage()
	emp := h.employee("NV002", nil)

	if err := h.uc.RemoveAvatar(
		context.Background(), actorAll(), emp.ID,
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if len(store.deleted) != 0 {
		t.Errorf("không có gì để xoá, đã xoá = %v", store.deleted)
	}
}

func TestRemoveAvatarOutsideScopeReturns404(t *testing.T) {
	h := newHRHarness()
	store := h.withStorage()
	other := h.employee("NV002", nil)
	other.AvatarKey = "avatars/" + other.ID.String() + "/anh.png"

	err := h.uc.RemoveAvatar(
		context.Background(), actorSelf(uuid.New()), other.ID)

	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
	if len(store.deleted) != 0 {
		t.Error("đã xoá ảnh của hồ sơ ngoài phạm vi")
	}
}

// Ràng buộc kiểu: bản giả lập phải khớp cổng lưu trữ ở tầng domain, nếu không
// nó chỉ kiểm thử chính nó.
var _ domainhr.FileStorage = (*fakeStorage)(nil)
