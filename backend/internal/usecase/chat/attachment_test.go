package chat

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	domainchat "github.com/PhamVanPhuc2k2/manage/internal/domain/chat"
)

// Bộ kiểm thử xác thực tệp đính kèm.
//
// Điểm cốt lõi: client tải tệp THẲNG lên R2, không qua api. Mọi thứ nó gửi
// kèm lúc gắn tệp vào tin nhắn — kích thước, kiểu tệp, tên — đều chỉ là lời
// khai và sửa được bằng một lần chỉnh request. Hàng rào duy nhất là đọc lại
// dữ liệu thật từ R2.

// attach dựng một tệp "đã nằm trên R2" rồi trả về input để gắn vào tin nhắn.
func attach(
	h *chatHarness,
	convID uuid.UUID,
	declared, detected string,
	size int64,
) AttachmentInput {
	key := "chat/" + convID.String() + "/" + uuid.NewString() + "/tep.bin"
	h.storage.stored[key] = storedObject{size: size, detected: detected}

	return AttachmentInput{
		StorageKey:  key,
		FileName:    "tep.bin",
		ContentType: declared,
		SizeBytes:   size,
	}
}

func sendWith(h *chatHarness, convID uuid.UUID, in AttachmentInput) error {
	_, err := h.uc.Send(context.Background(), actorOf(actorID(h, convID)), SendInput{
		ConversationID: convID,
		Kind:           domainchat.MessageFile,
		Attachments:    []AttachmentInput{in},
	})
	return err
}

// actorID lấy id thành viên đầu tiên của hội thoại.
func actorID(h *chatHarness, convID uuid.UUID) uuid.UUID {
	for id := range h.convs.members[convID] {
		return id
	}
	return uuid.Nil
}

// =========================================================================
// LỜI KHAI KHÔNG BAO GIỜ THẮNG NỘI DUNG
// =========================================================================

// TestAttachmentRejectsDisguisedExecutable là phép thử BẢO MẬT quan trọng
// nhất của tệp này.
//
// Kẻ tấn công tải lên một tệp thực thi rồi khai nó là "image/png". Nếu hệ
// thống tin lời khai thì tệp đó được ghi vào database với kiểu ảnh, và mọi
// người nhận sẽ thấy một "tấm ảnh" tải về là chạy được.
func TestAttachmentRejectsDisguisedExecutable(t *testing.T) {
	h, convID, _ := sendSetup()

	in := attach(h, convID,
		"image/png",                // client KHAI là ảnh
		"application/x-msdownload", // nội dung THẬT là tệp thực thi
		2048)

	err := sendWith(h, convID, in)
	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
	if len(h.msgs.atts) != 0 {
		t.Error("không được ghi tệp đính kèm")
	}

	// Và tệp phải bị XOÁ khỏi R2. Để lại thì nó vẫn ở đó với một URL ký được,
	// và người tải lên vẫn chia sẻ link cho người khác được — việc từ chối gắn
	// vào tin nhắn không ngăn được điều đó.
	if len(h.storage.deleted) != 1 {
		t.Errorf("xoá %d tệp khỏi R2, muốn 1", len(h.storage.deleted))
	}
}

// TestAttachmentRejectsHTMLAndSVG: cả hai chạy được JavaScript khi mở, và
// trình duyệt hiển thị chúng nội tuyến — tức là XSS lưu trữ nhắm thẳng vào
// người nhận tin nhắn.
func TestAttachmentRejectsHTMLAndSVG(t *testing.T) {
	for _, detected := range []string{
		"text/html", "text/html; charset=utf-8", "image/svg+xml",
	} {
		t.Run(detected, func(t *testing.T) {
			h, convID, _ := sendSetup()
			in := attach(h, convID, "text/plain", detected, 512)

			err := sendWith(h, convID, in)
			if got := statusOf(err); got != http.StatusBadRequest {
				t.Errorf("mã lỗi = %d, muốn 400", got)
			}
		})
	}
}

// TestAttachmentStoresDetectedType: kiểu ghi vào database phải là kiểu PHÁT
// HIỆN được, không phải kiểu client khai.
//
// Ngay cả khi cả hai đều nằm trong danh sách trắng: khai "application/pdf" cho
// một tệp ảnh sẽ khiến giao diện cố mở nó bằng trình xem PDF.
func TestAttachmentStoresDetectedType(t *testing.T) {
	h, convID, _ := sendSetup()

	in := attach(h, convID, "application/pdf", "image/png", 4096)

	if err := sendWith(h, convID, in); err != nil {
		t.Fatalf("Send lỗi: %v", err)
	}
	if len(h.msgs.atts) != 1 {
		t.Fatalf("ghi %d tệp, muốn 1", len(h.msgs.atts))
	}
	if got := h.msgs.atts[0].ContentType; got != "image/png" {
		t.Errorf("kiểu tệp đã ghi = %q, muốn kiểu phát hiện được \"image/png\"", got)
	}
}

// TestAttachmentStoresRealSize: kích thước ghi vào database lấy từ R2, không
// lấy từ lời khai.
//
// Khai kích thước nhỏ để lách giới hạn là cách lách đơn giản nhất, và con số
// sai còn làm giao diện hiện "2 KB" cho một tệp 20 MB.
func TestAttachmentStoresRealSize(t *testing.T) {
	h, convID, _ := sendSetup()

	in := attach(h, convID, "image/png", "image/png", 5_000_000)
	in.SizeBytes = 1024 // client KHAI nhỏ hơn thật rất nhiều

	if err := sendWith(h, convID, in); err != nil {
		t.Fatalf("Send lỗi: %v", err)
	}
	if got := h.msgs.atts[0].SizeBytes; got != 5_000_000 {
		t.Errorf("kích thước đã ghi = %d, muốn kích thước thật 5000000", got)
	}
}

// TestAttachmentRejectsOversizedByRealSize: khai nhỏ không lách được giới hạn.
func TestAttachmentRejectsOversizedByRealSize(t *testing.T) {
	h, convID, _ := sendSetup()

	in := attach(h, convID, "image/png", "image/png", maxAttachmentSize+1)
	in.SizeBytes = 1024 // lời khai nằm trong giới hạn

	err := sendWith(h, convID, in)
	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
	if len(h.storage.deleted) != 1 {
		t.Error("tệp quá lớn phải bị xoá khỏi R2")
	}
}

func TestAttachmentRejectsEmptyFile(t *testing.T) {
	h, convID, _ := sendSetup()

	in := attach(h, convID, "image/png", "image/png", 0)

	err := sendWith(h, convID, in)
	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
}

func TestAttachmentRejectsMissingFile(t *testing.T) {
	h, convID, _ := sendSetup()

	// Khoá đúng định dạng nhưng KHÔNG có tệp nào trên R2: client gửi lên một
	// khoá bịa ra.
	in := AttachmentInput{
		StorageKey:  "chat/" + convID.String() + "/bia/ra.png",
		FileName:    "ra.png",
		ContentType: "image/png",
		SizeBytes:   1024,
	}

	err := sendWith(h, convID, in)
	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
}

// =========================================================================
// KHOÁ PHẢI THUỘC ĐÚNG HỘI THOẠI
// =========================================================================

// TestAttachmentRejectsKeyFromAnotherConversation chống rò rỉ tệp giữa các
// hội thoại.
//
// Không kiểm tiền tố thì một người gắn được tệp của hội thoại khác vào tin
// nhắn của mình chỉ bằng cách gửi lên một khoá đoán được — và tệp đó sẽ được
// ký URL cho mọi thành viên hội thoại mới xem.
func TestAttachmentRejectsKeyFromAnotherConversation(t *testing.T) {
	h, convID, _ := sendSetup()

	otherConv := uuid.New()
	foreignKey := "chat/" + otherConv.String() + "/abc/bi-mat.pdf"
	h.storage.stored[foreignKey] = storedObject{size: 1024, detected: "application/pdf"}

	in := AttachmentInput{
		StorageKey:  foreignKey,
		FileName:    "bi-mat.pdf",
		ContentType: "application/pdf",
		SizeBytes:   1024,
	}

	err := sendWith(h, convID, in)
	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}

	// Và KHÔNG được xoá tệp của hội thoại kia — đó là dữ liệu thật của người
	// khác. Từ chối là đủ.
	for _, k := range h.storage.deleted {
		if k == foreignKey {
			t.Error("đã xoá tệp của hội thoại khác")
		}
	}
}

// TestAttachmentRejectsPathTraversalKey: khoá cố đi ra khỏi thư mục hội thoại.
func TestAttachmentRejectsPathTraversalKey(t *testing.T) {
	h, convID, _ := sendSetup()

	for _, key := range []string{
		"../../etc/passwd",
		"chat/../bi-mat.pdf",
		"payslips/luong-giam-doc.pdf",
		"",
	} {
		in := AttachmentInput{
			StorageKey: key, FileName: "x.pdf",
			ContentType: "application/pdf", SizeBytes: 100,
		}
		err := sendWith(h, convID, in)
		if got := statusOf(err); got != http.StatusBadRequest {
			t.Errorf("khoá %q: mã lỗi = %d, muốn 400", key, got)
		}
	}
}

// =========================================================================
// TỆP OFFICE
// =========================================================================

// TestAttachmentAcceptsOfficeFiles: docx, xlsx và pptx đều là zip đổi tên, nên
// magic bytes luôn trả "application/zip". Với những kiểu này ta chấp nhận lời
// khai KHI VÀ CHỈ KHI nội dung thật đúng là zip.
func TestAttachmentAcceptsOfficeFiles(t *testing.T) {
	office := map[string]string{
		"docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	}

	for name, mime := range office {
		t.Run(name, func(t *testing.T) {
			h, convID, _ := sendSetup()
			in := attach(h, convID, mime, "application/zip", 20_000)

			if err := sendWith(h, convID, in); err != nil {
				t.Fatalf("tệp %s phải gửi được: %v", name, err)
			}
			if got := h.msgs.atts[0].ContentType; got != mime {
				t.Errorf("kiểu đã ghi = %q, muốn %q", got, mime)
			}
		})
	}
}

// TestAttachmentZipClaimingOfficeStaysZip: khai một kiểu KHÔNG nằm trong nhóm
// zip cho một tệp zip thì phải dùng kiểu phát hiện được.
func TestAttachmentZipClaimingImageStaysZip(t *testing.T) {
	h, convID, _ := sendSetup()

	in := attach(h, convID, "image/png", "application/zip", 20_000)

	if err := sendWith(h, convID, in); err != nil {
		t.Fatalf("Send lỗi: %v", err)
	}
	if got := h.msgs.atts[0].ContentType; got != "application/zip" {
		t.Errorf("kiểu đã ghi = %q, muốn \"application/zip\"", got)
	}
}

// TestAttachmentExecutableClaimingOfficeIsRejected là phép thử chống lách
// nhánh ngoại lệ dành cho tệp Office.
//
// Nhánh đó chỉ áp dụng khi nội dung THẬT là zip. Một tệp thực thi khai là
// docx vẫn phải bị chặn ở bước kiểm tra nội dung.
func TestAttachmentExecutableClaimingOfficeIsRejected(t *testing.T) {
	h, convID, _ := sendSetup()

	in := attach(h, convID,
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"application/x-msdownload",
		20_000)

	err := sendWith(h, convID, in)
	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
}

// =========================================================================
// XIN URL TẢI LÊN
// =========================================================================

// TestPresignUploadRejectsDisallowedTypeEarly: chặn theo lời khai ngay ở bước
// xin URL.
//
// Đây KHÔNG phải hàng rào bảo mật (nội dung thật được kiểm lại sau), mà là để
// báo lỗi SỚM — người dùng biết tệp không được hỗ trợ trước khi tốn công tải
// lên vài chục megabyte.
func TestPresignUploadRejectsDisallowedTypeEarly(t *testing.T) {
	h, convID, me := sendSetup()

	for _, ct := range []string{"text/html", "image/svg+xml", "application/x-sh", ""} {
		_, _, err := h.uc.PresignUpload(
			context.Background(), me, convID, "x", ct, 1024)
		if got := statusOf(err); got != http.StatusBadRequest {
			t.Errorf("kiểu %q: mã lỗi = %d, muốn 400", ct, got)
		}
	}
}

func TestPresignUploadAcceptsCharsetParameter(t *testing.T) {
	h, convID, me := sendSetup()

	// "text/plain; charset=utf-8" phải được chấp nhận: tham số charset là
	// phần bình thường của Content-Type, và trình duyệt hay gửi kèm.
	if _, _, err := h.uc.PresignUpload(
		context.Background(), me, convID, "ghi-chu.txt",
		"text/plain; charset=utf-8", 1024); err != nil {
		t.Errorf("Content-Type có charset phải được chấp nhận: %v", err)
	}
}

func TestNormalizeContentType(t *testing.T) {
	cases := map[string]string{
		"image/png":                 "image/png",
		"IMAGE/PNG":                 "image/png",
		"text/plain; charset=utf-8": "text/plain",
		"  text/csv  ":              "text/csv",
		"":                          "",
	}
	for in, want := range cases {
		if got := normalizeContentType(in); got != want {
			t.Errorf("normalizeContentType(%q) = %q, muốn %q", in, got, want)
		}
	}
}

// TestSendWithoutStorageConfigured: chưa cấu hình R2 thì gửi tệp phải báo lỗi
// rõ ràng, không panic và không ghi bản ghi trỏ tới tệp không tồn tại.
func TestSendWithoutStorageConfigured(t *testing.T) {
	h, convID, me := sendSetup()
	h.uc.storage = nil

	_, err := h.uc.Send(context.Background(), me, SendInput{
		ConversationID: convID,
		Kind:           domainchat.MessageFile,
		Attachments: []AttachmentInput{{
			StorageKey:  "chat/" + convID.String() + "/a/b.png",
			FileName:    "b.png",
			ContentType: "image/png",
			SizeBytes:   100,
		}},
	})
	if err == nil {
		t.Fatal("phải báo lỗi khi chưa cấu hình lưu trữ")
	}
	if len(h.msgs.atts) != 0 {
		t.Error("không được ghi tệp đính kèm")
	}
}
