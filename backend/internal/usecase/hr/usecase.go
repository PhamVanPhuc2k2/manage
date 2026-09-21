// Package hr là tầng nghiệp vụ của module nhân sự.
package hr

import (
	"context"

	"github.com/google/uuid"

	domainhr "github.com/PhamVanPhuc2k2/manage/internal/domain/hr"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
)

const (
	defaultPageSize = 20
	maxPageSize     = 100
)

type Usecase struct {
	companies   domainhr.CompanyRepository
	departments domainhr.DepartmentRepository
	positions   domainhr.PositionRepository
	employees   domainhr.EmployeeRepository
	users       domainhr.UserRepository
	roles       domainhr.RoleRepository

	// storage có thể là nil khi chưa cấu hình Cloudflare R2. Mọi chỗ dùng
	// phải kiểm tra nil và báo lỗi rõ ràng — hệ thống vẫn chạy được, chỉ
	// riêng chức năng tệp là không.
	storage domainhr.FileStorage

	// welcomeMailer có thể nil; khi đó tạo tài khoản vẫn thành công, chỉ
	// không gửi mail — mật khẩu tạm vẫn trả về trong response để HR đưa tay.
	welcomeMailer WelcomeMailer

	// onEmployeeDeactivated được gọi khi một nhân viên bị vô hiệu hoá,
	// để module auth cắt phiên đăng nhập của họ.
	//
	// Dùng callback thay vì import thẳng usecase/auth: hr không được phụ
	// thuộc auth (auth đã phụ thuộc repository của hr), import ngược sẽ
	// tạo vòng. Composition root nối dây hai bên với nhau.
	onEmployeeDeactivated func(ctx context.Context, userID uuid.UUID)
}

// SetOnEmployeeDeactivated nối module hr với module auth. Gọi ở cmd/api.
func (u *Usecase) SetOnEmployeeDeactivated(fn func(ctx context.Context, userID uuid.UUID)) {
	u.onEmployeeDeactivated = fn
}

func NewUsecase(
	companies domainhr.CompanyRepository,
	departments domainhr.DepartmentRepository,
	positions domainhr.PositionRepository,
	employees domainhr.EmployeeRepository,
	users domainhr.UserRepository,
	roles domainhr.RoleRepository,
	storage domainhr.FileStorage,
) *Usecase {
	return &Usecase{
		companies:   companies,
		departments: departments,
		positions:   positions,
		employees:   employees,
		users:       users,
		roles:       roles,
		storage:     storage,
	}
}

// companyID lấy id công ty hiện tại.
//
// Hệ thống hiện phục vụ một công ty. Gom giả định đó vào một hàm để sau này
// hỗ trợ nhiều công ty thì chỉ phải sửa ở đây.
func (u *Usecase) companyID(ctx context.Context) (uuid.UUID, error) {
	c, err := u.companies.GetFirst(ctx)
	if err != nil {
		return uuid.Nil, apperror.New(apperror.KindUnprocessable,
			"Chưa khởi tạo công ty. Chạy lệnh seed trước.")
	}
	return c.ID, nil
}

// CurrentCompanyID công khai companyID cho module khác dùng.
//
// Tồn tại để module project biết nó đang làm việc cho công ty nào mà không
// phải import repository của hr. Module project khai báo interface một
// method (CompanyLookup) và composition root nối hrUC vào đó.
func (u *Usecase) CurrentCompanyID(ctx context.Context) (uuid.UUID, error) {
	return u.companyID(ctx)
}

// normalizePage chặn trên page_size.
//
// Không chặn thì ai đó gửi page_size=1000000 là kéo sập database.
func normalizePage(page, size int) (int, int) {
	if page <= 0 {
		page = 1
	}
	switch {
	case size <= 0:
		size = defaultPageSize
	case size > maxPageSize:
		size = maxPageSize
	}
	return page, size
}
