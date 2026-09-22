package system

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	domainsystem "github.com/PhamVanPhuc2k2/manage/internal/domain/system"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
)

// Bộ kiểm thử endpoint kiểm tra hệ thống.
//
// Thứ đáng kiểm ở đây không phải là "ping chạy được" mà là CÁCH PHÂN BIỆT
// giữa thành phần bắt buộc và thành phần được phép hỏng. Cùng một cách nghĩ
// được dùng lại khắp hệ thống: mất Redis thì chậm chứ không sai, mất
// database thì sai. Endpoint này là nơi luật đó được viết ra lần đầu, nên
// nó cũng là nơi dễ bị "sửa cho nhất quán" nhất.

var errFake = errors.New("bản giả lập: thành phần không phản hồi")

func statusOf(err error) int {
	if err == nil {
		return http.StatusOK
	}
	code, _ := apperror.HTTPStatus(err)
	return code
}

type fakeClock struct {
	now time.Time
	err error
}

func (f fakeClock) Now(context.Context) (time.Time, error) {
	if f.err != nil {
		return time.Time{}, f.err
	}
	return f.now, nil
}

type fakeCache struct{ err error }

func (f fakeCache) Ping(context.Context) error { return f.err }

type fakePublisher struct {
	published []domainsystem.Job
	err       error
}

func (f *fakePublisher) Publish(_ context.Context, job domainsystem.Job) error {
	if f.err != nil {
		return f.err
	}
	f.published = append(f.published, job)
	return nil
}

func dbTime() time.Time {
	return time.Date(2026, 9, 22, 10, 30, 0, 0, time.UTC)
}

// =========================================================================
// ĐƯỜNG THÀNH CÔNG
// =========================================================================

func TestPingReportsEveryComponent(t *testing.T) {
	pub := &fakePublisher{}
	uc := NewPingUsecase(fakeClock{now: dbTime()}, fakeCache{}, pub)

	got, err := uc.Ping(context.Background(), "req-123")
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if !got.DatabaseTime.Equal(dbTime()) {
		t.Errorf("giờ database = %v, muốn %v", got.DatabaseTime, dbTime())
	}
	if !got.RedisOK || !got.JobQueued {
		t.Errorf("redis = %v, job = %v; muốn cả hai true", got.RedisOK, got.JobQueued)
	}
	if got.RequestID != "req-123" {
		t.Errorf("request id = %q, muốn req-123", got.RequestID)
	}
}

// TestPingPublishesTypedJob: tên job là hằng chứ không phải chuỗi gõ tay ở
// hai nơi, và request id phải đi theo job xuống worker để hai bên nối được
// nhật ký với nhau.
func TestPingPublishesTypedJob(t *testing.T) {
	pub := &fakePublisher{}
	uc := NewPingUsecase(fakeClock{now: dbTime()}, fakeCache{}, pub)

	if _, err := uc.Ping(context.Background(), "req-123"); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if len(pub.published) != 1 {
		t.Fatalf("số job đã đẩy = %d, muốn 1", len(pub.published))
	}
	job := pub.published[0]
	if job.Name != domainsystem.JobSystemPing {
		t.Errorf("tên job = %q, muốn %q", job.Name, domainsystem.JobSystemPing)
	}
	if job.RequestID != "req-123" {
		t.Errorf("request id trong job = %q, muốn req-123", job.RequestID)
	}
	if job.Payload["source"] != "api" {
		t.Errorf("payload source = %v, muốn api", job.Payload["source"])
	}
}

// =========================================================================
// THÀNH PHẦN HỎNG
// =========================================================================

// TestDatabaseFailureIsFatal.
//
// Database là thành phần DUY NHẤT bắt buộc. Hỏng thì trả lỗi, không trả về
// một ảnh chụp nửa vời — báo "khoẻ" trong khi không đọc nổi dữ liệu là cách
// nhanh nhất để một sự cố lọt qua mọi cảnh báo.
func TestDatabaseFailureIsFatal(t *testing.T) {
	pub := &fakePublisher{}
	uc := NewPingUsecase(fakeClock{err: errFake}, fakeCache{}, pub)

	_, err := uc.Ping(context.Background(), "req-123")
	if got := statusOf(err); got != http.StatusInternalServerError {
		t.Errorf("mã lỗi = %d, muốn 500", got)
	}
}

// TestRedisFailureIsReportedNotFatal.
//
// Mất Redis thì chậm chứ không sai. Trả lỗi ở đây sẽ khiến health check đỏ
// và bộ cân bằng tải rút cả instance ra khỏi vòng phục vụ — biến một sự cố
// hiệu năng thành một sự cố ngừng dịch vụ.
func TestRedisFailureIsReportedNotFatal(t *testing.T) {
	pub := &fakePublisher{}
	uc := NewPingUsecase(fakeClock{now: dbTime()}, fakeCache{err: errFake}, pub)

	got, err := uc.Ping(context.Background(), "req-123")
	if err != nil {
		t.Fatalf("Redis hỏng không được làm hỏng cả lời gọi: %v", err)
	}
	if got.RedisOK {
		t.Error("phải đánh dấu Redis là hỏng")
	}
	if !got.JobQueued {
		t.Error("Redis hỏng không được ảnh hưởng tới hàng đợi")
	}
}

// TestQueueFailureIsReportedNotFatal: cùng lý do với Redis — việc nền chậm
// đi, nhưng API vẫn phục vụ được.
func TestQueueFailureIsReportedNotFatal(t *testing.T) {
	pub := &fakePublisher{err: errFake}
	uc := NewPingUsecase(fakeClock{now: dbTime()}, fakeCache{}, pub)

	got, err := uc.Ping(context.Background(), "req-123")
	if err != nil {
		t.Fatalf("hàng đợi hỏng không được làm hỏng cả lời gọi: %v", err)
	}
	if got.JobQueued {
		t.Error("phải đánh dấu hàng đợi là hỏng")
	}
	if !got.RedisOK {
		t.Error("hàng đợi hỏng không được ảnh hưởng tới Redis")
	}
}

// TestBothOptionalComponentsDown: cả hai thành phần phụ cùng hỏng vẫn phải
// trả về được ảnh chụp, để người trực biết chính xác thứ gì đang hỏng.
func TestBothOptionalComponentsDown(t *testing.T) {
	pub := &fakePublisher{err: errFake}
	uc := NewPingUsecase(
		fakeClock{now: dbTime()}, fakeCache{err: errFake}, pub)

	got, err := uc.Ping(context.Background(), "req-123")
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if got.RedisOK || got.JobQueued {
		t.Errorf("redis = %v, job = %v; muốn cả hai false",
			got.RedisOK, got.JobQueued)
	}
	if !got.DatabaseTime.Equal(dbTime()) {
		t.Error("vẫn phải trả về giờ database")
	}
}

// Ràng buộc kiểu: bản giả lập phải khớp cổng ở tầng domain.
var (
	_ domainsystem.Clock        = fakeClock{}
	_ domainsystem.Cache        = fakeCache{}
	_ domainsystem.JobPublisher = (*fakePublisher)(nil)
)
