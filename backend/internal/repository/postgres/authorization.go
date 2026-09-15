package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	"github.com/PhamVanPhuc2k2/manage/pkg/postgres"
)

// AuthorizationReader tính quyền của một người dùng.
type AuthorizationReader struct {
	db *postgres.DB
}

func NewAuthorizationReader(db *postgres.DB) *AuthorizationReader {
	return &AuthorizationReader{db: db}
}

// Load gom vai trò, quyền, phạm vi và danh sách phòng ban quản lý.
//
// Gọi ở hai thời điểm: lúc đăng nhập và lúc làm mới token. Kết quả được nhét
// vào access token nên middleware không phải truy vấn lại mỗi request.
func (r *AuthorizationReader) Load(
	ctx context.Context,
	userID, employeeID uuid.UUID,
) (*domainauth.Authorization, error) {
	const q = `
		SELECT r.code, r.scope, COALESCE(p.code, '')
		FROM user_roles ur
		JOIN roles r                 ON r.id = ur.role_id
		LEFT JOIN role_permissions rp ON rp.role_id = r.id
		LEFT JOIN permissions p       ON p.id = rp.permission_id
		WHERE ur.user_id = $1`

	rows, err := r.db.Query(ctx, q, userID)
	if err != nil {
		return nil, fmt.Errorf("đọc phân quyền: %w", err)
	}
	defer rows.Close()

	roleSet := map[string]struct{}{}
	permSet := map[string]struct{}{}
	scope := domainauth.ScopeSelf

	for rows.Next() {
		var roleCode, scopeStr, permCode string
		if err := rows.Scan(&roleCode, &scopeStr, &permCode); err != nil {
			return nil, err
		}
		roleSet[roleCode] = struct{}{}
		if permCode != "" {
			permSet[permCode] = struct{}{}
		}
		// Một người có nhiều vai trò thì lấy phạm vi RỘNG NHẤT.
		if s := domainauth.Scope(scopeStr); s.Rank() > scope.Rank() {
			scope = s
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	auth := &domainauth.Authorization{
		Roles:       keysOf(roleSet),
		Permissions: keysOf(permSet),
		Scope:       scope,
	}

	// Phạm vi "department" cần biết chính xác những phòng nào thuộc quyền.
	// Gồm phòng mà người này làm trưởng, phòng mà người này đang thuộc về,
	// VÀ toàn bộ phòng con của chúng.
	if scope == domainauth.ScopeDepartment {
		ids, err := r.loadManagedDepartments(ctx, employeeID)
		if err != nil {
			return nil, err
		}
		auth.ManagedDepartmentIDs = ids
	}

	return auth, nil
}

// loadManagedDepartments trả về phòng ban thuộc quyền quản lý, gồm cả
// phòng con ở mọi cấp.
//
// Van depth < 10 phòng trường hợp dữ liệu đã lỡ có vòng lặp (do sửa tay,
// do migration sai). Không có nó, WITH RECURSIVE sẽ chạy vô hạn và treo
// cả connection pool.
func (r *AuthorizationReader) loadManagedDepartments(
	ctx context.Context,
	employeeID uuid.UUID,
) ([]uuid.UUID, error) {
	const q = `
		WITH RECURSIVE roots AS (
		    -- Phòng mà người này làm trưởng
		    SELECT d.id FROM departments d
		    WHERE d.manager_id = $1 AND d.deleted_at IS NULL
		    UNION
		    -- Phòng mà người này đang thuộc về
		    SELECT e.department_id FROM employees e
		    WHERE e.id = $1 AND e.department_id IS NOT NULL AND e.deleted_at IS NULL
		),
		subtree AS (
		    SELECT id, 0 AS depth FROM roots

		    UNION ALL

		    SELECT d.id, s.depth + 1
		    FROM departments d
		    JOIN subtree s ON d.parent_id = s.id
		    WHERE d.deleted_at IS NULL AND s.depth < 10
		)
		SELECT DISTINCT id FROM subtree WHERE id IS NOT NULL`

	rows, err := r.db.Query(ctx, q, employeeID)
	if err != nil {
		return nil, fmt.Errorf("đọc phòng ban quản lý: %w", err)
	}
	defer rows.Close()

	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func keysOf(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
