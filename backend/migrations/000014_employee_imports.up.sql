-- Nhập nhân viên hàng loạt từ CSV/Excel.
--
-- Mỗi lượt nhập là MỘT bản ghi: tệp đã đọc thành các dòng (rows), và kết
-- quả từng dòng được nối dần vào results trong lúc worker chạy.
--
-- Vì sao lưu cả rows vào database chứ không để trong message RabbitMQ:
-- worker có thể chết giữa chừng và message được giao lại. Khi đó nó đọc
-- results để biết đã làm tới dòng nào rồi làm tiếp — không tạo trùng những
-- người đã tạo. Message chỉ mang id.

CREATE TYPE employee_import_status AS ENUM ('queued', 'processing', 'done', 'failed');

CREATE TABLE employee_imports (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    company_id      UUID NOT NULL REFERENCES companies(id) ON DELETE RESTRICT,
    file_name       VARCHAR(255) NOT NULL,
    status          employee_import_status NOT NULL DEFAULT 'queued',
    create_accounts BOOLEAN NOT NULL DEFAULT FALSE,
    total_rows      INT NOT NULL,
    rows            JSONB NOT NULL,
    results         JSONB NOT NULL DEFAULT '[]'::jsonb,

    -- Người bấm nhập, và phạm vi quyền của họ ĐÚNG lúc bấm. Worker dựng lại
    -- actor từ đây: tạo tài khoản cho người ngoài phạm vi của mình thì qua
    -- đường nhập hàng loạt cũng không được, y như qua form.
    created_by      UUID REFERENCES employees(id) ON DELETE SET NULL,
    actor           JSONB NOT NULL,

    error           TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at      TIMESTAMPTZ,
    finished_at     TIMESTAMPTZ
);

CREATE INDEX idx_employee_imports_company ON employee_imports (company_id, created_at DESC);
CREATE INDEX idx_employee_imports_creator ON employee_imports (created_by, created_at DESC);
