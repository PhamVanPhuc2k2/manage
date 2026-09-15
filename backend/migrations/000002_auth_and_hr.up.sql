-- =========================================================================
-- CÔNG TY
-- =========================================================================
CREATE TABLE companies (
    id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name         VARCHAR(255) NOT NULL,
    tax_code     VARCHAR(50),
    address      TEXT,
    timezone     VARCHAR(64)  NOT NULL DEFAULT 'Asia/Ho_Chi_Minh',
    -- Khung giờ làm việc mặc định, Phase 3 dùng để tính đi muộn về sớm.
    work_start   TIME         NOT NULL DEFAULT '08:00',
    work_end     TIME         NOT NULL DEFAULT '17:30',
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- =========================================================================
-- PHÒNG BAN — cây bằng danh sách kề
-- =========================================================================
CREATE TABLE departments (
    id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    company_id   UUID NOT NULL REFERENCES companies(id) ON DELETE RESTRICT,
    parent_id    UUID REFERENCES departments(id) ON DELETE RESTRICT,
    code         VARCHAR(50)  NOT NULL,
    name         VARCHAR(255) NOT NULL,
    description  TEXT,
    -- Trưởng phòng. NULL được vì lúc tạo phòng có thể chưa có người.
    -- Khoá ngoại gắn sau khi bảng employees tồn tại (xem cuối file).
    manager_id   UUID,
    deleted_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_departments_code
  ON departments (company_id, lower(code)) WHERE deleted_at IS NULL;
CREATE INDEX idx_departments_parent ON departments (parent_id);

-- =========================================================================
-- CHỨC VỤ
-- =========================================================================
CREATE TABLE positions (
    id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    company_id   UUID NOT NULL REFERENCES companies(id) ON DELETE RESTRICT,
    code         VARCHAR(50)  NOT NULL,
    name         VARCHAR(255) NOT NULL,
    -- Dải lương tham khảo, Phase 4 dùng để cảnh báo khi đặt lương ngoài khung.
    salary_min   NUMERIC(15,2),
    salary_max   NUMERIC(15,2),
    deleted_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_positions_salary_range
        CHECK (salary_min IS NULL OR salary_max IS NULL OR salary_min <= salary_max)
);

CREATE UNIQUE INDEX idx_positions_code
  ON positions (company_id, lower(code)) WHERE deleted_at IS NULL;

-- =========================================================================
-- NHÂN VIÊN
-- =========================================================================
CREATE TYPE work_mode AS ENUM ('onsite', 'remote', 'hybrid');
CREATE TYPE employee_status AS ENUM ('probation', 'official', 'resigned');

CREATE TABLE employees (
    id             UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    company_id     UUID NOT NULL REFERENCES companies(id) ON DELETE RESTRICT,
    employee_code  VARCHAR(50)  NOT NULL,
    full_name      VARCHAR(255) NOT NULL,
    email          VARCHAR(255) NOT NULL,
    phone          VARCHAR(20),
    date_of_birth  DATE,
    gender         VARCHAR(10),
    address        TEXT,

    department_id  UUID REFERENCES departments(id) ON DELETE RESTRICT,
    position_id    UUID REFERENCES positions(id)   ON DELETE RESTRICT,
    -- Cấp trên trực tiếp. Tự tham chiếu, cũng cần chặn vòng lặp như phòng ban.
    manager_id     UUID REFERENCES employees(id)   ON DELETE SET NULL,

    work_mode      work_mode       NOT NULL DEFAULT 'onsite',
    status         employee_status NOT NULL DEFAULT 'probation',
    joined_at      DATE NOT NULL,
    resigned_at    DATE,

    avatar_key     VARCHAR(500),   -- khoá object trong MinIO, KHÔNG phải URL

    deleted_at     TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_employees_resigned
        CHECK (resigned_at IS NULL OR resigned_at >= joined_at),
    -- Không cho phép tự làm cấp trên của chính mình.
    CONSTRAINT chk_employees_self_manager
        CHECK (manager_id IS NULL OR manager_id <> id)
);

CREATE UNIQUE INDEX idx_employees_code
  ON employees (company_id, lower(employee_code)) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX idx_employees_email
  ON employees (lower(email)) WHERE deleted_at IS NULL;
CREATE INDEX idx_employees_department ON employees (department_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_employees_manager    ON employees (manager_id)    WHERE deleted_at IS NULL;

-- Giờ mới gắn được khoá ngoại manager_id của departments.
ALTER TABLE departments
  ADD CONSTRAINT fk_departments_manager
  FOREIGN KEY (manager_id) REFERENCES employees(id) ON DELETE SET NULL;

-- =========================================================================
-- TÌM KIẾM TIẾNG VIỆT KHÔNG DẤU
-- =========================================================================
-- unaccent() KHÔNG phải hàm IMMUTABLE nên PostgreSQL từ chối đánh chỉ số
-- trực tiếp lên nó. Bọc lại thành hàm IMMUTABLE mới tạo được GIN index.
--
-- Đây là một lời hứa với PostgreSQL: nếu sau này ai sửa từ điển unaccent
-- thì chỉ số sẽ sai và phải REINDEX. Thực tế không ai sửa.
CREATE OR REPLACE FUNCTION f_unaccent(text)
RETURNS text
LANGUAGE sql
IMMUTABLE PARALLEL SAFE STRICT
AS $$ SELECT public.unaccent('public.unaccent'::regdictionary, $1) $$;

CREATE INDEX idx_employees_name_trgm
  ON employees USING gin (f_unaccent(lower(full_name)) gin_trgm_ops);
CREATE INDEX idx_employees_code_trgm
  ON employees USING gin (lower(employee_code) gin_trgm_ops);

-- =========================================================================
-- TÀI KHOẢN ĐĂNG NHẬP
-- =========================================================================
CREATE TABLE users (
    id               UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    employee_id      UUID NOT NULL UNIQUE REFERENCES employees(id) ON DELETE RESTRICT,
    email            VARCHAR(255) NOT NULL,
    password_hash    VARCHAR(255) NOT NULL,
    is_active        BOOLEAN NOT NULL DEFAULT TRUE,

    -- Bắt đổi mật khẩu ở lần đăng nhập đầu (tài khoản do HR tạo hộ).
    must_change_password BOOLEAN NOT NULL DEFAULT TRUE,

    last_login_at    TIMESTAMPTZ,
    -- Redis lo phần khoá tạm thời; hai cột này giữ lại để HR nhìn thấy
    -- lịch sử bất thường của tài khoản.
    failed_attempts  INT NOT NULL DEFAULT 0,
    locked_until     TIMESTAMPTZ,

    deleted_at       TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Mệnh đề WHERE cho phép tái sử dụng email của người đã nghỉ việc.
CREATE UNIQUE INDEX idx_users_email_active
  ON users (lower(email)) WHERE deleted_at IS NULL;

-- =========================================================================
-- RBAC
-- =========================================================================
CREATE TYPE data_scope AS ENUM ('all', 'department', 'self');

CREATE TABLE roles (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    code        VARCHAR(50)  NOT NULL UNIQUE,
    name        VARCHAR(255) NOT NULL,
    description TEXT,
    scope       data_scope   NOT NULL DEFAULT 'self',
    -- Vai trò hệ thống thì không cho sửa hay xoá qua API.
    is_system   BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE permissions (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    code        VARCHAR(100) NOT NULL UNIQUE,   -- dạng "employee:update"
    resource    VARCHAR(50)  NOT NULL,
    action      VARCHAR(50)  NOT NULL,
    description TEXT
);

CREATE TABLE role_permissions (
    role_id       UUID NOT NULL REFERENCES roles(id)       ON DELETE CASCADE,
    permission_id UUID NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,
    PRIMARY KEY (role_id, permission_id)
);

CREATE TABLE user_roles (
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id     UUID NOT NULL REFERENCES roles(id) ON DELETE RESTRICT,
    assigned_by UUID REFERENCES users(id) ON DELETE SET NULL,
    assigned_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, role_id)
);

CREATE INDEX idx_user_roles_user ON user_roles (user_id);

-- =========================================================================
-- TRIGGER CẬP NHẬT updated_at
--
-- Dùng trigger thay vì để Go tự đặt, vì trigger không quên: migration sửa
-- dữ liệu trực tiếp, script dọn dẹp, người sửa tay trong Adminer — tất cả
-- đều bỏ qua code Go.
-- =========================================================================
CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END $$;

CREATE TRIGGER trg_companies_updated   BEFORE UPDATE ON companies
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trg_departments_updated BEFORE UPDATE ON departments
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trg_positions_updated   BEFORE UPDATE ON positions
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trg_employees_updated   BEFORE UPDATE ON employees
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trg_users_updated       BEFORE UPDATE ON users
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
