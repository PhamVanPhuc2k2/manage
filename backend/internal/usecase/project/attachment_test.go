package project

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	domainproject "github.com/PhamVanPhuc2k2/manage/internal/domain/project"
)

// Bộ kiểm thử tệp đính kèm của công việc.
//
// Tệp đi THẲNG từ trình duyệt lên R2 bằng presigned URL, nên máy chủ không
// nhìn thấy byte nào trong lúc tải. Bước xác nhận sau đó là hàng rào duy
// nhất giữa bucket của công ty và một tệp thực thi được đặt tên là ảnh.

// =========================================================================
// GIẢ LẬP LƯU TRỮ
// =========================================================================

type storedObject struct {
	size        int64
	contentType string // kiểu THẬT, suy từ nội dung
}

type fakeProjectStorage struct {
	objects map[string]storedObject
	deleted []string

	presignPutErr error
	presignGetErr error
}

func newFakeProjectStorage() *fakeProjectStorage {
	return &fakeProjectStorage{objects: map[string]storedObject{}}
}

func (f *fakeProjectStorage) put(key string, size int64, contentType string) {
	f.objects[key] = storedObject{size: size, contentType: contentType}
}

func (f *fakeProjectStorage) PresignPut(
	_ context.Context, key, _ string, _ time.Duration,
) (string, error) {
	if f.presignPutErr != nil {
		return "", f.presignPutErr
	}
	return "https://r2.example/" + key + "?sig=put", nil
}

func (f *fakeProjectStorage) PresignGet(
	_ context.Context, key string, _ time.Duration,
) (string, error) {
	if f.presignGetErr != nil {
		return "", f.presignGetErr
	}
	return "https://r2.example/" + key + "?sig=get", nil
}

func (f *fakeProjectStorage) Stat(
	_ context.Context, key string,
) (int64, string, error) {
	o, ok := f.objects[key]
	if !ok {
		return 0, "", errors.New("không có object")
	}
	return o.size, o.contentType, nil
}

func (f *fakeProjectStorage) DetectContentType(
	_ context.Context, key string,
) (string, error) {
	o, ok := f.objects[key]
	if !ok {
		return "", errors.New("không có object")
	}
	return o.contentType, nil
}

func (f *fakeProjectStorage) Delete(_ context.Context, key string) error {
	f.deleted = append(f.deleted, key)
	delete(f.objects, key)
	return nil
}

// withStorage gắn bản lưu trữ giả vào harness. Mặc định harness để nil, vì
// hệ thống phải chạy được khi chưa cấu hình R2.
func (h *projectHarness) withStorage() *fakeProjectStorage {
	s := newFakeProjectStorage()
	h.uc.storage = s
	return s
}

// seedTask dựng một dự án kèm một công việc, trả về cả hai.
func seedTask(h *projectHarness, owner uuid.UUID) (*domainproject.Project, *domainproject.Task) {
	p := h.seedProject(owner)
	t := h.tasks.add(&domainproject.Task{
		ProjectID: p.ID, Title: "Việc", Status: domainproject.TaskTodo,
	})
	return p, t
}

// =========================================================================
// XIN URL TẢI LÊN
// =========================================================================

func TestRequestAttachmentUploadChecksType(t *testing.T) {
	owner := uuid.New()

	cases := []struct {
		contentType string
		want        int
	}{
		{"image/png", http.StatusOK},
		{"application/pdf", http.StatusOK},
		{"text/csv", http.StatusOK},
		{"APPLICATION/PDF", http.StatusOK},           // hoa thường không đổi kết quả
		{"text/plain; charset=utf-8", http.StatusOK}, // tham số cũng vậy
		{"application/x-msdownload", http.StatusBadRequest},
		{"text/html", http.StatusBadRequest},
		{"image/svg+xml", http.StatusBadRequest},
		{"", http.StatusBadRequest},
	}

	for _, tc := range cases {
		t.Run(tc.contentType, func(t *testing.T) {
			h := newProjectHarness()
			h.withStorage()
			_, task := seedTask(h, owner)

			_, err := h.uc.RequestAttachmentUpload(
				context.Background(), actorIn(owner), task.ID,
				"tai-lieu.pdf", tc.contentType)

			if got := statusOf(err); got != tc.want {
				t.Errorf("mã lỗi = %d, muốn %d", got, tc.want)
			}
		})
	}
}

func TestRequestAttachmentUploadRejectsBlankFileName(t *testing.T) {
	owner := uuid.New()
	h := newProjectHarness()
	h.withStorage()
	_, task := seedTask(h, owner)

	_, err := h.uc.RequestAttachmentUpload(
		context.Background(), actorIn(owner), task.ID, "   ", "image/png")

	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
}

// TestAttachmentKeyIsScopedToTask: bước xác nhận dựa vào chính tiền tố này
// để biết tệp thuộc về công việc nào.
func TestAttachmentKeyIsScopedToTask(t *testing.T) {
	owner := uuid.New()
	h := newProjectHarness()
	h.withStorage()
	_, task := seedTask(h, owner)

	got, err := h.uc.RequestAttachmentUpload(
		context.Background(), actorIn(owner), task.ID,
		"ban-thiet-ke.pdf", "application/pdf")
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	want := "tasks/" + task.ID.String() + "/"
	if !strings.HasPrefix(got.Key, want) {
		t.Errorf("khoá = %q, phải bắt đầu bằng %q", got.Key, want)
	}
	if !strings.HasSuffix(got.Key, ".pdf") {
		t.Errorf("khoá = %q, phải giữ đuôi .pdf để R2 trả đúng Content-Type",
			got.Key)
	}
}

// TestAttachmentKeyIsUnique: hai người tải lên cùng tên tệp không được ghi
// đè lên nhau.
func TestAttachmentKeyIsUnique(t *testing.T) {
	owner := uuid.New()
	h := newProjectHarness()
	h.withStorage()
	_, task := seedTask(h, owner)

	first, _ := h.uc.RequestAttachmentUpload(context.Background(),
		actorIn(owner), task.ID, "anh.png", "image/png")
	second, _ := h.uc.RequestAttachmentUpload(context.Background(),
		actorIn(owner), task.ID, "anh.png", "image/png")

	if first.Key == second.Key {
		t.Error("hai lần tải cùng tên tệp cho ra cùng khoá — sẽ ghi đè lên nhau")
	}
}

// TestAttachmentKeyDropsWeirdExtension: đuôi dài bất thường bị bỏ đi, vì nó
// đi vào khoá lưu trữ và qua CDN.
func TestAttachmentKeyDropsWeirdExtension(t *testing.T) {
	owner := uuid.New()
	h := newProjectHarness()
	h.withStorage()
	_, task := seedTask(h, owner)

	got, err := h.uc.RequestAttachmentUpload(context.Background(),
		actorIn(owner), task.ID,
		"tep."+strings.Repeat("x", 30), "application/pdf")
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if strings.Contains(got.Key, "xxxxx") {
		t.Errorf("khoá = %q, đuôi bất thường phải bị bỏ", got.Key)
	}
}

func TestRequestAttachmentUploadWithoutStorageIsUnprocessable(t *testing.T) {
	owner := uuid.New()
	h := newProjectHarness() // storage = nil
	_, task := seedTask(h, owner)

	_, err := h.uc.RequestAttachmentUpload(context.Background(),
		actorIn(owner), task.ID, "anh.png", "image/png")

	if got := statusOf(err); got != http.StatusUnprocessableEntity {
		t.Errorf("mã lỗi = %d, muốn 422", got)
	}
}

func TestViewerCannotUploadAttachment(t *testing.T) {
	owner := uuid.New()
	viewer := uuid.New()

	h := newProjectHarness()
	h.withStorage()
	p, task := seedTask(h, owner)
	h.members.join(p.ID, viewer, domainproject.RoleViewer)

	_, err := h.uc.RequestAttachmentUpload(context.Background(),
		actorIn(viewer), task.ID, "anh.png", "image/png")

	if got := statusOf(err); got != http.StatusForbidden {
		t.Errorf("mã lỗi = %d, muốn 403", got)
	}
}

// =========================================================================
// XÁC NHẬN
// =========================================================================

// TestConfirmAttachmentRejectsOtherTasksKey.
//
// Thiếu kiểm tra này, ai đó gọi confirm với khoá của task khác và đính kèm
// tệp của dự án họ không được xem — hoặc trỏ vào tệp bất kỳ trong bucket,
// kể cả ảnh đại diện của người khác.
func TestConfirmAttachmentRejectsOtherTasksKey(t *testing.T) {
	owner := uuid.New()
	h := newProjectHarness()
	store := h.withStorage()
	_, task := seedTask(h, owner)

	foreignKey := "tasks/" + uuid.NewString() + "/bi-mat.pdf"
	store.put(foreignKey, 1024, "application/pdf")

	_, err := h.uc.ConfirmAttachment(context.Background(), actorIn(owner),
		task.ID, foreignKey, "bi-mat.pdf")

	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
	if len(h.attachments.created) != 0 {
		t.Error("đã đính kèm tệp của công việc khác")
	}
}

// TestConfirmAttachmentRejectsDisguisedFile là lý do tồn tại của cả bước
// xác nhận: client khai "application/pdf" lúc xin URL rồi PUT lên một tệp
// thực thi. Chỉ đọc nội dung thật mới phát hiện được.
func TestConfirmAttachmentRejectsDisguisedFile(t *testing.T) {
	owner := uuid.New()
	h := newProjectHarness()
	store := h.withStorage()
	_, task := seedTask(h, owner)

	key := "tasks/" + task.ID.String() + "/tra-hinh.pdf"
	store.put(key, 1024, "application/x-msdownload")

	_, err := h.uc.ConfirmAttachment(context.Background(), actorIn(owner),
		task.ID, key, "tra-hinh.pdf")

	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
	if len(store.deleted) != 1 || store.deleted[0] != key {
		t.Errorf("tệp bị từ chối phải được xoá, đã xoá = %v", store.deleted)
	}
}

func TestConfirmAttachmentRejectsOversizeAndEmpty(t *testing.T) {
	owner := uuid.New()

	cases := []struct {
		name string
		size int64
	}{
		{"tệp rỗng", 0},
		{"vượt 20MB", maxAttachmentBytes + 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newProjectHarness()
			store := h.withStorage()
			_, task := seedTask(h, owner)

			key := "tasks/" + task.ID.String() + "/tep.pdf"
			store.put(key, tc.size, "application/pdf")

			_, err := h.uc.ConfirmAttachment(context.Background(),
				actorIn(owner), task.ID, key, "tep.pdf")

			if got := statusOf(err); got != http.StatusBadRequest {
				t.Errorf("mã lỗi = %d, muốn 400", got)
			}
			if len(store.deleted) != 1 {
				t.Errorf("tệp bị từ chối phải được xoá, đã xoá = %v", store.deleted)
			}
		})
	}
}

func TestConfirmAttachmentRejectsMissingFile(t *testing.T) {
	owner := uuid.New()
	h := newProjectHarness()
	h.withStorage()
	_, task := seedTask(h, owner)

	_, err := h.uc.ConfirmAttachment(context.Background(), actorIn(owner),
		task.ID, "tasks/"+task.ID.String()+"/khong-co.pdf", "khong-co.pdf")

	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
}

// TestConfirmAttachmentStoresDetectedType: kiểu lưu vào database là kiểu
// ĐOÁN TỪ NỘI DUNG, không phải kiểu client khai.
func TestConfirmAttachmentStoresDetectedType(t *testing.T) {
	owner := uuid.New()
	h := newProjectHarness()
	store := h.withStorage()
	_, task := seedTask(h, owner)

	key := "tasks/" + task.ID.String() + "/anh.png"
	store.put(key, 2048, "image/png")

	got, err := h.uc.ConfirmAttachment(context.Background(), actorIn(owner),
		task.ID, key, "anh.png")
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if got.ContentType != "image/png" {
		t.Errorf("kiểu tệp = %q, muốn image/png", got.ContentType)
	}
	if got.SizeBytes != 2048 {
		t.Errorf("kích thước = %d, muốn 2048", got.SizeBytes)
	}
	if got.UploadedBy != owner {
		t.Errorf("người tải lên = %v, muốn %v", got.UploadedBy, owner)
	}
}

// =========================================================================
// XOÁ
// =========================================================================

// TestOnlyUploaderOrOwnerCanDeleteAttachment.
func TestOnlyUploaderOrOwnerCanDeleteAttachment(t *testing.T) {
	owner := uuid.New()
	uploader := uuid.New()
	other := uuid.New()

	setup := func() (*projectHarness, *fakeProjectStorage, uuid.UUID) {
		h := newProjectHarness()
		store := h.withStorage()
		p, task := seedTask(h, owner)
		h.members.join(p.ID, uploader, domainproject.RoleMember)
		h.members.join(p.ID, other, domainproject.RoleMember)

		key := "tasks/" + task.ID.String() + "/tep.pdf"
		store.put(key, 1024, "application/pdf")
		a := h.attachments.add(&domainproject.Attachment{
			TaskID: task.ID, UploadedBy: uploader,
			StorageKey: key, FileName: "tep.pdf",
		})
		return h, store, a.ID
	}

	t.Run("người tải lên xoá được", func(t *testing.T) {
		h, store, id := setup()
		if err := h.uc.DeleteAttachment(
			context.Background(), actorIn(uploader), id,
		); err != nil {
			t.Fatalf("lỗi không mong đợi: %v", err)
		}
		if len(store.deleted) != 1 {
			t.Error("tệp trên R2 chưa được dọn")
		}
	})

	t.Run("chủ dự án xoá được", func(t *testing.T) {
		h, _, id := setup()
		if err := h.uc.DeleteAttachment(
			context.Background(), actorIn(owner), id,
		); err != nil {
			t.Fatalf("lỗi không mong đợi: %v", err)
		}
	})

	t.Run("thành viên khác không xoá được", func(t *testing.T) {
		h, store, id := setup()
		err := h.uc.DeleteAttachment(context.Background(), actorIn(other), id)

		if got := statusOf(err); got != http.StatusForbidden {
			t.Errorf("mã lỗi = %d, muốn 403", got)
		}
		if len(h.attachments.deleted) != 0 || len(store.deleted) != 0 {
			t.Error("đã xoá tệp của người khác")
		}
	})
}

func TestDeleteMissingAttachmentReturns404(t *testing.T) {
	h := newProjectHarness()
	h.withStorage()

	err := h.uc.DeleteAttachment(
		context.Background(), actorIn(uuid.New()), uuid.New())

	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
}

// =========================================================================
// ĐỌC
// =========================================================================

func TestListAttachments(t *testing.T) {
	owner := uuid.New()
	h := newProjectHarness()
	h.withStorage()
	_, task := seedTask(h, owner)
	h.attachments.add(&domainproject.Attachment{
		TaskID: task.ID, UploadedBy: owner, FileName: "tep.pdf",
	})

	got, err := h.uc.ListAttachments(context.Background(), actorIn(owner), task.ID)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("số tệp = %d, muốn 1", len(got))
	}
}

func TestListAttachmentsOfTaskYouCannotSeeReturns404(t *testing.T) {
	h := newProjectHarness()
	h.withStorage()
	_, task := seedTask(h, uuid.New())

	_, err := h.uc.ListAttachments(
		context.Background(), actorIn(uuid.New()), task.ID)

	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
}

// TestAttachmentURLDegradesQuietly: không ký được một URL thì ẩn nút tải về,
// còn hơn làm hỏng cả màn hình chi tiết công việc vì một tệp.
func TestAttachmentURLDegradesQuietly(t *testing.T) {
	h := newProjectHarness()
	store := h.withStorage()
	store.presignGetErr = errors.New("R2 sập")

	if got := h.uc.AttachmentURL(context.Background(), "tasks/x/y.pdf"); got != "" {
		t.Errorf("URL = %q, muốn rỗng", got)
	}
}

func TestAttachmentURLEmptyCases(t *testing.T) {
	t.Run("khoá rỗng", func(t *testing.T) {
		h := newProjectHarness()
		h.withStorage()
		if got := h.uc.AttachmentURL(context.Background(), ""); got != "" {
			t.Errorf("URL = %q, muốn rỗng", got)
		}
	})

	t.Run("chưa cấu hình lưu trữ", func(t *testing.T) {
		h := newProjectHarness()
		if got := h.uc.AttachmentURL(context.Background(), "tasks/x/y.pdf"); got != "" {
			t.Errorf("URL = %q, muốn rỗng", got)
		}
	})
}

// =========================================================================
// TIỆN ÍCH
// =========================================================================

func TestNormalizeContentType(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"image/png", "image/png"},
		{"IMAGE/PNG", "image/png"},
		{"  text/plain  ", "text/plain"},
		{"text/plain; charset=utf-8", "text/plain"},
		{"", ""},
	}

	for _, tc := range cases {
		if got := normalizeContentType(tc.in); got != tc.want {
			t.Errorf("normalizeContentType(%q) = %q, muốn %q", tc.in, got, tc.want)
		}
	}
}

// TestSanitizeFileName: tên tệp không dùng làm khoá lưu trữ, nhưng nó là
// thứ trình duyệt dùng khi người dùng bấm tải về.
func TestSanitizeFileName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"tai-lieu.pdf", "tai-lieu.pdf"},
		{"  tai-lieu.pdf  ", "tai-lieu.pdf"},
		{"../../etc/passwd", "passwd"},
		{`C:\Users\a\tep.docx`, "tep.docx"},
		{"thu-muc/tep.png", "tep.png"},
		{"", "tep-dinh-kem"},
		{".", "tep-dinh-kem"},
		{"..", "tep-dinh-kem"},
		{strings.Repeat("a", 300), strings.Repeat("a", 255)},
	}

	for _, tc := range cases {
		if got := sanitizeFileName(tc.in); got != tc.want {
			t.Errorf("sanitizeFileName(%q) = %q, muốn %q", tc.in, got, tc.want)
		}
	}
}

// Ràng buộc kiểu: bản giả lập phải khớp cổng lưu trữ ở tầng domain.
var _ domainproject.FileStorage = (*fakeProjectStorage)(nil)
