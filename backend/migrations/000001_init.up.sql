-- Phase 0 chỉ tạo một bảng nhỏ để chứng minh đường migration chạy được.
-- Bảng nghiệp vụ thật bắt đầu từ Phase 1.

CREATE TABLE IF NOT EXISTS system_info (
    key        VARCHAR(100) PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO system_info (key, value)
VALUES ('schema_initialized_at', NOW()::TEXT)
ON CONFLICT (key) DO NOTHING;
