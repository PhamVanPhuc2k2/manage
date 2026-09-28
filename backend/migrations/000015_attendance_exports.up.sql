-- Xuất báo cáo chấm công ra Excel, chạy nền.
--
-- Tệp kết quả lưu thẳng trong bảng (BYTEA) chứ không lên R2: một tháng của
-- vài trăm người chỉ vài chục KB, và chức năng này phải chạy được cả khi
-- chưa cấu hình R2. Tệp quá 7 ngày bị xoá (chỉ cột file, bản ghi giữ lại)
-- mỗi khi worker làm xong một lượt xuất mới — xem ProcessExport.

CREATE TYPE attendance_export_status AS ENUM ('queued', 'processing', 'done', 'failed');

CREATE TABLE attendance_exports (
    id            UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    year          INT NOT NULL CHECK (year BETWEEN 2000 AND 2200),
    month         INT NOT NULL CHECK (month BETWEEN 1 AND 12),
    department_id UUID REFERENCES departments(id) ON DELETE SET NULL,
    status        attendance_export_status NOT NULL DEFAULT 'queued',

    -- Người yêu cầu và phạm vi quyền của họ ĐÚNG lúc yêu cầu. Worker dựng
    -- lại actor từ đây: trưởng phòng xuất thì tệp chỉ có người trong phòng
    -- mình, y như khi xem trên màn hình.
    requested_by  UUID REFERENCES employees(id) ON DELETE SET NULL,
    actor         JSONB NOT NULL,

    file_name     VARCHAR(255),
    file          BYTEA,
    employee_count INT,
    error         TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at   TIMESTAMPTZ
);

CREATE INDEX idx_attendance_exports_requester ON attendance_exports (requested_by, created_at DESC);
