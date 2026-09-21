package project

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	domainproject "github.com/PhamVanPhuc2k2/manage/internal/domain/project"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
	"github.com/PhamVanPhuc2k2/manage/pkg/logger"
)

const (
	// 20MB. Rộng hơn ảnh đại diện nhiều vì đây là tài liệu công việc: bản
	// thiết kế, tài liệu đặc tả, ảnh chụp màn hình lỗi.
	maxAttachmentBytes = 20 << 20

	attachmentUploadTTL = 10 * time.Minute
	attachmentViewTTL   = time.Hour
)

// Kiểu tệp CHO PHÉP, so khớp với kết quả đoán từ NỘI DUNG thật.
//
// Danh sách trắng chứ không phải danh sách đen: danh sách đen luôn thiếu một
// thứ gì đó, và thứ thiếu đó là thứ sẽ được dùng để tấn công.
var allowedAttachmentTypes = map[string]struct{}{
	"image/jpeg":      {},
	"image/png":       {},
	"image/webp":      {},
	"image/gif":       {},
	"application/pdf": {},
	"text/plain":      {},
	"text/csv":        {},
	"application/zip": {},
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document":   {},
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         {},
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": {},
	"application/msword":            {},
	"application/vnd.ms-excel":      {},
	"application/vnd.ms-powerpoint": {},
}

type AttachmentTicket struct {
	UploadURL string `json:"upload_url"`
	Key       string `json:"key"`
	ExpiresIn int    `json:"expires_in"`
}

// RequestAttachmentUpload cấp URL để client tải tệp THẲNG lên R2.
//
// Cùng luồng ba bước với ảnh đại diện: xin URL, PUT lên R2, gọi confirm.
// Bước confirm là bắt buộc — presigned URL chỉ cho phép ghi, nó KHÔNG kiểm
// tra nội dung. Thiếu bước đó, ai có URL cũng tải lên được tệp thực thi.
func (u *Usecase) RequestAttachmentUpload(
	ctx context.Context,
	actor *domainauth.Actor,
	taskID uuid.UUID,
	fileName, contentType string,
) (*AttachmentTicket, error) {
	fileName = strings.TrimSpace(fileName)
	if fileName == "" {
		return nil, apperror.Invalid("Thiếu tên tệp", nil)
	}
	if _, ok := allowedAttachmentTypes[normalizeContentType(contentType)]; !ok {
		return nil, apperror.Invalid(
			"Loại tệp không được hỗ trợ. Chấp nhận ảnh, PDF, văn bản, bảng tính, trình chiếu và ZIP.", nil)
	}
	if u.storage == nil {
		return nil, apperror.New(apperror.KindUnprocessable,
			"Chức năng tải tệp chưa được cấu hình. Liên hệ bộ phận kỹ thuật.")
	}

	t, err := u.GetTask(ctx, actor, taskID)
	if err != nil {
		return nil, err
	}
	acc, err := u.loadAccess(ctx, actor, t.ProjectID)
	if err != nil {
		return nil, err
	}
	if !acc.canWrite() {
		return nil, apperror.Forbidden("Bạn chỉ có quyền xem dự án này")
	}

	// Khoá có thành phần ngẫu nhiên: hai người tải lên cùng tên tệp sẽ không
	// ghi đè lên nhau. Giữ đuôi tệp để R2 trả đúng Content-Type khi tải về.
	ext := filepath.Ext(fileName)
	if len(ext) > 10 {
		ext = "" // đuôi bất thường, bỏ đi cho an toàn
	}
	key := fmt.Sprintf("tasks/%s/%s%s", taskID, uuid.NewString(), ext)

	url, err := u.storage.PresignPut(ctx, key, contentType, attachmentUploadTTL)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	return &AttachmentTicket{
		UploadURL: url,
		Key:       key,
		ExpiresIn: int(attachmentUploadTTL.Seconds()),
	}, nil
}

// ConfirmAttachment kiểm tra tệp vừa tải lên rồi mới ghi vào database.
func (u *Usecase) ConfirmAttachment(
	ctx context.Context,
	actor *domainauth.Actor,
	taskID uuid.UUID,
	key, fileName string,
) (*domainproject.Attachment, error) {
	if u.storage == nil {
		return nil, apperror.New(apperror.KindUnprocessable,
			"Chức năng tải tệp chưa được cấu hình.")
	}
	log := logger.FromContext(ctx)

	t, err := u.GetTask(ctx, actor, taskID)
	if err != nil {
		return nil, err
	}
	acc, err := u.loadAccess(ctx, actor, t.ProjectID)
	if err != nil {
		return nil, err
	}
	if !acc.canWrite() {
		return nil, apperror.Forbidden("Bạn chỉ có quyền xem dự án này")
	}

	// Khoá phải nằm đúng thư mục của task này.
	//
	// Thiếu kiểm tra này, ai đó gọi confirm với khoá của task khác và đính
	// kèm tệp của dự án họ không được xem — hoặc trỏ vào tệp bất kỳ trong
	// bucket, kể cả ảnh đại diện của người khác.
	wantPrefix := fmt.Sprintf("tasks/%s/", taskID)
	if !strings.HasPrefix(key, wantPrefix) {
		return nil, apperror.Invalid("Khoá tệp không hợp lệ", nil)
	}

	size, _, err := u.storage.Stat(ctx, key)
	if err != nil {
		return nil, apperror.Invalid("Không tìm thấy tệp vừa tải lên", nil)
	}
	if size == 0 {
		_ = u.storage.Delete(ctx, key)
		return nil, apperror.Invalid("Tệp rỗng", nil)
	}
	if size > maxAttachmentBytes {
		_ = u.storage.Delete(ctx, key)
		return nil, apperror.Invalid("Tệp vượt quá 20MB", nil)
	}

	// Kiểm tra kiểu tệp bằng NỘI DUNG THẬT. Content-Type client khai lúc xin
	// URL chỉ là lời khai, sửa được tuỳ ý.
	detected, err := u.storage.DetectContentType(ctx, key)
	if err != nil {
		return nil, apperror.Internal(err)
	}
	detected = normalizeContentType(detected)

	if _, ok := allowedAttachmentTypes[detected]; !ok {
		_ = u.storage.Delete(ctx, key)
		log.Warn().
			Str("task_id", taskID.String()).
			Str("detected", detected).
			Msg("từ chối tệp đính kèm không hợp lệ")
		return nil, apperror.Invalid("Nội dung tệp không thuộc loại được hỗ trợ", nil)
	}

	a := &domainproject.Attachment{
		TaskID:      taskID,
		UploadedBy:  actor.EmployeeID,
		StorageKey:  key,
		FileName:    sanitizeFileName(fileName),
		ContentType: detected,
		SizeBytes:   size,
	}
	if err := u.attachments.Create(ctx, a); err != nil {
		_ = u.storage.Delete(ctx, key)
		return nil, apperror.Internal(err)
	}

	u.logActivity(ctx, &domainproject.Activity{
		TaskID:   taskID,
		ActorID:  actorEmployeeID(actor),
		Action:   domainproject.ActionAttachmentAdded,
		NewValue: a.FileName,
	})

	return u.attachments.GetByID(ctx, a.ID)
}

func (u *Usecase) ListAttachments(
	ctx context.Context,
	actor *domainauth.Actor,
	taskID uuid.UUID,
) ([]*domainproject.Attachment, error) {
	if _, err := u.GetTask(ctx, actor, taskID); err != nil {
		return nil, err
	}
	list, err := u.attachments.ListByTask(ctx, taskID)
	if err != nil {
		return nil, apperror.Internal(err)
	}
	return list, nil
}

// AttachmentURL sinh liên kết tải tệp.
//
// KHÔNG lưu URL vào database: presigned URL có hạn, lưu lại thì ít lâu sau
// là hỏng. Database chỉ giữ khoá object, URL ký lại mỗi lần đọc.
func (u *Usecase) AttachmentURL(ctx context.Context, key string) string {
	if key == "" || u.storage == nil {
		return ""
	}
	url, err := u.storage.PresignGet(ctx, key, attachmentViewTTL)
	if err != nil {
		// Nuốt lỗi có chủ ý: không ký được một URL thì ẩn nút tải về, còn
		// hơn làm hỏng cả màn hình chi tiết công việc vì một tệp.
		return ""
	}
	return url
}

func (u *Usecase) DeleteAttachment(
	ctx context.Context,
	actor *domainauth.Actor,
	attachmentID uuid.UUID,
) error {
	a, err := u.attachments.GetByID(ctx, attachmentID)
	if err != nil {
		return apperror.NotFound("tệp đính kèm")
	}
	t, err := u.GetTask(ctx, actor, a.TaskID)
	if err != nil {
		return apperror.NotFound("tệp đính kèm")
	}
	acc, err := u.loadAccess(ctx, actor, t.ProjectID)
	if err != nil {
		return apperror.NotFound("tệp đính kèm")
	}
	if a.UploadedBy != actor.EmployeeID && !acc.canManage() {
		return apperror.Forbidden("Chỉ người tải lên hoặc chủ dự án mới xoá được tệp này")
	}

	// Xoá bản ghi TRƯỚC, xoá tệp sau.
	//
	// Thứ tự này quan trọng: nếu xoá tệp trước rồi ghi database lỗi, bản ghi
	// còn lại sẽ trỏ tới một tệp không tồn tại và người dùng thấy liên kết
	// hỏng. Ngược lại, tệp mồ côi trên R2 chỉ tốn ít dung lượng.
	if err := u.attachments.Delete(ctx, attachmentID); err != nil {
		return apperror.Internal(err)
	}
	if u.storage != nil {
		if err := u.storage.Delete(ctx, a.StorageKey); err != nil {
			log := logger.FromContext(ctx)
			log.Warn().Err(err).Str("key", a.StorageKey).
				Msg("không xoá được tệp trên R2, bản ghi đã gỡ")
		}
	}

	u.logActivity(ctx, &domainproject.Activity{
		TaskID:   a.TaskID,
		ActorID:  actorEmployeeID(actor),
		Action:   domainproject.ActionAttachmentDel,
		OldValue: a.FileName,
	})
	return nil
}

func normalizeContentType(ct string) string {
	// Content-Type có thể kèm tham số, ví dụ "text/plain; charset=utf-8".
	return strings.ToLower(strings.TrimSpace(strings.Split(ct, ";")[0]))
}

// sanitizeFileName giữ lại tên gốc để hiển thị nhưng bỏ mọi thành phần
// đường dẫn.
//
// Tên tệp KHÔNG được dùng làm khoá lưu trữ (khoá do server sinh), nên đây
// không phải chống path traversal ở tầng lưu trữ — mà là chống việc tên hiển
// thị chứa "../" rồi gây bất ngờ khi người dùng tải về.
func sanitizeFileName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "\\", "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	if name == "" || name == "." || name == ".." {
		return "tep-dinh-kem"
	}
	if len(name) > 255 {
		name = name[:255]
	}
	return name
}
