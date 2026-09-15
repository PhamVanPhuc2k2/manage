package middleware

import (
	"context"
	"net/http"
	"strings"

	chimw "github.com/go-chi/chi/v5/middleware"

	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	"github.com/PhamVanPhuc2k2/manage/pkg/httpx"
	"github.com/PhamVanPhuc2k2/manage/pkg/jwt"
	"github.com/PhamVanPhuc2k2/manage/pkg/logger"
)

type actorKey struct{}

// ActorFrom lấy Actor từ context. Trả về nil nếu route không qua RequireAuth.
func ActorFrom(ctx context.Context) *domainauth.Actor {
	a, _ := ctx.Value(actorKey{}).(*domainauth.Actor)
	return a
}

// RequireAuth xác minh access token và nạp Actor vào context.
func RequireAuth(
	jwtMgr *jwt.Manager,
	sessions domainauth.SessionStore,
	authReader domainauth.AuthorizationReader,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := bearerToken(r)
			if raw == "" {
				unauthorized(w, r, "Thiếu token xác thực")
				return
			}

			claims, err := jwtMgr.Verify(raw)
			if err != nil {
				unauthorized(w, r, "Token không hợp lệ hoặc đã hết hạn")
				return
			}

			// Kiểm tra phiên còn sống.
			//
			// ĐÂY là bước biến JWT từ "không thu hồi được" thành "thu hồi
			// tức thì". Bỏ nó đi thì đăng xuất không có tác dụng thật:
			// token vẫn dùng được tới khi hết hạn, tối đa 15 phút.
			//
			// Cái giá là một lệnh Redis mỗi request, dưới 1ms.
			session, err := sessions.Get(r.Context(), claims.SessionID)
			if err != nil || session == nil {
				unauthorized(w, r, "Phiên đăng nhập đã kết thúc")
				return
			}

			perms := make(map[string]struct{}, len(claims.Permissions))
			for _, p := range claims.Permissions {
				perms[p] = struct{}{}
			}

			actor := &domainauth.Actor{
				UserID:      claims.UserID,
				EmployeeID:  claims.EmployeeID,
				SessionID:   claims.SessionID,
				Roles:       claims.Roles,
				Permissions: perms,
				Scope:       domainauth.Scope(claims.Scope),
			}

			// Phạm vi "department" cần danh sách phòng ban cụ thể. Danh sách
			// này không nằm trong token vì nó có thể dài và có thể đổi khi
			// cơ cấu tổ chức thay đổi giữa hai lần đăng nhập.
			if actor.Scope == domainauth.ScopeDepartment {
				if authz, err := authReader.Load(r.Context(), actor.UserID, actor.EmployeeID); err == nil {
					actor.ManagedDepartmentIDs = authz.ManagedDepartmentIDs
				}
			}

			// Cập nhật thời điểm hoạt động cuối. Chạy nền vì nó chỉ phục vụ
			// hiển thị; lỗi ở đây không được chặn request.
			//
			// context.WithoutCancel giữ giá trị của context (logger, trace)
			// nhưng không chết theo khi request kết thúc.
			bgCtx := context.WithoutCancel(r.Context())
			go func() { _ = sessions.Touch(bgCtx, claims.SessionID) }()

			ctx := context.WithValue(r.Context(), actorKey{}, actor)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequirePermission kiểm tra quyền chi tiết. Đặt SAU RequireAuth.
//
// LƯU Ý: middleware này KHÔNG kiểm tra phạm vi dữ liệu, vì nó chỉ nhìn thấy
// đường dẫn và token — nó không biết bản ghi cụ thể nào đang bị đụng tới.
// Kiểm tra phạm vi bắt buộc nằm ở tầng usecase.
func RequirePermission(permission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			actor := ActorFrom(r.Context())
			if actor == nil {
				unauthorized(w, r, "Chưa xác thực")
				return
			}
			if !actor.Can(permission) {
				// Gán ra biến trước: zerolog.Logger có method pointer
				// receiver nên không gọi trực tiếp trên giá trị trả về
				// từ hàm được.
				log := logger.FromContext(r.Context())
				log.Warn().
					Str("user_id", actor.UserID.String()).
					Str("permission", permission).
					Msg("từ chối truy cập do thiếu quyền")

				forbidden(w, r, "Bạn không có quyền thực hiện thao tác này")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if after, ok := strings.CutPrefix(h, "Bearer "); ok {
		return strings.TrimSpace(after)
	}
	return ""
}

func unauthorized(w http.ResponseWriter, r *http.Request, msg string) {
	httpx.JSON(w, http.StatusUnauthorized, httpx.Envelope{Error: &httpx.ErrorBody{
		Code:      string(domainauth.ErrCodeUnauthorized),
		Message:   msg,
		RequestID: chimw.GetReqID(r.Context()),
	}})
}

func forbidden(w http.ResponseWriter, r *http.Request, msg string) {
	httpx.JSON(w, http.StatusForbidden, httpx.Envelope{Error: &httpx.ErrorBody{
		Code:      string(domainauth.ErrCodeForbidden),
		Message:   msg,
		RequestID: chimw.GetReqID(r.Context()),
	}})
}
