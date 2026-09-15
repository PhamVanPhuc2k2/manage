// Binary seed: tạo công ty và tài khoản quản trị đầu tiên.
//
// Chạy MỘT LẦN khi cài đặt hệ thống:
//
//	docker compose run --rm api go run ./cmd/seed \
//	    --company "Công ty ABC" --email admin@abc.vn --name "Quản trị viên"
//
// Vì sao không seed bằng migration? Vì mật khẩu sẽ nằm trong git vĩnh viễn.
// Lệnh này sinh mật khẩu ngẫu nhiên và in ra màn hình ĐÚNG MỘT LẦN.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	// Nhúng dữ liệu múi giờ vào binary.
	//
	// Không có nó, image production (không cài gói tzdata) sẽ không hiểu
	// "Asia/Ho_Chi_Minh" và time.LoadLocation trả lỗi — mọi tính toán giờ
	// giấc sẽ âm thầm chạy theo UTC.
	_ "time/tzdata"

	domainhr "github.com/PhamVanPhuc2k2/manage/internal/domain/hr"
	repopg "github.com/PhamVanPhuc2k2/manage/internal/repository/postgres"
	"github.com/PhamVanPhuc2k2/manage/pkg/config"
	"github.com/PhamVanPhuc2k2/manage/pkg/hash"
	"github.com/PhamVanPhuc2k2/manage/pkg/logger"
	"github.com/PhamVanPhuc2k2/manage/pkg/postgres"
	"github.com/PhamVanPhuc2k2/manage/pkg/token"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "\nLỖI: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		companyName = flag.String("company", "Công ty của tôi", "Tên công ty")
		email       = flag.String("email", "", "Email tài khoản quản trị (bắt buộc)")
		fullName    = flag.String("name", "Quản trị viên", "Họ tên người quản trị")
		empCode     = flag.String("code", "NV0001", "Mã nhân viên")
	)
	flag.Parse()

	if strings.TrimSpace(*email) == "" {
		flag.Usage()
		return errors.New("thiếu --email")
	}

	cfg, err := config.Load("seed")
	if err != nil {
		return err
	}

	log := logger.New("seed", "info", cfg.Env)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	db, err := postgres.New(ctx, cfg.PostgresDSN, 4, log)
	if err != nil {
		return err
	}
	defer db.Close()

	companies := repopg.NewCompanyRepository(db)
	departments := repopg.NewDepartmentRepository(db)
	positions := repopg.NewPositionRepository(db)
	employees := repopg.NewEmployeeRepository(db)
	users := repopg.NewUserRepository(db)
	roles := repopg.NewRoleRepository(db)

	// --- Chặn chạy lại lần hai ---
	//
	// Nếu tài khoản đã tồn tại thì DỪNG, không âm thầm đổi mật khẩu.
	// Âm thầm đổi là cách chắc chắn để khoá chính mình ra khỏi hệ thống.
	if existing, err := users.FindByEmail(ctx, *email); err == nil && existing != nil {
		return fmt.Errorf("tài khoản %s đã tồn tại — huỷ để tránh ghi đè mật khẩu", *email)
	}

	// --- Công ty ---
	company, err := companies.GetFirst(ctx)
	if err != nil {
		company = &domainhr.Company{Name: *companyName, Timezone: "Asia/Ho_Chi_Minh"}
		if err := companies.Create(ctx, company); err != nil {
			return fmt.Errorf("tạo công ty: %w", err)
		}
		fmt.Printf("  ✓ Đã tạo công ty: %s\n", company.Name)
	} else {
		fmt.Printf("  · Công ty đã có: %s\n", company.Name)
	}

	// --- Phòng ban ---
	dept := &domainhr.Department{
		CompanyID: company.ID,
		Code:      "BGD",
		Name:      "Ban giám đốc",
	}
	if exists, _ := departments.ExistsCode(ctx, company.ID, dept.Code, nil); !exists {
		if err := departments.Create(ctx, dept); err != nil {
			return fmt.Errorf("tạo phòng ban: %w", err)
		}
		fmt.Printf("  ✓ Đã tạo phòng ban: %s\n", dept.Name)
	} else {
		list, _ := departments.List(ctx, company.ID)
		for _, d := range list {
			if strings.EqualFold(d.Code, "BGD") {
				dept = d
				break
			}
		}
		fmt.Printf("  · Phòng ban đã có: %s\n", dept.Name)
	}

	// --- Chức vụ ---
	pos := &domainhr.Position{CompanyID: company.ID, Code: "GD", Name: "Giám đốc"}
	if exists, _ := positions.ExistsCode(ctx, company.ID, pos.Code, nil); !exists {
		if err := positions.Create(ctx, pos); err != nil {
			return fmt.Errorf("tạo chức vụ: %w", err)
		}
		fmt.Printf("  ✓ Đã tạo chức vụ: %s\n", pos.Name)
	} else {
		list, _ := positions.List(ctx, company.ID)
		for _, p := range list {
			if strings.EqualFold(p.Code, "GD") {
				pos = p
				break
			}
		}
		fmt.Printf("  · Chức vụ đã có: %s\n", pos.Name)
	}

	// --- Nhân viên ---
	emp := &domainhr.Employee{
		CompanyID:    company.ID,
		EmployeeCode: *empCode,
		FullName:     *fullName,
		Email:        strings.ToLower(strings.TrimSpace(*email)),
		DepartmentID: &dept.ID,
		PositionID:   &pos.ID,
		WorkMode:     domainhr.WorkModeOnsite,
		Status:       domainhr.StatusOfficial,
		JoinedAt:     time.Now(),
	}
	if err := employees.Create(ctx, emp); err != nil {
		return fmt.Errorf("tạo nhân viên: %w", err)
	}
	fmt.Printf("  ✓ Đã tạo nhân viên: %s (%s)\n", emp.FullName, emp.EmployeeCode)

	// --- Tài khoản ---
	password, err := token.RandomPassword(16)
	if err != nil {
		return err
	}
	pwHash, err := hash.Password(password)
	if err != nil {
		return err
	}

	user := &domainhr.User{
		EmployeeID:         emp.ID,
		Email:              emp.Email,
		PasswordHash:       pwHash,
		IsActive:           true,
		MustChangePassword: true,
	}
	if err := users.Create(ctx, user); err != nil {
		return fmt.Errorf("tạo tài khoản: %w", err)
	}

	// --- Gán vai trò admin ---
	adminRole, err := roles.GetByCode(ctx, "admin")
	if err != nil {
		return fmt.Errorf("không tìm thấy vai trò admin — migration seed_rbac đã chạy chưa? %w", err)
	}
	if err := users.AssignRole(ctx, user.ID, adminRole.ID, user.ID); err != nil {
		return fmt.Errorf("gán vai trò admin: %w", err)
	}

	// --- Gán trưởng phòng ---
	dept.ManagerID = &emp.ID
	_ = departments.Update(ctx, dept)

	// Mật khẩu CHỈ in ra stdout, không ghi vào file, không ghi vào log.
	fmt.Printf(`
╔══════════════════════════════════════════════════════════════╗
║  TÀI KHOẢN QUẢN TRỊ ĐÃ ĐƯỢC TẠO                              ║
╠══════════════════════════════════════════════════════════════╣
║  Email:     %-48s ║
║  Mật khẩu:  %-48s ║
╠══════════════════════════════════════════════════════════════╣
║  Mật khẩu này CHỈ HIỆN MỘT LẦN. Lưu lại ngay.                ║
║  Hệ thống sẽ bắt đổi mật khẩu ở lần đăng nhập đầu tiên.       ║
╚══════════════════════════════════════════════════════════════╝

`, user.Email, password)

	return nil
}
