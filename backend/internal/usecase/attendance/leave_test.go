package attendance

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	domainatt "github.com/PhamVanPhuc2k2/manage/internal/domain/attendance"
)

// TestCountLeaveDaysSkipsWeekend là phép thử quan trọng nhất của module nghỉ
// phép.
//
// Nghỉ từ thứ sáu tới thứ hai là 2 ngày phép, không phải 4. Tính sai ở đây
// nghĩa là trừ oan quỹ phép của nhân viên — loại lỗi người ta nhớ rất lâu và
// không tin hệ thống nữa.
func TestCountLeaveDaysSkipsWeekend(t *testing.T) {
	// 2026-09-25 là thứ sáu, 2026-09-28 là thứ hai.
	friday := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	monday := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)

	if friday.Weekday() != time.Friday || monday.Weekday() != time.Monday {
		t.Fatalf("mốc kiểm thử sai: %s phải là thứ sáu, %s phải là thứ hai",
			friday.Format("2006-01-02"), monday.Format("2006-01-02"))
	}

	h := newHarness(friday)
	h.schedules.applicable = func(uuid.UUID, time.Time) []*domainatt.Schedule {
		return []*domainatt.Schedule{officeHours()}
	}

	days, err := h.uc.CountLeaveDays(
		context.Background(), uuid.New(), friday, monday, domainatt.PartFull)
	if err != nil {
		t.Fatalf("CountLeaveDays lỗi: %v", err)
	}
	if days != 2 {
		t.Errorf("nghỉ thứ sáu → thứ hai = %.1f ngày, muốn 2", days)
	}
}

func TestCountLeaveDaysSkipsHolidays(t *testing.T) {
	monday := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	friday := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	wednesday := time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)

	h := newHarness(monday)
	h.schedules.applicable = func(uuid.UUID, time.Time) []*domainatt.Schedule {
		return []*domainatt.Schedule{officeHours()}
	}
	h.holidays.between = func(time.Time, time.Time) []*domainatt.Holiday {
		return []*domainatt.Holiday{{Date: wednesday, Name: "Nghỉ lễ giữa tuần"}}
	}

	days, err := h.uc.CountLeaveDays(
		context.Background(), uuid.New(), monday, friday, domainatt.PartFull)
	if err != nil {
		t.Fatalf("CountLeaveDays lỗi: %v", err)
	}
	// Thứ hai đến thứ sáu là 5 ngày làm việc, trừ một ngày lễ giữa tuần.
	if days != 4 {
		t.Errorf("cả tuần có một ngày lễ = %.1f ngày, muốn 4", days)
	}
}

func TestCountLeaveDaysHalfDay(t *testing.T) {
	monday := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)

	h := newHarness(monday)
	h.schedules.applicable = func(uuid.UUID, time.Time) []*domainatt.Schedule {
		return []*domainatt.Schedule{officeHours()}
	}

	for _, part := range []domainatt.DayPart{domainatt.PartMorning, domainatt.PartAfternoon} {
		days, err := h.uc.CountLeaveDays(
			context.Background(), uuid.New(), monday, monday, part)
		if err != nil {
			t.Fatalf("CountLeaveDays(%s) lỗi: %v", part, err)
		}
		if days != 0.5 {
			t.Errorf("nghỉ %s = %.1f ngày, muốn 0.5", part, days)
		}
	}
}

// TestCountLeaveDaysWeekendOnlyIsZero: đơn nghỉ rơi trọn vào cuối tuần không
// tiêu ngày phép nào. Không có phép thử này thì một đơn "nghỉ thứ bảy" vẫn
// trừ quỹ, và không ai để ý cho tới khi hết phép sớm một tuần.
func TestCountLeaveDaysWeekendOnlyIsZero(t *testing.T) {
	saturday := time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)
	sunday := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)

	h := newHarness(saturday)
	h.schedules.applicable = func(uuid.UUID, time.Time) []*domainatt.Schedule {
		return []*domainatt.Schedule{officeHours()}
	}

	days, err := h.uc.CountLeaveDays(
		context.Background(), uuid.New(), saturday, sunday, domainatt.PartFull)
	if err != nil {
		t.Fatalf("CountLeaveDays lỗi: %v", err)
	}
	if days != 0 {
		t.Errorf("nghỉ trọn cuối tuần = %.1f ngày, muốn 0", days)
	}
}

// TestCountLeaveDaysNoScheduleCountsEveryDay: chưa cấu hình khung giờ thì
// không biết ngày nào là cuối tuần, nên đếm hết.
//
// Đây là hành vi CÓ Ý: hệ thống mới dựng chưa có khung giờ, và im lặng bỏ qua
// mọi ngày (trả 0) sẽ cho nghỉ phép miễn phí không giới hạn.
func TestCountLeaveDaysNoScheduleCountsEveryDay(t *testing.T) {
	saturday := time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)
	sunday := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)

	h := newHarness(saturday) // không gán schedules.applicable

	days, err := h.uc.CountLeaveDays(
		context.Background(), uuid.New(), saturday, sunday, domainatt.PartFull)
	if err != nil {
		t.Fatalf("CountLeaveDays lỗi: %v", err)
	}
	if days != 2 {
		t.Errorf("không có khung giờ = %.1f ngày, muốn 2 (đếm mọi ngày)", days)
	}
}

func TestCountLeaveDaysSingleDay(t *testing.T) {
	monday := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)

	h := newHarness(monday)
	h.schedules.applicable = func(uuid.UUID, time.Time) []*domainatt.Schedule {
		return []*domainatt.Schedule{officeHours()}
	}

	days, err := h.uc.CountLeaveDays(
		context.Background(), uuid.New(), monday, monday, domainatt.PartFull)
	if err != nil {
		t.Fatalf("CountLeaveDays lỗi: %v", err)
	}
	if days != 1 {
		t.Errorf("nghỉ một ngày = %.1f, muốn 1", days)
	}
}
