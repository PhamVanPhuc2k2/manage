-- Chạy một lần duy nhất khi volume pgdata còn rỗng.
-- Sửa file này sau đó sẽ KHÔNG có tác dụng cho tới khi xoá volume (make destroy).

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- pg_trgm + unaccent phục vụ tìm kiếm tiếng Việt không dấu:
-- gõ "nguyen van a" vẫn tìm ra "Nguyễn Văn A".
CREATE EXTENSION IF NOT EXISTS "pg_trgm";
CREATE EXTENSION IF NOT EXISTS "unaccent";
