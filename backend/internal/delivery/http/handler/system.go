// Package handler là tầng delivery HTTP: dịch giữa HTTP và usecase.
// Tuyệt đối không chứa logic nghiệp vụ.
package handler

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"

	domainsystem "github.com/yourorg/manage/internal/domain/system"
	"github.com/yourorg/manage/pkg/apperror"
	"github.com/yourorg/manage/pkg/httpx"
)

// PingUsecase là interface mà handler cần.
//
// Khai báo ở ĐÂY, phía người dùng, chứ không phải ở package usecase —
// đó là cách Go làm dependency inversion, và nhờ vậy handler test được
// mà không cần usecase thật.
type PingUsecase interface {
	Ping(ctx context.Context, requestID string) (domainsystem.HealthSnapshot, error)
}

type SystemHandler struct {
	uc      PingUsecase
	version string
}

func NewSystemHandler(uc PingUsecase, version string) *SystemHandler {
	return &SystemHandler{uc: uc, version: version}
}

// Ping là endpoint nghiệm thu của Phase 0.
func (h *SystemHandler) Ping(w http.ResponseWriter, r *http.Request) {
	requestID := middleware.GetReqID(r.Context())

	snapshot, err := h.uc.Ping(r.Context(), requestID)
	if err != nil {
		Error(w, err, requestID)
		return
	}

	httpx.OK(w, map[string]any{
		"database_time": snapshot.DatabaseTime,
		"redis_ok":      snapshot.RedisOK,
		"job_queued":    snapshot.JobQueued,
		"request_id":    snapshot.RequestID,
		"version":       h.version,
	})
}

// Error là điểm DUY NHẤT dịch lỗi ứng dụng thành HTTP response.
func Error(w http.ResponseWriter, err error, requestID string) {
	status, appErr := apperror.HTTPStatus(err)

	message := appErr.Message
	// Không để lộ chi tiết lỗi nội bộ ra ngoài — đó là thông tin cho attacker.
	if status == http.StatusInternalServerError {
		message = "Đã có lỗi xảy ra, vui lòng thử lại"
	}

	httpx.JSON(w, status, httpx.Envelope{Error: &httpx.ErrorBody{
		Code:      string(appErr.Kind),
		Message:   message,
		Details:   appErr.Details,
		RequestID: requestID,
	}})
}
