-- Phase 0 chỉ tạo một bảng nhỏ để chứng minh đường migration chạy được.
-- Bảng nghiệp vụ thật bắt đầu từ Phase 1.

-- Extension phải nằm Ở ĐÂY, không chỉ ở docker/postgres/init/.
--
-- Thư mục init của image postgres chỉ chạy khi volume dữ liệu còn RỖNG. Mọi
-- đường tạo database khác — một database mới trên cùng máy chủ, dịch vụ
-- postgres của GitHub Actions, một container kiểm thử — đều không chạy nó,
-- và migration 000002 chết ngay với "function uuid_generate_v4() does not
-- exist". Thông báo đó không gợi ý gì về nguyên nhân thật.
--
-- Extension là một phần của hợp đồng schema, nên nó thuộc về migration.
-- Giữ tiếp tệp init vì nó vô hại và chạy sớm hơn.
--
-- Cần quyền tạo extension. Đúng với PostgreSQL tự dựng (đây là trường hợp
-- của dự án). Trên dịch vụ quản lý như RDS thì quản trị viên phải tạo
-- trước; IF NOT EXISTS khi đó biến bước này thành vô hại.
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pg_trgm";
CREATE EXTENSION IF NOT EXISTS "unaccent";

CREATE TABLE IF NOT EXISTS system_info (
    key        VARCHAR(100) PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO system_info (key, value)
VALUES ('schema_initialized_at', NOW()::TEXT)
ON CONFLICT (key) DO NOTHING;
