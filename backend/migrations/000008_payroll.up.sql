-- =========================================================================
-- PHASE 4 — LƯƠNG
--
-- QUY ƯỚC VỀ TIỀN: mọi số tiền lưu bằng BIGINT, đơn vị ĐỒNG.
--
-- Không dùng NUMERIC(15,2) và không dùng số thực. Lý do:
--
--   - Lương Việt Nam không có đơn vị nhỏ hơn đồng. Hai chữ số thập phân
--     của NUMERIC(15,2) luôn bằng 0, chỉ tốn chỗ và mời gọi sai sót.
--   - Số thực (float/double) tích luỹ sai số: cộng dồn vài chục khoản phụ
--     cấp rồi nhân tỷ lệ thuế sẽ cho ra những con số lệch vài đồng, và
--     bảng lương lệch một đồng là bảng lương sai.
--   - BIGINT chứa tới 9.2 tỷ tỷ đồng — dư sức cho mọi kỳ lương.
--
-- Riêng TỶ LỆ (phần trăm bảo hiểm, bậc thuế) dùng NUMERIC vì chúng là phân
-- số thật, không phải tiền.
-- =========================================================================

-- =========================================================================
-- THAM SỐ TÍNH LƯƠNG
--
-- Tách khỏi code vì chúng thay đổi theo quy định nhà nước: mức giảm trừ gia
-- cảnh, trần đóng bảo hiểm và bậc thuế đều đã đổi nhiều lần. Hard-code
-- nghĩa là mỗi lần nhà nước sửa luật là phải sửa code và triển khai lại.
-- =========================================================================
CREATE TABLE payroll_settings (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    company_id UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,

    -- Giảm trừ gia cảnh cho bản thân và cho mỗi người phụ thuộc, mỗi tháng.
    personal_deduction  BIGINT NOT NULL DEFAULT 11000000,
    dependent_deduction BIGINT NOT NULL DEFAULT 4400000,

    -- Tỷ lệ bảo hiểm phần NHÂN VIÊN đóng.
    social_rate       NUMERIC(6,4) NOT NULL DEFAULT 0.0800,  -- BHXH 8%
    health_rate       NUMERIC(6,4) NOT NULL DEFAULT 0.0150,  -- BHYT 1.5%
    unemployment_rate NUMERIC(6,4) NOT NULL DEFAULT 0.0100,  -- BHTN 1%

    -- Tỷ lệ bảo hiểm phần CÔNG TY đóng. Không trừ vào lương nhân viên
    -- nhưng là chi phí thật, và báo cáo chi phí nhân sự cần nó.
    employer_social_rate       NUMERIC(6,4) NOT NULL DEFAULT 0.1750,
    employer_health_rate       NUMERIC(6,4) NOT NULL DEFAULT 0.0300,
    employer_unemployment_rate NUMERIC(6,4) NOT NULL DEFAULT 0.0100,

    -- Trần đóng bảo hiểm. Phần lương vượt trần không phải đóng.
    -- NULL nghĩa là không có trần.
    social_cap       BIGINT,
    unemployment_cap BIGINT,

    -- Số ngày công chuẩn một tháng, dùng để chia lương theo ngày làm việc
    -- thực tế. Nhiều công ty dùng 22, một số dùng 26.
    standard_workdays NUMERIC(4,1) NOT NULL DEFAULT 22,

    effective_from DATE NOT NULL DEFAULT CURRENT_DATE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (company_id, effective_from),

    CONSTRAINT chk_payroll_settings_workdays CHECK (standard_workdays > 0)
);

CREATE INDEX idx_payroll_settings_company ON payroll_settings (company_id, effective_from DESC);

-- =========================================================================
-- BẬC THUẾ THU NHẬP CÁ NHÂN
--
-- Luỹ tiến từng phần: mỗi bậc chỉ áp tỷ lệ của nó cho PHẦN thu nhập nằm
-- trong bậc đó, không áp cho toàn bộ. Lưu thành bảng để sửa được khi biểu
-- thuế thay đổi.
-- =========================================================================
CREATE TABLE tax_brackets (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    settings_id UUID NOT NULL REFERENCES payroll_settings(id) ON DELETE CASCADE,

    ordinal     INT    NOT NULL,
    from_amount BIGINT NOT NULL,
    -- NULL = bậc cuối, không giới hạn trên.
    to_amount   BIGINT,
    rate        NUMERIC(6,4) NOT NULL,

    UNIQUE (settings_id, ordinal),

    CONSTRAINT chk_tax_brackets_range
        CHECK (to_amount IS NULL OR to_amount > from_amount),
    CONSTRAINT chk_tax_brackets_rate CHECK (rate >= 0 AND rate <= 1)
);

-- =========================================================================
-- CẤU HÌNH LƯƠNG THEO NHÂN VIÊN
--
-- Có hiệu lực theo khoảng thời gian, nên mỗi lần tăng lương là một dòng
-- MỚI chứ không sửa đè. Sửa đè sẽ xoá mất lịch sử, và không ai trả lời
-- được câu "tháng ba năm ngoái lương người này là bao nhiêu" — câu hỏi luôn
-- xuất hiện khi tính lại hoặc khi có tranh chấp.
-- =========================================================================
CREATE TABLE salary_structures (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    employee_id UUID NOT NULL REFERENCES employees(id) ON DELETE CASCADE,

    base_salary BIGINT NOT NULL,

    -- Lương đóng bảo hiểm. Thường bằng lương cơ bản nhưng nhiều công ty
    -- tách riêng. NULL = dùng base_salary.
    insurance_salary BIGINT,

    dependents INT NOT NULL DEFAULT 0,

    -- Thông tin ngân hàng. Số tài khoản chỉ hiển thị 4 số cuối ở tầng API —
    -- xem usecase/payroll. Lưu đủ vì bộ phận kế toán cần để chuyển khoản.
    bank_account VARCHAR(50),
    bank_name    VARCHAR(255),

    effective_from DATE NOT NULL,
    effective_to   DATE,
    note           TEXT,

    created_by UUID REFERENCES employees(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_salary_base CHECK (base_salary >= 0),
    CONSTRAINT chk_salary_dependents CHECK (dependents >= 0),
    CONSTRAINT chk_salary_effective
        CHECK (effective_to IS NULL OR effective_to >= effective_from)
);

CREATE INDEX idx_salary_structures_emp
  ON salary_structures (employee_id, effective_from DESC);

-- =========================================================================
-- THÀNH PHẦN LƯƠNG: phụ cấp, thưởng, khấu trừ
-- =========================================================================
CREATE TYPE salary_component_kind AS ENUM ('allowance', 'bonus', 'deduction');

CREATE TABLE salary_components (
    id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    structure_id UUID NOT NULL REFERENCES salary_structures(id) ON DELETE CASCADE,

    kind   salary_component_kind NOT NULL,
    code   VARCHAR(50)  NOT NULL,
    name   VARCHAR(255) NOT NULL,
    amount BIGINT       NOT NULL,

    -- Chịu thuế hay không. Phụ cấp ăn trưa trong mức quy định và phụ cấp
    -- điện thoại theo chứng từ thì không chịu thuế; thưởng thì có.
    taxable BOOLEAN NOT NULL DEFAULT TRUE,

    -- Có chia theo ngày công thực tế hay không.
    --
    -- Phụ cấp ăn trưa tính theo ngày đi làm (prorated), phụ cấp trách nhiệm
    -- thì trả đủ dù nghỉ vài hôm. Gộp hai loại làm một sẽ tính sai cho một
    -- trong hai, và không ai phát hiện cho tới khi nhân viên thắc mắc.
    prorated BOOLEAN NOT NULL DEFAULT FALSE,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (structure_id, code),
    CONSTRAINT chk_salary_components_amount CHECK (amount >= 0)
);

-- =========================================================================
-- KỲ LƯƠNG
-- =========================================================================
CREATE TYPE payroll_status AS ENUM
    ('draft', 'calculating', 'locked', 'paid', 'cancelled');

CREATE TABLE payroll_periods (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    company_id UUID NOT NULL REFERENCES companies(id) ON DELETE RESTRICT,

    year  INT NOT NULL,
    month INT NOT NULL,
    name  VARCHAR(255) NOT NULL,

    period_start DATE NOT NULL,
    period_end   DATE NOT NULL,

    status payroll_status NOT NULL DEFAULT 'draft',

    -- Số tổng, tính sẵn lúc chạy máy tính lương.
    --
    -- Lưu sẵn chứ không SUM lại mỗi lần đọc: danh sách kỳ lương hiển thị
    -- tổng chi của từng kỳ, và SUM qua hàng nghìn phiếu cho mỗi dòng danh
    -- sách là truy vấn N+1 ở dạng tệ nhất.
    total_gross    BIGINT NOT NULL DEFAULT 0,
    total_net      BIGINT NOT NULL DEFAULT 0,
    total_tax      BIGINT NOT NULL DEFAULT 0,
    total_insurance BIGINT NOT NULL DEFAULT 0,
    employee_count INT    NOT NULL DEFAULT 0,

    calculated_at TIMESTAMPTZ,
    locked_at     TIMESTAMPTZ,
    paid_at       TIMESTAMPTZ,

    created_by UUID REFERENCES employees(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (company_id, year, month),

    CONSTRAINT chk_payroll_periods_month CHECK (month BETWEEN 1 AND 12),
    CONSTRAINT chk_payroll_periods_range CHECK (period_end >= period_start)
);

CREATE INDEX idx_payroll_periods_status ON payroll_periods (status);

-- =========================================================================
-- PHIẾU LƯƠNG
--
-- Mỗi dòng là ẢNH CHỤP kết quả tính tại thời điểm chạy, không phải một khung
-- nhìn tính lại mỗi lần đọc. Lương đã trả thì con số phải đứng yên vĩnh
-- viễn, kể cả khi cấu hình lương, biểu thuế hay dữ liệu công thay đổi sau
-- đó. Đây là lý do mọi trường đều lưu giá trị cụ thể chứ không lưu tham
-- chiếu tới cấu hình.
-- =========================================================================
CREATE TABLE payslips (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    period_id   UUID NOT NULL REFERENCES payroll_periods(id) ON DELETE CASCADE,
    employee_id UUID NOT NULL REFERENCES employees(id) ON DELETE RESTRICT,

    -- Ảnh chụp dữ liệu công.
    standard_workdays NUMERIC(5,2) NOT NULL,
    actual_workdays   NUMERIC(5,2) NOT NULL,
    leave_days        NUMERIC(5,2) NOT NULL DEFAULT 0,
    absent_days       NUMERIC(5,2) NOT NULL DEFAULT 0,

    -- Các con số tiền, theo đúng thứ tự phép tính.
    base_salary   BIGINT NOT NULL DEFAULT 0,  -- lương cơ bản đã chia theo công
    allowances    BIGINT NOT NULL DEFAULT 0,
    bonuses       BIGINT NOT NULL DEFAULT 0,
    gross_salary  BIGINT NOT NULL DEFAULT 0,

    insurance_base     BIGINT NOT NULL DEFAULT 0,
    insurance_employee BIGINT NOT NULL DEFAULT 0,
    insurance_employer BIGINT NOT NULL DEFAULT 0,

    taxable_income      BIGINT NOT NULL DEFAULT 0,
    personal_deduction  BIGINT NOT NULL DEFAULT 0,
    dependent_deduction BIGINT NOT NULL DEFAULT 0,
    assessable_income   BIGINT NOT NULL DEFAULT 0,
    income_tax          BIGINT NOT NULL DEFAULT 0,

    other_deductions BIGINT NOT NULL DEFAULT 0,
    net_salary       BIGINT NOT NULL DEFAULT 0,

    dependents INT NOT NULL DEFAULT 0,
    note       TEXT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (period_id, employee_id)
);

CREATE INDEX idx_payslips_employee ON payslips (employee_id);

-- =========================================================================
-- DÒNG CHI TIẾT PHIẾU LƯƠNG
--
-- Để phiếu lương giải thích được từng con số. Thiếu bảng này thì nhân viên
-- chỉ thấy một khoản "phụ cấp: 3.000.000" mà không biết gồm những gì, và
-- mọi thắc mắc đều phải hỏi kế toán.
-- =========================================================================
CREATE TABLE payslip_items (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    payslip_id UUID NOT NULL REFERENCES payslips(id) ON DELETE CASCADE,

    kind    salary_component_kind NOT NULL,
    code    VARCHAR(50)  NOT NULL,
    name    VARCHAR(255) NOT NULL,
    amount  BIGINT       NOT NULL,
    taxable BOOLEAN      NOT NULL DEFAULT TRUE,
    sort_order INT       NOT NULL DEFAULT 0,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_payslip_items_payslip ON payslip_items (payslip_id, sort_order);

-- =========================================================================
-- NHẬT KÝ TRUY CẬP DỮ LIỆU NHẠY CẢM
--
-- Bảng chỉ ghi thêm. Ghi lại MỌI lượt xem và sửa dữ liệu lương, không chỉ
-- lượt sửa: rò rỉ bảng lương thường là do đọc chứ không phải do ghi, và
-- không có nhật ký đọc thì không bao giờ biết ai đã xem gì.
-- =========================================================================
CREATE TABLE audit_logs (
    id       UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    actor_id UUID REFERENCES employees(id) ON DELETE SET NULL,

    action      VARCHAR(50) NOT NULL,
    resource    VARCHAR(50) NOT NULL,
    resource_id UUID,

    -- Chi tiết tuỳ loại hành động. JSONB để không phải sửa lược đồ mỗi lần
    -- thêm một loại sự kiện cần ghi.
    detail JSONB,

    ip         VARCHAR(45),
    request_id VARCHAR(64),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_audit_logs_resource ON audit_logs (resource, resource_id, created_at DESC);
CREATE INDEX idx_audit_logs_actor    ON audit_logs (actor_id, created_at DESC);

-- =========================================================================
-- TRIGGER CẬP NHẬT updated_at
-- =========================================================================
CREATE TRIGGER trg_payroll_settings_updated BEFORE UPDATE ON payroll_settings
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trg_salary_structures_updated BEFORE UPDATE ON salary_structures
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trg_payroll_periods_updated BEFORE UPDATE ON payroll_periods
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trg_payslips_updated BEFORE UPDATE ON payslips
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- =========================================================================
-- THAM SỐ MẶC ĐỊNH VÀ BIỂU THUẾ LUỸ TIẾN
--
-- Biểu thuế 7 bậc theo Luật Thuế thu nhập cá nhân hiện hành. Seed sẵn để hệ
-- thống tính được ngay; nhân sự sửa lại khi quy định thay đổi.
-- =========================================================================
-- effective_from lùi về quá khứ xa, KHÔNG phải ngày cài đặt.
--
-- Đây là bản tham số MẶC ĐỊNH: nó phải áp dụng cho mọi kỳ lương, kể cả kỳ
-- của những tháng trước khi hệ thống được cài. Đặt CURRENT_DATE nghĩa là
-- không tính được lương cho bất kỳ tháng nào trước ngày cài — và lỗi đó chỉ
-- lộ ra khi ai đó thử tính kỳ đầu tiên.
INSERT INTO payroll_settings (company_id, effective_from, social_cap, unemployment_cap)
SELECT id, DATE '2000-01-01',
       -- Trần BHXH/BHYT: 20 lần mức lương cơ sở.
       20 * 2340000,
       -- Trần BHTN: 20 lần mức lương tối thiểu vùng I.
       20 * 4960000
FROM companies;

INSERT INTO tax_brackets (settings_id, ordinal, from_amount, to_amount, rate)
SELECT s.id, b.ordinal, b.from_amount, b.to_amount, b.rate
FROM payroll_settings s
CROSS JOIN (VALUES
    (1,         0::BIGINT,   5000000::BIGINT, 0.05),
    (2,   5000000::BIGINT,  10000000::BIGINT, 0.10),
    (3,  10000000::BIGINT,  18000000::BIGINT, 0.15),
    (4,  18000000::BIGINT,  32000000::BIGINT, 0.20),
    (5,  32000000::BIGINT,  52000000::BIGINT, 0.25),
    (6,  52000000::BIGINT,  80000000::BIGINT, 0.30),
    (7,  80000000::BIGINT,          NULL,     0.35)
) AS b(ordinal, from_amount, to_amount, rate);
