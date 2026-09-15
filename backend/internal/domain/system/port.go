package system

import (
	"context"
	"time"
)

// Đây là các CỔNG (port) ra thế giới bên ngoài.
//
// Chúng được khai báo ở tầng domain nhưng ĐƯỢC HIỆN THỰC ở tầng repository.
// Nhờ vậy usecase chỉ biết interface, không biết đằng sau là PostgreSQL hay
// cái gì khác — và test được bằng bản giả mà không cần chạy database thật.

// Clock đọc giờ từ nguồn bên ngoài (ở đây là database).
type Clock interface {
	Now(ctx context.Context) (time.Time, error)
}

// Cache là bộ nhớ đệm.
type Cache interface {
	Ping(ctx context.Context) error
}

// JobPublisher đẩy việc sang hàng đợi để worker xử lý.
type JobPublisher interface {
	Publish(ctx context.Context, job Job) error
}
