// Package jwt phát hành và xác minh access token.
package jwt

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var (
	ErrExpired = errors.New("token đã hết hạn")
	ErrInvalid = errors.New("token không hợp lệ")
)

// Claims là nội dung access token.
//
// Nhét sẵn roles và permissions để middleware khỏi truy vấn database mỗi
// request. Đánh đổi: đổi quyền cho ai đó thì phải chờ tối đa 15 phút token
// cũ hết hạn — hoặc chủ động huỷ phiên của họ để bắt đăng nhập lại.
type Claims struct {
	UserID      uuid.UUID `json:"uid"`
	EmployeeID  uuid.UUID `json:"eid"`
	SessionID   uuid.UUID `json:"sid"`
	Roles       []string  `json:"roles"`
	Permissions []string  `json:"perms"`
	Scope       string    `json:"scope"`
	jwt.RegisteredClaims
}

type Manager struct {
	secret    []byte
	accessTTL time.Duration
	issuer    string
}

func NewManager(secret string, accessTTL time.Duration, issuer string) (*Manager, error) {
	// HS256 với khoá ngắn là mời gọi tấn công vét cạn.
	if len(secret) < 32 {
		return nil, fmt.Errorf("JWT_SECRET phải dài ít nhất 32 ký tự, hiện có %d", len(secret))
	}
	return &Manager{secret: []byte(secret), accessTTL: accessTTL, issuer: issuer}, nil
}

func (m *Manager) AccessTTL() time.Duration { return m.accessTTL }

func (m *Manager) Issue(c Claims) (string, time.Time, error) {
	now := time.Now()
	expiresAt := now.Add(m.accessTTL)

	c.RegisteredClaims = jwt.RegisteredClaims{
		Issuer:    m.issuer,
		Subject:   c.UserID.String(),
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(expiresAt),
		ID:        uuid.NewString(),
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(m.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("ký token: %w", err)
	}
	return signed, expiresAt, nil
}

func (m *Manager) Verify(tokenString string) (*Claims, error) {
	var claims Claims

	_, err := jwt.ParseWithClaims(tokenString, &claims,
		func(t *jwt.Token) (any, error) {
			// BẮT BUỘC kiểm tra thuật toán.
			//
			// Thiếu bước này là lỗ hổng kinh điển của JWT: kẻ tấn công đổi
			// header sang alg=none hoặc alg=RS256 rồi tự ký bằng "khoá công
			// khai" chính là secret của ta — thư viện sẽ chấp nhận.
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("thuật toán ký không hợp lệ: %v", t.Header["alg"])
			}
			return m.secret, nil
		},
		jwt.WithIssuer(m.issuer),
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
	)

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrExpired
		}
		return nil, ErrInvalid
	}
	return &claims, nil
}
