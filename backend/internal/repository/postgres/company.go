package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	domainhr "github.com/PhamVanPhuc2k2/manage/internal/domain/hr"
	"github.com/PhamVanPhuc2k2/manage/pkg/postgres"
)

type CompanyRepository struct {
	db *postgres.DB
}

func NewCompanyRepository(db *postgres.DB) *CompanyRepository {
	return &CompanyRepository{db: db}
}

const selectCompany = `
SELECT id, name, COALESCE(tax_code,''), COALESCE(address,''), timezone,
       to_char(work_start,'HH24:MI'), to_char(work_end,'HH24:MI'),
       created_at, updated_at
FROM companies
`

func scanCompany(row pgx.Row) (*domainhr.Company, error) {
	var c domainhr.Company
	err := row.Scan(&c.ID, &c.Name, &c.TaxCode, &c.Address, &c.Timezone,
		&c.WorkStart, &c.WorkEnd, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainhr.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("đọc công ty: %w", err)
	}
	return &c, nil
}

func (r *CompanyRepository) Create(ctx context.Context, c *domainhr.Company) error {
	const q = `
		INSERT INTO companies (name, tax_code, address, timezone)
		VALUES ($1, NULLIF($2,''), NULLIF($3,''), COALESCE(NULLIF($4,''), 'Asia/Ho_Chi_Minh'))
		RETURNING id, created_at, updated_at`

	err := r.db.QueryRow(ctx, q, c.Name, c.TaxCode, c.Address, c.Timezone).
		Scan(&c.ID, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return fmt.Errorf("tạo công ty: %w", err)
	}
	return nil
}

// GetFirst lấy công ty đầu tiên.
//
// Hệ thống hiện chỉ phục vụ một công ty. Hàm này là chỗ duy nhất giả định
// điều đó — khi nào cần hỗ trợ nhiều công ty thì sửa ở đây và ở nơi gọi,
// phần còn lại đã mang sẵn company_id nên không phải đụng tới.
func (r *CompanyRepository) GetFirst(ctx context.Context) (*domainhr.Company, error) {
	return scanCompany(r.db.QueryRow(ctx, selectCompany+` ORDER BY created_at LIMIT 1`))
}

func (r *CompanyRepository) GetByID(ctx context.Context, id uuid.UUID) (*domainhr.Company, error) {
	return scanCompany(r.db.QueryRow(ctx, selectCompany+` WHERE id = $1`, id))
}
