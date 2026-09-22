-- Chỉ gỡ hàm. KHÔNG xoá dữ liệu mặc định đã tạo: chúng có thể đã được sửa
-- (đổi giờ làm, đổi biểu thuế) và xoá đi là mất cấu hình thật của công ty.
DROP FUNCTION IF EXISTS seed_company_defaults(UUID);
