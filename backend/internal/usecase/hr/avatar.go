package hr

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
	"github.com/PhamVanPhuc2k2/manage/pkg/logger"
)

const (
	// 2MB là quá đủ cho ảnh đại diện. Giới hạn này được kiểm tra SAU KHI tải
	// lên (xem ConfirmAvatar) vì presigned URL không ép được kích thước.
	maxAvatarBytes = 2 << 20

	uploadURLTTL = 5 * time.Minute
	viewURLTTL   = time.Hour
)

// Kiểu ảnh chấp nhận. So khớp với kết quả đoán từ NỘI DUNG tệp,
// không phải với header do client khai báo.
var allowedAvatarTypes = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
}

type AvatarUploadTicket struct {
	UploadURL string `json:"upload_url"`
	Key       string `json:"key"`
	ExpiresIn int    `json:"expires_in"`
}

// RequestAvatarUpload cấp URL để client tải ảnh THẲNG lên R2.
//
// Luồng ba bước:
//  1. Client gọi hàm này, nhận URL có chữ ký
//  2. Client PUT thẳng lên R2 bằng URL đó
//  3. Client gọi ConfirmAvatar để server kiểm tra rồi mới ghi vào database
//
// Bước 3 là bắt buộc: presigned URL chỉ cho phép ghi, nó KHÔNG kiểm tra nội
// dung. Thiếu bước xác nhận, ai đó có thể tải lên một tệp thực thi rồi gán
// nó làm ảnh đại diện.
func (u *Usecase) RequestAvatarUpload(
	ctx context.Context,
	actor *domainauth.Actor,
	employeeID uuid.UUID,
	contentType string,
) (*AvatarUploadTicket, error) {
	// Kiểm tra dữ liệu vào TRƯỚC khi kiểm tra cấu hình hệ thống.
	//
	// Ngược lại thì người gửi sai kiểu tệp sẽ nhận thông báo "chưa cấu hình",
	// không liên quan gì tới lỗi thật của họ.
	ext, ok := allowedAvatarTypes[strings.ToLower(strings.TrimSpace(contentType))]
	if !ok {
		return nil, apperror.Invalid("Chỉ chấp nhận ảnh JPEG, PNG hoặc WebP", nil)
	}

	if u.storage == nil {
		return nil, apperror.New(apperror.KindUnprocessable,
			"Chức năng tải tệp chưa được cấu hình. Liên hệ bộ phận kỹ thuật.")
	}

	emp, err := u.employees.GetByID(ctx, employeeID)
	if err != nil {
		return nil, apperror.NotFound("nhân viên")
	}
	if !actor.CanSeeEmployee(emp.ID, emp.DepartmentID) {
		return nil, apperror.NotFound("nhân viên")
	}

	// Khoá có thành phần ngẫu nhiên để lần tải mới không ghi đè lần cũ ngay
	// lập tức — nhờ vậy nếu bước xác nhận lỗi thì ảnh cũ vẫn còn nguyên.
	key := fmt.Sprintf("avatars/%s/%s%s", employeeID, uuid.NewString(), ext)

	url, err := u.storage.PresignPut(ctx, key, contentType, uploadURLTTL)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	return &AvatarUploadTicket{
		UploadURL: url,
		Key:       key,
		ExpiresIn: int(uploadURLTTL.Seconds()),
	}, nil
}

// ConfirmAvatar kiểm tra tệp vừa tải lên rồi mới gắn vào hồ sơ.
func (u *Usecase) ConfirmAvatar(
	ctx context.Context,
	actor *domainauth.Actor,
	employeeID uuid.UUID,
	key string,
) (string, error) {
	if u.storage == nil {
		return "", apperror.New(apperror.KindUnprocessable,
			"Chức năng tải tệp chưa được cấu hình.")
	}
	log := logger.FromContext(ctx)

	emp, err := u.employees.GetByID(ctx, employeeID)
	if err != nil {
		return "", apperror.NotFound("nhân viên")
	}
	if !actor.CanSeeEmployee(emp.ID, emp.DepartmentID) {
		return "", apperror.NotFound("nhân viên")
	}

	// Khoá phải nằm đúng thư mục của nhân viên này.
	//
	// Thiếu kiểm tra này, ai đó có thể gọi confirm với khoá của người khác
	// và gán ảnh của họ — hoặc tệ hơn, trỏ vào một tệp bất kỳ trong bucket.
	wantPrefix := fmt.Sprintf("avatars/%s/", employeeID)
	if !strings.HasPrefix(key, wantPrefix) {
		return "", apperror.Invalid("Khoá tệp không hợp lệ", nil)
	}

	size, _, err := u.storage.Stat(ctx, key)
	if err != nil {
		return "", apperror.Invalid("Không tìm thấy tệp vừa tải lên", nil)
	}
	if size > maxAvatarBytes {
		_ = u.storage.Delete(ctx, key)
		return "", apperror.Invalid("Ảnh vượt quá 2MB", nil)
	}
	if size == 0 {
		_ = u.storage.Delete(ctx, key)
		return "", apperror.Invalid("Tệp rỗng", nil)
	}

	// Kiểm tra kiểu tệp bằng NỘI DUNG THẬT.
	//
	// Content-Type do client gửi lúc xin URL chỉ là lời khai, sửa được tuỳ ý.
	// Đây là chỗ duy nhất biết chắc tệp là gì.
	detected, err := u.storage.DetectContentType(ctx, key)
	if err != nil {
		return "", apperror.Internal(err)
	}
	// DetectContentType có thể kèm charset, ví dụ "text/plain; charset=utf-8".
	detected = strings.TrimSpace(strings.Split(detected, ";")[0])

	if _, ok := allowedAvatarTypes[detected]; !ok {
		_ = u.storage.Delete(ctx, key)
		log.Warn().
			Str("employee_id", employeeID.String()).
			Str("detected", detected).
			Msg("từ chối tệp không phải ảnh")
		return "", apperror.Invalid(
			"Tệp tải lên không phải ảnh hợp lệ (JPEG, PNG hoặc WebP)", nil)
	}

	oldKey := emp.AvatarKey
	if err := u.employees.UpdateAvatarKey(ctx, employeeID, key); err != nil {
		return "", apperror.Internal(err)
	}

	// Dọn ảnh cũ SAU KHI database đã ghi thành công. Xoá trước mà ghi lỗi
	// thì nhân viên mất ảnh mà không được gì.
	if oldKey != "" && oldKey != key {
		if err := u.storage.Delete(ctx, oldKey); err != nil {
			// Chỉ tốn dung lượng, không ảnh hưởng người dùng.
			log.Warn().Err(err).Str("key", oldKey).Msg("không xoá được ảnh cũ")
		}
	}

	return u.AvatarURL(ctx, key)
}

// AvatarURL sinh liên kết xem ảnh.
//
// KHÔNG lưu URL vào database: presigned URL có hạn, lưu lại thì chỉ ít lâu
// sau là hỏng. Database chỉ giữ khoá object, URL sinh lại mỗi lần đọc.
func (u *Usecase) AvatarURL(ctx context.Context, key string) (string, error) {
	if key == "" || u.storage == nil {
		return "", nil
	}
	url, err := u.storage.PresignGet(ctx, key, viewURLTTL)
	if err != nil {
		// Nuốt lỗi CÓ CHỦ Ý: không ký được URL ảnh thì hiển thị ảnh mặc
		// định, còn hơn làm hỏng cả trang danh sách nhân viên vì một ảnh.
		//
		//nolint:nilerr // suy giảm êm có chủ ý
		return "", nil
	}
	return url, nil
}

// RemoveAvatar gỡ ảnh đại diện.
func (u *Usecase) RemoveAvatar(
	ctx context.Context,
	actor *domainauth.Actor,
	employeeID uuid.UUID,
) error {
	emp, err := u.employees.GetByID(ctx, employeeID)
	if err != nil {
		return apperror.NotFound("nhân viên")
	}
	if !actor.CanSeeEmployee(emp.ID, emp.DepartmentID) {
		return apperror.NotFound("nhân viên")
	}
	if emp.AvatarKey == "" {
		return nil
	}

	if err := u.employees.UpdateAvatarKey(ctx, employeeID, ""); err != nil {
		return apperror.Internal(err)
	}
	if u.storage != nil {
		_ = u.storage.Delete(ctx, emp.AvatarKey)
	}
	return nil
}
