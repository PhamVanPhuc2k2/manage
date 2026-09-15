// Package token sinh và băm token ngẫu nhiên (refresh token, token đặt lại
// mật khẩu). Những token này KHÔNG phải JWT: chúng không mang thông tin,
// chỉ là khoá tra cứu trong Redis.
package token

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/big"
)

// New sinh token ngẫu nhiên 32 byte, mã hoá base64url (43 ký tự).
func New() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("sinh token ngẫu nhiên: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Hash băm token trước khi lưu.
//
// Vì sao phải băm? Nếu ai đó đọc được Redis (dump bộ nhớ, bản sao lưu,
// lệnh KEYS lọt ra ngoài), họ vẫn không mạo danh được ai.
//
// Dùng SHA-256 chứ không dùng bcrypt: token đã là 32 byte ngẫu nhiên nên
// không thể vét cạn, không cần làm chậm. bcrypt ở đây chỉ tổ tốn CPU.
func Hash(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

// NumericCode sinh mã số ngẫu nhiên dùng cho OTP, ví dụ "042915".
//
// Giữ nguyên số 0 ở đầu: mã là một CHUỖI, không phải số. Sinh bằng
// rand.Int(1_000_000) rồi định dạng "%06d" cũng ra kết quả đúng, nhưng chỉ
// cần ai đó lỡ tay parse sang int là mất chữ số đầu và mã sai vĩnh viễn.
//
// Dùng rand.Int của crypto/rand chứ không phải math/rand: math/rand đoán
// được toàn bộ dãy sau khi biết vài giá trị đầu.
func NumericCode(digits int) (string, error) {
	if digits < 4 {
		digits = 4
	}
	const numbers = "0123456789"
	max := big.NewInt(int64(len(numbers)))

	b := make([]byte, digits)
	for i := range b {
		// rand.Int lấy mẫu đều trong [0, max) — không dùng phép chia dư,
		// nên không lệch về phía các chữ số nhỏ.
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", fmt.Errorf("sinh mã OTP: %w", err)
		}
		b[i] = numbers[n.Int64()]
	}
	return string(b), nil
}

// Bỏ các ký tự dễ nhìn nhầm: 0/O, 1/l/I. Mật khẩu này được đọc qua điện
// thoại hoặc chép tay nên tránh nhầm lẫn quan trọng hơn tăng độ dài.
const passwordAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789@#$%"

// RandomPassword sinh mật khẩu tạm cho tài khoản mới.
func RandomPassword(length int) (string, error) {
	if length < 12 {
		length = 12
	}
	max := big.NewInt(int64(len(passwordAlphabet)))

	b := make([]byte, length)
	for i := range b {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", fmt.Errorf("sinh mật khẩu ngẫu nhiên: %w", err)
		}
		b[i] = passwordAlphabet[n.Int64()]
	}
	return string(b), nil
}
