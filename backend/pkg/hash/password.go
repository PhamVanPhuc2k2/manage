// Package hash băm và kiểm tra mật khẩu bằng bcrypt.
package hash

import (
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// Cost 12 là điểm cân bằng hiện nay: khoảng 250ms mỗi lần băm trên máy chủ
// thông thường — đủ chậm để chặn dò mật khẩu hàng loạt, đủ nhanh để người
// dùng không thấy đợi. Cost 10 (mặc định của thư viện) đã quá yếu.
//
// KHÔNG hạ cost để "cho nhanh". 250ms chỉ tốn đúng một lần lúc đăng nhập.
const bcryptCost = 12

var ErrMismatch = errors.New("mật khẩu không đúng")

func Password(plain string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("băm mật khẩu: %w", err)
	}
	return string(b), nil
}

// Verify so sánh mật khẩu. bcrypt tự so sánh theo kiểu chống đo thời gian.
func Verify(hashed, plain string) error {
	err := bcrypt.CompareHashAndPassword([]byte(hashed), []byte(plain))
	if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		return ErrMismatch
	}
	return err
}

// NeedsRehash cho biết hash cũ có được tạo bằng cost thấp hơn hiện tại không.
//
// Gọi sau khi đăng nhập thành công: nếu đúng thì băm lại bằng cost mới và
// cập nhật âm thầm. Nhờ vậy nâng cost sau này không cần bắt ai đổi mật khẩu.
func NeedsRehash(hashed string) bool {
	cost, err := bcrypt.Cost([]byte(hashed))
	return err == nil && cost < bcryptCost
}

// DummyVerify tiêu tốn thời gian tương đương một lần Verify thật.
//
// Gọi khi email không tồn tại, để thời gian phản hồi không khác biệt so với
// trường hợp sai mật khẩu. Không có bước này, thời gian trả lời nhanh bất
// thường sẽ tiết lộ email nào có trong hệ thống.
func DummyVerify(plain string) {
	// Hash cố định của chuỗi "dummy", sinh sẵn bằng cost 12.
	const dummyHash = "$2a$12$C6UzMDM.H6dfI/f/IKcEe.7Zfi2H8wHy9rLMkhuGCYYzjKdWGwSxG"
	_ = bcrypt.CompareHashAndPassword([]byte(dummyHash), []byte(plain))
}
