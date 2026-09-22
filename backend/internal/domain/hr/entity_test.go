package hr

import (
	"testing"
	"time"
)

// TestCanLoginBlocksEveryDisabledState là phép thử BẢO MẬT.
//
// CanLogin là hàng rào cuối giữa "mật khẩu đúng" và "được vào hệ thống". Nó
// phải chặn BỐN trạng thái khác nhau, và bỏ sót bất kỳ trạng thái nào nghĩa là
// người đã nghỉ việc vẫn đăng nhập được bằng mật khẩu cũ.
//
// Bốn trạng thái là bốn cách khác nhau để một tài khoản mất hiệu lực, và chúng
// nằm ở hai bảng khác nhau (users và employees) — đó chính là lý do dễ sót.
func TestCanLoginBlocksEveryDisabledState(t *testing.T) {
	now := time.Now()

	cases := []struct {
		name    string
		user    *User
		blocked bool
	}{
		{
			name:    "bình thường",
			user:    &User{IsActive: true, EmployeeStatus: StatusOfficial},
			blocked: false,
		},
		{
			name:    "đang thử việc vẫn vào được",
			user:    &User{IsActive: true, EmployeeStatus: StatusProbation},
			blocked: false,
		},
		{
			name:    "tài khoản bị tắt",
			user:    &User{IsActive: false, EmployeeStatus: StatusOfficial},
			blocked: true,
		},
		{
			name: "tài khoản bị xoá mềm",
			user: &User{
				IsActive: true, DeletedAt: &now, EmployeeStatus: StatusOfficial,
			},
			blocked: true,
		},
		{
			name: "hồ sơ nhân viên bị xoá",
			user: &User{
				IsActive: true, EmployeeDeletedAt: &now, EmployeeStatus: StatusOfficial,
			},
			blocked: true,
		},
		{
			name:    "nhân viên đã nghỉ việc",
			user:    &User{IsActive: true, EmployeeStatus: StatusResigned},
			blocked: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.user.CanLogin()
			if c.blocked && err == nil {
				t.Error("phải bị chặn nhưng lại cho qua")
			}
			if !c.blocked && err != nil {
				t.Errorf("phải cho qua nhưng bị chặn: %v", err)
			}
		})
	}
}

// TestCanLoginZeroValueIsBlocked: giá trị zero của User (IsActive = false)
// phải bị chặn.
//
// Quan trọng vì đó là thứ ta nhận được khi một lỗi đọc dữ liệu bị bỏ qua và
// con trỏ nil được thay bằng struct rỗng. Mặc định "cho qua" ở đây là lỗ hổng.
func TestCanLoginZeroValueIsBlocked(t *testing.T) {
	if (&User{}).CanLogin() == nil {
		t.Fatal("User rỗng phải bị chặn đăng nhập")
	}
}

// TestCanLoginErrorsAreDistinct: bốn lý do chặn phải cho bốn thông báo khác
// nhau.
//
// Người dùng bị chặn cần biết nên liên hệ ai: tài khoản bị tắt thì hỏi quản
// trị, nghỉ việc thì hỏi nhân sự. Một thông báo chung cho cả bốn sẽ khiến họ
// gọi nhầm chỗ.
//
// Lưu ý đây KHÁC với màn hình đăng nhập, nơi cố ý dùng một thông báo chung —
// ở đó người gọi chưa chứng minh được họ là ai, còn ở đây họ đã qua mật khẩu.
func TestCanLoginErrorsAreDistinct(t *testing.T) {
	now := time.Now()

	users := []*User{
		{IsActive: false, EmployeeStatus: StatusOfficial},
		{IsActive: true, EmployeeDeletedAt: &now, EmployeeStatus: StatusOfficial},
		{IsActive: true, EmployeeStatus: StatusResigned},
	}

	seen := map[string]bool{}
	for _, u := range users {
		err := u.CanLogin()
		if err == nil {
			t.Fatal("phải bị chặn")
		}
		if seen[err.Error()] {
			t.Errorf("hai lý do chặn cho cùng thông báo: %q", err.Error())
		}
		seen[err.Error()] = true
	}
}

func TestEmployeeStatusValid(t *testing.T) {
	for _, s := range []EmployeeStatus{
		StatusProbation, StatusOfficial, StatusResigned,
	} {
		if !s.Valid() {
			t.Errorf("%q phải hợp lệ", s)
		}
	}
	for _, s := range []string{"", "active", "OFFICIAL", "nghi_viec"} {
		if EmployeeStatus(s).Valid() {
			t.Errorf("%q không được coi là hợp lệ", s)
		}
	}
}
