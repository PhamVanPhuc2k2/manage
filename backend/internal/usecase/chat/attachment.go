package chat

import (
	"context"
	"strings"

	"github.com/google/uuid"

	domainchat "github.com/PhamVanPhuc2k2/manage/internal/domain/chat"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
	"github.com/PhamVanPhuc2k2/manage/pkg/logger"
)

// allowedAttachmentTypes là danh sách trắng kiểu tệp gửi được trong chat.
//
// Danh sách TRẮNG, không phải danh sách đen. Danh sách đen luôn thiếu: mỗi
// định dạng thực thi mới xuất hiện là một lỗ hổng cho tới khi có người nhớ
// thêm nó vào.
//
// Không có ở đây, và cố ý:
//
//   - text/html và image/svg+xml — cả hai chạy được JavaScript khi mở. Trình
//     duyệt hiển thị chúng nội tuyến, nên một tệp như vậy là XCS lưu trữ nhắm
//     vào chính người nhận tin nhắn.
//   - application/x-msdownload, x-sh và mọi thứ thực thi được.
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

// normalizeContentType bỏ tham số khỏi Content-Type ("text/plain; charset=utf-8").
func normalizeContentType(ct string) string {
	return strings.ToLower(strings.TrimSpace(strings.Split(ct, ";")[0]))
}

// zipBasedTypes là những định dạng mà nội dung thật sự LÀ một tệp zip.
//
// docx, xlsx và pptx đều là zip đổi tên. http.DetectContentType nhìn vào
// magic bytes nên nó luôn trả "application/zip" cho cả ba — không có cách nào
// phân biệt bằng 512 byte đầu.
//
// Vì vậy với những kiểu này ta chấp nhận lời khai của client KHI VÀ CHỈ KHI
// nội dung thật đúng là zip. Nghe có vẻ lỏng, nhưng nó không mở ra lỗ hổng
// nào: thứ nguy hiểm là tệp thực thi và tệp chạy script, và cả hai đều không
// phải zip nên vẫn bị chặn ở bước kiểm tra nội dung.
var zipBasedTypes = map[string]struct{}{
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document":   {},
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         {},
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": {},
	"application/zip": {},
}

// verifyAttachment kiểm tra một tệp đã nằm trên R2 và trả về kiểu tệp THẬT.
//
// Phải kiểm tra ở đây vì client tải tệp THẲNG lên R2, không qua api. Mọi thứ
// client gửi kèm lúc gắn tệp vào tin nhắn — kích thước, kiểu tệp — đều chỉ là
// lời khai, và sửa được bằng một lần chỉnh request.
//
// Tệp không đạt bị XOÁ khỏi R2 ngay. Để lại thì nó vẫn nằm đó với một URL ký
// được, và người tải lên vẫn chia sẻ link cho người khác được — việc từ chối
// gắn vào tin nhắn không ngăn được điều đó.
func (u *Usecase) verifyAttachment(
	ctx context.Context,
	conversationID uuid.UUID,
	in AttachmentInput,
) (string, int64, error) {
	log := logger.FromContext(ctx)

	// Khoá phải nằm trong thư mục của ĐÚNG hội thoại này.
	//
	// Thiếu bước này, một người gắn được tệp của hội thoại khác vào tin nhắn
	// của mình chỉ bằng cách gửi lên một khoá đoán được — và tệp đó sẽ được
	// ký URL cho mọi thành viên hội thoại mới xem.
	wantPrefix := "chat/" + conversationID.String() + "/"
	if !strings.HasPrefix(in.StorageKey, wantPrefix) {
		return "", 0, apperror.Invalid("Khoá tệp không thuộc hội thoại này", nil)
	}

	size, _, err := u.storage.Stat(ctx, in.StorageKey)
	if err != nil {
		return "", 0, apperror.Invalid("Không tìm thấy tệp vừa tải lên", nil)
	}
	if size <= 0 {
		_ = u.storage.Delete(ctx, in.StorageKey)
		return "", 0, apperror.Invalid("Tệp rỗng", nil)
	}
	if size > maxAttachmentSize {
		_ = u.storage.Delete(ctx, in.StorageKey)
		return "", 0, apperror.Invalid("Tệp vượt quá giới hạn kích thước", nil)
	}

	detected, err := u.storage.DetectContentType(ctx, in.StorageKey)
	if err != nil {
		return "", 0, apperror.Internal(err)
	}
	detected = normalizeContentType(detected)

	if _, ok := allowedAttachmentTypes[detected]; !ok {
		_ = u.storage.Delete(ctx, in.StorageKey)
		log.Warn().
			Str("conversation_id", conversationID.String()).
			Str("declared", normalizeContentType(in.ContentType)).
			Str("detected", detected).
			Msg("từ chối tệp đính kèm: nội dung không thuộc loại được hỗ trợ")
		return "", 0, apperror.Invalid("Nội dung tệp không thuộc loại được hỗ trợ", nil)
	}

	// Tệp Office: giữ lời khai của client nếu nó hợp lệ và nội dung là zip.
	// Nếu không, dùng kiểu phát hiện được — lời khai không bao giờ thắng.
	claimed := normalizeContentType(in.ContentType)
	if _, isZip := zipBasedTypes[detected]; isZip {
		if _, allowed := allowedAttachmentTypes[claimed]; allowed {
			if _, claimedIsZip := zipBasedTypes[claimed]; claimedIsZip {
				return claimed, size, nil
			}
		}
	}

	return detected, size, nil
}

// saveAttachments kiểm tra rồi ghi tệp đính kèm của một tin nhắn.
//
// Kích thước và kiểu tệp lấy từ R2, KHÔNG lấy từ dữ liệu client gửi lên.
func (u *Usecase) saveAttachments(
	ctx context.Context,
	m *domainchat.Message,
	items []AttachmentInput,
) error {
	if len(items) == 0 {
		return nil
	}
	if u.storage == nil {
		return apperror.New(apperror.KindUnprocessable,
			"Chức năng tải tệp chưa được cấu hình")
	}

	atts := make([]*domainchat.Attachment, 0, len(items))
	for _, it := range items {
		if it.StorageKey == "" || it.FileName == "" {
			return apperror.Invalid("Tệp đính kèm thiếu thông tin", nil)
		}

		contentType, size, err := u.verifyAttachment(ctx, m.ConversationID, it)
		if err != nil {
			return err
		}

		atts = append(atts, &domainchat.Attachment{
			MessageID:   m.ID,
			StorageKey:  it.StorageKey,
			FileName:    sanitizeName(it.FileName),
			ContentType: contentType,
			SizeBytes:   size,
			Width:       it.Width,
			Height:      it.Height,
		})
	}
	return u.msgs.AddAttachments(ctx, atts)
}
