package payroll

import "testing"

// Bộ kiểm thử số học tiền.
//
// Money là int64 đồng, không phải float. Mọi hàm ở đây đều nhỏ, nhưng chúng
// nhân với hàng nghìn phiếu lương mỗi tháng — một sai số làm tròn có hệ thống
// ở đây là tiền thật của người thật.

// TestApplyRoundsHalfUp là phép thử quan trọng nhất của tệp này.
//
// Cắt cụt (ép kiểu int64 trực tiếp) sẽ LUÔN có lợi cho công ty: mọi phần thập
// phân đều bị bỏ. Với hàng nghìn phiếu lương mỗi tháng, đó không còn là sai số
// ngẫu nhiên mà là một khoản bớt đều đặn.
func TestApplyRoundsHalfUp(t *testing.T) {
	cases := []struct {
		name string
		base Money
		rate float64
		want Money
	}{
		{"tròn sẵn", 10_000_000, 0.08, 800_000},
		{"phần thập phân .5 làm tròn LÊN", 101, 0.5, 51},          // 50.5 → 51
		{"phần thập phân dưới .5 làm tròn xuống", 100, 0.504, 50}, // 50.4 → 50
		{"phần thập phân trên .5 làm tròn lên", 100, 0.506, 51},   // 50.6 → 51
		{"tỷ lệ 0", 10_000_000, 0, 0},
		{"tỷ lệ 1 giữ nguyên", 12_345_678, 1, 12_345_678},
		{"số 0", 0, 0.08, 0},

		// Bảo hiểm nhân viên: 8% + 1.5% + 1% trên một mức lương lẻ.
		{"BHXH 8% trên lương lẻ", 7_333_333, 0.08, 586_667}, // 586666.64 → 586667
		{"BHYT 1.5% trên lương lẻ", 7_333_333, 0.015, 110_000},
		{"BHTN 1% trên lương lẻ", 7_333_333, 0.01, 73_333},
	}

	for _, c := range cases {
		if got := c.base.Apply(c.rate); got != c.want {
			t.Errorf("%s: Money(%d).Apply(%v) = %d, muốn %d",
				c.name, c.base, c.rate, got, c.want)
		}
	}
}

// TestApplyNeverTruncatesSystematically kiểm tra tính chất, không kiểm một
// giá trị cụ thể: với một dãy số liên tiếp, cắt cụt sẽ luôn cho kết quả nhỏ
// hơn hoặc bằng làm tròn, và tổng độ lệch sẽ dồn về một phía.
func TestApplyNeverTruncatesSystematically(t *testing.T) {
	const rate = 0.105 // 10.5%

	var roundedTotal, truncatedTotal int64
	for base := int64(1_000_000); base < 1_000_100; base++ {
		roundedTotal += int64(Money(base).Apply(rate))
		truncatedTotal += int64(float64(base) * rate) // cách làm SAI
	}

	if roundedTotal <= truncatedTotal {
		t.Fatalf("làm tròn (%d) phải lớn hơn cắt cụt (%d) trên một dãy số; "+
			"nếu không thì Apply đang cắt cụt", roundedTotal, truncatedTotal)
	}
}

func TestAtMost(t *testing.T) {
	cases := []struct {
		name  string
		value Money
		cap   Money
		want  Money
	}{
		{"dưới trần thì giữ nguyên", 10_000_000, 46_800_000, 10_000_000},
		{"trên trần thì bị kẹp", 100_000_000, 46_800_000, 46_800_000},
		{"đúng bằng trần", 46_800_000, 46_800_000, 46_800_000},

		// Trần 0 nghĩa là KHÔNG có trần, không phải "trần bằng 0".
		//
		// Phân biệt này quan trọng: bảo hiểm thất nghiệp và bảo hiểm xã hội có
		// hai trần khác nhau, và một tham số chưa cấu hình (giá trị 0) không
		// được biến thành "mọi người đóng 0 đồng".
		{"trần 0 nghĩa là không giới hạn", 100_000_000, 0, 100_000_000},
		{"trần âm cũng là không giới hạn", 100_000_000, -1, 100_000_000},
	}

	for _, c := range cases {
		if got := c.value.AtMost(c.cap); got != c.want {
			t.Errorf("%s: Money(%d).AtMost(%d) = %d, muốn %d",
				c.name, c.value, c.cap, got, c.want)
		}
	}
}

func TestNonNegative(t *testing.T) {
	cases := map[Money]Money{
		5_000_000:  5_000_000,
		0:          0,
		-1:         0,
		-5_000_000: 0,
	}

	for in, want := range cases {
		if got := in.NonNegative(); got != want {
			t.Errorf("Money(%d).NonNegative() = %d, muốn %d", in, got, want)
		}
	}
}

// TestInsuranceCapsAreDistinct khoá lại một điểm dễ sai của luật Việt Nam:
// BHXH/BHYT và BHTN có HAI trần khác nhau.
//
// Trần BHXH/BHYT là 20 lần lương cơ sở; trần BHTN là 20 lần lương tối thiểu
// vùng. Dùng chung một trần là lỗi tính lương im lặng — con số vẫn ra, chỉ là
// sai, và sai theo cùng một chiều với mọi người có lương cao.
func TestInsuranceCapsAreDistinct(t *testing.T) {
	const (
		baseSalary    = 2_340_000 // lương cơ sở
		minRegionWage = 4_960_000 // lương tối thiểu vùng I
	)

	socialCap := Money(20 * baseSalary)      // 46.800.000
	unemployCap := Money(20 * minRegionWage) // 99.200.000

	if socialCap == unemployCap {
		t.Fatal("hai trần bảo hiểm phải khác nhau")
	}

	// Người có lương 60 triệu: bị kẹp ở trần BHXH nhưng CHƯA tới trần BHTN.
	salary := Money(60_000_000)

	if got := salary.AtMost(socialCap); got != socialCap {
		t.Errorf("mức đóng BHXH = %d, muốn kẹp ở %d", got, socialCap)
	}
	if got := salary.AtMost(unemployCap); got != salary {
		t.Errorf("mức đóng BHTN = %d, muốn giữ nguyên %d", got, salary)
	}
}
