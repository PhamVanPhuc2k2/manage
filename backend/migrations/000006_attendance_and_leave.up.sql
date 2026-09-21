-- =========================================================================
-- PHASE 3 — CHẤM CÔNG & NGHỈ PHÉP
-- =========================================================================

-- =========================================================================
-- KHUNG GIỜ LÀM VIỆC
--
-- Một bảng phục vụ ba mức áp dụng: công ty, phòng ban, cá nhân. Mức hẹp hơn
-- thắng mức rộng hơn. Tách ba bảng riêng sẽ khiến logic "tìm khung giờ áp
-- dụng cho người này" phải hợp nhất ba nguồn ở mọi chỗ gọi.
-- =========================================================================
CREATE TABLE work_schedules (
    id            UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    company_id    UUID NOT NULL REFERENCES companies(id) ON DELETE RESTRICT,

    -- Cả hai NULL: áp dụng toàn công ty.
    -- department_id: áp dụng cho một phòng. employee_id: cho một người.
    department_id UUID REFERENCES departments(id) ON DELETE CASCADE,
    employee_id   UUID REFERENCES employees(id)   ON DELETE CASCADE,

    name          VARCHAR(255) NOT NULL,
    work_start    TIME NOT NULL,
    work_end      TIME NOT NULL,

    -- Ngày làm việc trong tuần theo ISO: 1 = thứ hai ... 7 = chủ nhật.
    -- Dùng ISO chứ không dùng quy ước của PostgreSQL (0 = chủ nhật) để khớp
    -- với EXTRACT(ISODOW) — trộn hai quy ước là nguồn lỗi lệch một ngày.
    workdays      SMALLINT[] NOT NULL DEFAULT '{1,2,3,4,5}',

    break_minutes INT NOT NULL DEFAULT 60,

    -- Số phút đi muộn được bỏ qua. Không có nó thì kẹt xe 3 phút cũng thành
    -- vi phạm, và bảng công đầy những con số vô nghĩa.
    grace_minutes INT NOT NULL DEFAULT 10,

    effective_from DATE NOT NULL DEFAULT CURRENT_DATE,
    effective_to   DATE,

    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_work_schedules_hours CHECK (work_start < work_end),
    CONSTRAINT chk_work_schedules_effective
        CHECK (effective_to IS NULL OR effective_to >= effective_from),
    -- Một dòng chỉ được thuộc MỘT mức áp dụng.
    CONSTRAINT chk_work_schedules_scope
        CHECK (department_id IS NULL OR employee_id IS NULL)
);

CREATE INDEX idx_work_schedules_dept ON work_schedules (department_id)
  WHERE department_id IS NOT NULL;
CREATE INDEX idx_work_schedules_emp  ON work_schedules (employee_id)
  WHERE employee_id IS NOT NULL;

-- =========================================================================
-- NGÀY LỄ
-- =========================================================================
CREATE TABLE holidays (
    id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    company_id   UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    holiday_date DATE NOT NULL,
    name         VARCHAR(255) NOT NULL,
    -- Ngày lễ có hưởng lương hay không, Phase 4 sẽ dùng.
    is_paid      BOOLEAN NOT NULL DEFAULT TRUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (company_id, holiday_date)
);

-- =========================================================================
-- PHIÊN LÀM VIỆC
--
-- Mỗi dòng là một khoảng thời gian liên tục người đó có mặt. Job nền quét
-- presence mỗi phút và NỚI DÀI phiên gần nhất thay vì tạo dòng mới, nên một
-- ngày làm việc bình thường chỉ sinh một vài dòng chứ không phải 480.
-- =========================================================================
CREATE TYPE attendance_source AS ENUM ('presence', 'manual', 'adjustment');

CREATE TABLE attendance_sessions (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    employee_id UUID NOT NULL REFERENCES employees(id) ON DELETE CASCADE,

    -- Ngày làm việc theo MÚI GIỜ CÔNG TY, không phải theo UTC.
    -- Tính sẵn lúc ghi để mọi truy vấn báo cáo khỏi phải đổi múi giờ.
    work_date   DATE NOT NULL,

    started_at  TIMESTAMPTZ NOT NULL,
    ended_at    TIMESTAMPTZ NOT NULL,

    -- Số phút CÓ HOẠT ĐỘNG trong phiên. Luôn <= độ dài phiên: mở tab rồi bỏ
    -- đó vẫn tính là online nhưng không tính là hoạt động.
    active_minutes INT NOT NULL DEFAULT 0,

    source      attendance_source NOT NULL DEFAULT 'presence',
    note        TEXT,

    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_attendance_sessions_range CHECK (ended_at >= started_at),
    CONSTRAINT chk_attendance_sessions_active CHECK (active_minutes >= 0)
);

CREATE INDEX idx_attendance_sessions_emp_date
  ON attendance_sessions (employee_id, work_date);
-- Phục vụ job nối dài phiên: tìm phiên gần nhất của một người.
CREATE INDEX idx_attendance_sessions_latest
  ON attendance_sessions (employee_id, ended_at DESC);

-- =========================================================================
-- TỔNG HỢP THEO NGÀY
-- =========================================================================
CREATE TYPE attendance_day_status AS ENUM
    ('present', 'absent', 'leave', 'holiday', 'weekend');

CREATE TABLE attendance_days (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    employee_id UUID NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    work_date   DATE NOT NULL,

    online_minutes INT NOT NULL DEFAULT 0,
    active_minutes INT NOT NULL DEFAULT 0,

    first_seen_at TIMESTAMPTZ,
    last_seen_at  TIMESTAMPTZ,

    late_minutes        INT NOT NULL DEFAULT 0,
    early_leave_minutes INT NOT NULL DEFAULT 0,
    shortfall_minutes   INT NOT NULL DEFAULT 0,

    status      attendance_day_status NOT NULL DEFAULT 'absent',

    -- Khoá lại khi chốt kỳ lương. Sau khi khoá, mọi thay đổi phải đi qua
    -- một bản ghi điều chỉnh riêng chứ không sửa đè lên đây — xem bảng rủi
    -- ro "Tính lương sai do sửa dữ liệu công sau khi chốt".
    is_locked   BOOLEAN NOT NULL DEFAULT FALSE,

    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (employee_id, work_date)
);

CREATE INDEX idx_attendance_days_date ON attendance_days (work_date);

-- =========================================================================
-- YÊU CẦU ĐIỀU CHỈNH CÔNG
--
-- Mất mạng, họp ở ngoài, quên mở máy — presence không ghi nhận được. Không
-- có đường sửa hợp lệ thì người dùng sẽ tìm cách khác, và dữ liệu công mất
-- hết giá trị.
-- =========================================================================
CREATE TYPE approval_status AS ENUM ('pending', 'approved', 'rejected', 'cancelled');

CREATE TABLE attendance_adjustments (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    employee_id UUID NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    work_date   DATE NOT NULL,

    requested_start TIMESTAMPTZ NOT NULL,
    requested_end   TIMESTAMPTZ NOT NULL,
    reason          TEXT NOT NULL,

    status       approval_status NOT NULL DEFAULT 'pending',
    approver_id  UUID REFERENCES employees(id) ON DELETE SET NULL,
    decided_at   TIMESTAMPTZ,
    decision_note TEXT,

    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_adjustments_range CHECK (requested_end > requested_start)
);

CREATE INDEX idx_adjustments_emp ON attendance_adjustments (employee_id, work_date);
CREATE INDEX idx_adjustments_pending ON attendance_adjustments (status)
  WHERE status = 'pending';

-- =========================================================================
-- NGHỈ PHÉP
-- =========================================================================
CREATE TYPE leave_type AS ENUM ('annual', 'sick', 'unpaid', 'maternity', 'other');

-- Nửa ngày rất phổ biến trong thực tế (đi khám buổi sáng, về quê buổi chiều).
CREATE TYPE leave_day_part AS ENUM ('full', 'morning', 'afternoon');

CREATE TABLE leave_requests (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    employee_id UUID NOT NULL REFERENCES employees(id) ON DELETE CASCADE,

    leave_type  leave_type NOT NULL,
    start_date  DATE NOT NULL,
    end_date    DATE NOT NULL,
    day_part    leave_day_part NOT NULL DEFAULT 'full',

    -- Số ngày quy đổi, tính sẵn lúc tạo đơn.
    --
    -- Lưu sẵn chứ không tính lại lúc đọc: lịch nghỉ lễ của công ty có thể
    -- đổi sau khi đơn đã duyệt, và khi đó số ngày đã trừ vào quỹ phép phải
    -- giữ nguyên như lúc duyệt.
    days        NUMERIC(4,1) NOT NULL,

    reason      TEXT,
    status      approval_status NOT NULL DEFAULT 'pending',

    approver_id   UUID REFERENCES employees(id) ON DELETE SET NULL,
    decided_at    TIMESTAMPTZ,
    decision_note TEXT,

    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_leave_range CHECK (end_date >= start_date),
    CONSTRAINT chk_leave_days  CHECK (days > 0),
    -- Nửa ngày chỉ có nghĩa khi đơn gói gọn trong một ngày.
    CONSTRAINT chk_leave_day_part
        CHECK (day_part = 'full' OR start_date = end_date)
);

CREATE INDEX idx_leave_requests_emp ON leave_requests (employee_id, start_date);
CREATE INDEX idx_leave_requests_pending ON leave_requests (status)
  WHERE status = 'pending';
-- Phục vụ câu hỏi "ngày này ai nghỉ": quét theo khoảng ngày.
CREATE INDEX idx_leave_requests_range ON leave_requests (start_date, end_date)
  WHERE status = 'approved';

-- =========================================================================
-- QUỸ NGÀY PHÉP
-- =========================================================================
CREATE TABLE leave_balances (
    employee_id UUID NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    year        INT  NOT NULL,

    entitled_days     NUMERIC(4,1) NOT NULL DEFAULT 12,
    -- Phép tồn từ năm trước. Tách riêng để báo cáo trả lời được câu hỏi
    -- "phép năm nay dùng hết chưa" mà không lẫn với phép cũ.
    carried_over_days NUMERIC(4,1) NOT NULL DEFAULT 0,
    used_days         NUMERIC(4,1) NOT NULL DEFAULT 0,

    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (employee_id, year),

    CONSTRAINT chk_leave_balances_nonneg
        CHECK (entitled_days >= 0 AND carried_over_days >= 0 AND used_days >= 0)
);

-- =========================================================================
-- TRIGGER CẬP NHẬT updated_at
-- =========================================================================
CREATE TRIGGER trg_work_schedules_updated BEFORE UPDATE ON work_schedules
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trg_attendance_sessions_updated BEFORE UPDATE ON attendance_sessions
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trg_attendance_days_updated BEFORE UPDATE ON attendance_days
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trg_adjustments_updated BEFORE UPDATE ON attendance_adjustments
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trg_leave_requests_updated BEFORE UPDATE ON leave_requests
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trg_leave_balances_updated BEFORE UPDATE ON leave_balances
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- =========================================================================
-- KHUNG GIỜ MẶC ĐỊNH CHO CÔNG TY ĐÃ CÓ
--
-- Lấy từ chính cột work_start/work_end của bảng companies, nơi Phase 0 đã
-- đặt sẵn. Không seed thì mọi phép tính đi muộn/về sớm sẽ không có mốc so
-- sánh và im lặng trả về 0.
-- =========================================================================
INSERT INTO work_schedules (company_id, name, work_start, work_end)
SELECT id, 'Khung giờ mặc định', work_start, work_end FROM companies;
