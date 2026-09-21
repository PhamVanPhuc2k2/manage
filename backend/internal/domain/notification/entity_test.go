package notification

import "testing"

// TestImportantIsNarrow khoá lại một quyết định thiết kế dễ bị nới lỏng dần.
//
// Mỗi loại thêm vào danh sách "quan trọng" là thêm một loại email tự động gửi
// cho cả công ty. Gửi email cho mọi loại thông báo là cách nhanh nhất khiến
// người ta lập bộ lọc cho tất cả email từ hệ thống — và khi đó cả những email
// thật sự quan trọng cũng không ai đọc.
func TestImportantIsNarrow(t *testing.T) {
	want := map[Type]bool{
		TypeTaskAssigned:      true,
		TypeLeaveDecided:      true,
		TypeAdjustmentDecided: true,
		TypePayslipReady:      true,
	}

	for _, ty := range AllTypes() {
		if got := ty.Important(); got != want[ty] {
			t.Errorf("%s.Important() = %v, muốn %v", ty, got, want[ty])
		}
	}
}

// TestMutableExcludesDecisions: người dùng không được tắt những thông báo mang
// thông tin họ buộc phải biết, nếu không sẽ có tình huống "tôi không biết đơn
// bị từ chối" mà không ai giải quyết được.
func TestMutableExcludesDecisions(t *testing.T) {
	locked := []Type{
		TypeSystem, TypeLeaveDecided, TypeAdjustmentDecided, TypePayslipReady,
	}

	for _, ty := range locked {
		if ty.Mutable() {
			t.Errorf("%s không được phép tắt", ty)
		}
	}

	if !TypeNewMessage.Mutable() {
		t.Error("tin nhắn mới phải tắt được")
	}
}

func TestAllTypesAreValidAndLabelled(t *testing.T) {
	all := AllTypes()
	if len(all) == 0 {
		t.Fatal("danh mục loại thông báo rỗng")
	}

	for _, ty := range all {
		if !ty.Valid() {
			t.Errorf("%s có trong danh mục nhưng Valid() trả false", ty)
		}
		if ty.Label() == "" {
			t.Errorf("%s thiếu nhãn hiển thị", ty)
		}
	}
}

// TestAllTypesIsACopy: trả thẳng slice nội bộ ra ngoài thì một chỗ gọi lỡ tay
// sắp xếp lại là làm hỏng danh mục cho mọi chỗ khác.
func TestAllTypesIsACopy(t *testing.T) {
	first := AllTypes()
	first[0] = "đã bị sửa"

	if AllTypes()[0] == "đã bị sửa" {
		t.Fatal("AllTypes trả về slice dùng chung, sửa được từ bên ngoài")
	}
}
