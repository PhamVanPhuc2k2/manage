package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"

	appmw "github.com/PhamVanPhuc2k2/manage/internal/delivery/http/middleware"
	ucauth "github.com/PhamVanPhuc2k2/manage/internal/usecase/auth"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
	"github.com/PhamVanPhuc2k2/manage/pkg/httpx"
)

// refreshCookieName và refreshCookiePath: giới hạn cookie chỉ gửi kèm khi
// gọi đúng nhóm endpoint auth, không đính vào mọi request.
const (
	refreshCookieName = "manage_refresh"
	refreshCookiePath = "/api/v1/auth"
)

type AuthHandler struct {
	uc       *ucauth.Usecase
	secure   bool // đặt cờ Secure trên cookie (chỉ khi chạy HTTPS)
	basePath string
}

func NewAuthHandler(uc *ucauth.Usecase, secure bool) *AuthHandler {
	return &AuthHandler{uc: uc, secure: secure, basePath: refreshCookiePath}
}

// setRefreshCookie đặt refresh token vào cookie httpOnly.
//
// httpOnly: JavaScript không đọc được, nên XSS không lấy được token.
// SameSite=Lax: chặn gửi kèm trong request POST từ site khác — đủ để chống
// CSRF cho endpoint refresh mà không cần token CSRF riêng.
//
// gosec cảnh báo vì Secure là biến chứ không phải hằng true. Đây là chủ ý:
// bật Secure trên HTTP ở môi trường dev sẽ khiến trình duyệt vứt cookie đi
// và chức năng refresh không bao giờ chạy. Giá trị được đặt theo cfg.IsProduction()
// ở composition root, nên production luôn có Secure.
//
//nolint:gosec // Secure bật theo môi trường, xem giải thích ở trên
func (h *AuthHandler) setRefreshCookie(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    token,
		Path:     h.basePath,
		Expires:  expires,
		HttpOnly: true,
		Secure:   h.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

//nolint:gosec // Secure bật theo môi trường, xem setRefreshCookie
func (h *AuthHandler) clearRefreshCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    "",
		Path:     h.basePath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Device   string `json:"device"`
}

type tokenResponse struct {
	AccessToken        string    `json:"access_token"`
	ExpiresAt          time.Time `json:"expires_at"`
	MustChangePassword bool      `json:"must_change_password"`
	User               *meUser   `json:"user,omitempty"`
}

type meUser struct {
	ID         string   `json:"id"`
	EmployeeID string   `json:"employee_id"`
	Email      string   `json:"email"`
	FullName   string   `json:"full_name"`
	Roles      []string `json:"roles"`
	Perms      []string `json:"permissions"`
	Scope      string   `json:"scope"`
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, chimw.GetReqID(r.Context()))
		return
	}

	pair, err := h.uc.Login(r.Context(), ucauth.LoginInput{
		Email:      strings.TrimSpace(req.Email),
		Password:   req.Password,
		IP:         clientIP(r),
		UserAgent:  r.UserAgent(),
		DeviceName: deviceName(req.Device, r.UserAgent()),
	})
	if err != nil {
		Error(w, err, chimw.GetReqID(r.Context()))
		return
	}

	h.setRefreshCookie(w, pair.RefreshToken, pair.RefreshExpiresAt)

	// Access token trả trong BODY, không đặt vào cookie.
	// Frontend giữ nó trong bộ nhớ JS — không localStorage, không cookie.
	httpx.OK(w, tokenResponse{
		AccessToken:        pair.AccessToken,
		ExpiresAt:          pair.AccessExpiresAt,
		MustChangePassword: pair.MustChangePassword,
	})
}

func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	cookie, err := r.Cookie(refreshCookieName)
	if err != nil || cookie.Value == "" {
		Error(w, apperror.New(apperror.KindUnauthorized, "Không có phiên đăng nhập"), requestID)
		return
	}

	pair, err := h.uc.Refresh(r.Context(), cookie.Value, clientIP(r), r.UserAgent())
	if err != nil {
		// Xoá cookie hỏng để trình duyệt không gửi lại mãi.
		h.clearRefreshCookie(w)
		Error(w, err, requestID)
		return
	}

	h.setRefreshCookie(w, pair.RefreshToken, pair.RefreshExpiresAt)
	httpx.OK(w, tokenResponse{
		AccessToken:        pair.AccessToken,
		ExpiresAt:          pair.AccessExpiresAt,
		MustChangePassword: pair.MustChangePassword,
	})
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	actor := appmw.ActorFrom(r.Context())
	if actor != nil {
		_ = h.uc.Logout(r.Context(), actor.SessionID)
	}
	h.clearRefreshCookie(w)
	httpx.OK(w, map[string]string{"status": "đã đăng xuất"})
}

func (h *AuthHandler) LogoutAll(w http.ResponseWriter, r *http.Request) {
	actor := appmw.ActorFrom(r.Context())
	if actor != nil {
		if err := h.uc.LogoutAll(r.Context(), actor.UserID); err != nil {
			Error(w, err, chimw.GetReqID(r.Context()))
			return
		}
	}
	h.clearRefreshCookie(w)
	httpx.OK(w, map[string]string{"status": "đã đăng xuất khỏi mọi thiết bị"})
}

func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	actor := appmw.ActorFrom(r.Context())
	if actor == nil {
		Error(w, apperror.New(apperror.KindUnauthorized, "Chưa đăng nhập"), "")
		return
	}

	perms := make([]string, 0, len(actor.Permissions))
	for p := range actor.Permissions {
		perms = append(perms, p)
	}

	out := meUser{
		ID:         actor.UserID.String(),
		EmployeeID: actor.EmployeeID.String(),
		Roles:      actor.Roles,
		Perms:      perms,
		Scope:      string(actor.Scope),
	}

	// Email và họ tên không nằm trong token (xem ghi chú ở Usecase.Me),
	// nên đọc thêm từ database. Lỗi ở đây không chặn response: phần quan
	// trọng nhất — quyền — đã có sẵn từ token.
	if user, err := h.uc.Me(r.Context(), actor.UserID); err == nil && user != nil {
		out.Email = user.Email
		out.FullName = user.EmployeeName
	}

	httpx.OK(w, out)
}

func (h *AuthHandler) ListSessions(w http.ResponseWriter, r *http.Request) {
	actor := appmw.ActorFrom(r.Context())
	sessions, err := h.uc.ListSessions(r.Context(), actor.UserID)
	if err != nil {
		Error(w, err, chimw.GetReqID(r.Context()))
		return
	}

	type sessionDTO struct {
		ID         string    `json:"id"`
		DeviceName string    `json:"device_name"`
		IP         string    `json:"ip"`
		CreatedAt  time.Time `json:"created_at"`
		LastSeenAt time.Time `json:"last_seen_at"`
		Current    bool      `json:"current"`
	}

	out := make([]sessionDTO, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, sessionDTO{
			ID:         s.ID.String(),
			DeviceName: s.DeviceName,
			IP:         s.IP,
			CreatedAt:  s.CreatedAt,
			LastSeenAt: s.LastSeenAt,
			Current:    s.ID == actor.SessionID,
		})
	}
	httpx.OK(w, out)
}

type changePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

func (h *AuthHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())
	actor := appmw.ActorFrom(r.Context())

	var req changePasswordRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}

	if err := h.uc.ChangePassword(r.Context(), actor.UserID, actor.SessionID,
		req.OldPassword, req.NewPassword); err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, map[string]string{"status": "đã đổi mật khẩu"})
}

type forgotPasswordRequest struct {
	Email string `json:"email"`
}

func (h *AuthHandler) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req forgotPasswordRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, chimw.GetReqID(r.Context()))
		return
	}

	// Bỏ qua lỗi có chủ ý: LUÔN trả về cùng một thông báo dù email có tồn
	// tại hay không. Phân biệt hai trường hợp là biến endpoint này thành
	// công cụ dò danh sách email nhân viên.
	_ = h.uc.ForgotPassword(r.Context(), strings.TrimSpace(req.Email))

	httpx.OK(w, map[string]string{
		"status": "Nếu email tồn tại trong hệ thống, hướng dẫn đặt lại mật khẩu đã được gửi.",
	})
}

type resetPasswordRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

func (h *AuthHandler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	var req resetPasswordRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}

	if err := h.uc.ResetPassword(r.Context(), req.Token, req.NewPassword); err != nil {
		Error(w, err, requestID)
		return
	}
	h.clearRefreshCookie(w)
	httpx.OK(w, map[string]string{"status": "đã đặt lại mật khẩu"})
}

// --------------------------------------------------------------- tiện ích

func decodeJSON(r *http.Request, dst any) error {
	// Chặn body quá lớn: không giới hạn thì một request 1GB làm hết RAM.
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		return apperror.Invalid("Dữ liệu gửi lên không hợp lệ", err.Error())
	}
	return nil
}

func clientIP(r *http.Request) string {
	// chi/middleware.RealIP đã xử lý X-Forwarded-For, nên RemoteAddr ở đây
	// đã là IP thật của client phía sau nginx.
	if host, _, ok := strings.Cut(r.RemoteAddr, ":"); ok {
		return host
	}
	return r.RemoteAddr
}

func deviceName(provided, ua string) string {
	if s := strings.TrimSpace(provided); s != "" {
		if len(s) > 100 {
			s = s[:100]
		}
		return s
	}
	switch {
	case strings.Contains(ua, "Android"):
		return "Android"
	case strings.Contains(ua, "iPhone"), strings.Contains(ua, "iPad"):
		return "iOS"
	case strings.Contains(ua, "Windows"):
		return "Windows"
	case strings.Contains(ua, "Macintosh"):
		return "macOS"
	case strings.Contains(ua, "Linux"):
		return "Linux"
	default:
		return "Không rõ"
	}
}
