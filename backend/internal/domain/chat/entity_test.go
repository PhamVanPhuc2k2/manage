package chat

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestDirectKeyForIsSymmetric là bài kiểm tra quan trọng nhất của package này.
//
// Nếu (A,B) và (B,A) cho hai khoá khác nhau, hai người cùng bấm "nhắn tin" sẽ
// tạo ra hai hội thoại song song và tin nhắn của họ đi vào hai chỗ khác nhau —
// một lỗi chỉ lộ ra khi có đúng hai người thao tác gần nhau, nên rất khó bắt
// bằng thử tay.
func TestDirectKeyForIsSymmetric(t *testing.T) {
	a := uuid.MustParse("00000000-0000-0000-0000-0000000000aa")
	b := uuid.MustParse("ffffffff-ffff-ffff-ffff-ffffffffffff")

	if DirectKeyFor(a, b) != DirectKeyFor(b, a) {
		t.Fatalf("khoá 1-1 không đối xứng: %q vs %q",
			DirectKeyFor(a, b), DirectKeyFor(b, a))
	}
}

func TestDirectKeyForDistinguishesPairs(t *testing.T) {
	a := uuid.New()
	b := uuid.New()
	c := uuid.New()

	if DirectKeyFor(a, b) == DirectKeyFor(a, c) {
		t.Fatal("hai cặp khác nhau cho cùng một khoá")
	}
}

func TestKindManaged(t *testing.T) {
	cases := map[Kind]bool{
		KindDirect:     false,
		KindGroup:      false,
		KindDepartment: true,
		KindProject:    true,
	}

	for k, want := range cases {
		if got := k.Managed(); got != want {
			t.Errorf("%s.Managed() = %v, muốn %v", k, got, want)
		}
	}
}

// TestDisplayContentHidesDeleted khoá lại một quy tắc bảo mật: xoá mềm là để
// giữ dấu vết cho quản trị, không phải để nội dung vẫn rò ra qua API.
func TestDisplayContentHidesDeleted(t *testing.T) {
	now := time.Now()
	m := &Message{Content: "nội dung nhạy cảm", DeletedAt: &now}

	if got := m.DisplayContent(); got != "" {
		t.Fatalf("tin đã thu hồi vẫn trả về nội dung: %q", got)
	}

	m.DeletedAt = nil
	if got := m.DisplayContent(); got != "nội dung nhạy cảm" {
		t.Fatalf("tin bình thường bị giấu nội dung: %q", got)
	}
}

func TestMessageKindValid(t *testing.T) {
	for _, k := range []MessageKind{MessageText, MessageFile, MessageImage, MessageSystem} {
		if !k.Valid() {
			t.Errorf("%s phải hợp lệ", k)
		}
	}
	if MessageKind("sticker").Valid() {
		t.Error("loại tin lạ không được coi là hợp lệ")
	}
}
