// Package system chứa entity và port của module kiểm tra hệ thống.
//
// Module này không có giá trị nghiệp vụ. Nó tồn tại để chứng minh bộ khung
// chạy được, và làm mẫu cho mọi module nghiệp vụ sau này sao chép cấu trúc.
package system

import "time"

// HealthSnapshot mô tả tình trạng hạ tầng tại một thời điểm.
type HealthSnapshot struct {
	DatabaseTime time.Time
	RedisOK      bool
	JobQueued    bool
	RequestID    string
}

// Job là một việc cần xử lý nền.
type Job struct {
	Name      string         `json:"name"`
	RequestID string         `json:"request_id"`
	Payload   map[string]any `json:"payload"`
}

// Tên các job. Khai báo hằng để tránh gõ sai chuỗi ở hai nơi.
const JobSystemPing = "system.ping"
