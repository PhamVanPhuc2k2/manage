-- Dữ liệu mặc định mà MỌI công ty phải có, gom vào một hàm.
--
-- VẤN ĐỀ NÓ SỬA
--
-- Migration 000006 và 000008 đổ dữ liệu mặc định bằng `SELECT ... FROM
-- companies`. Cách đó chỉ chạy đúng nếu công ty đã tồn tại LÚC migration
-- chạy. Trên một cài đặt mới thì thứ tự ngược lại: migration chạy trên
-- database rỗng, rồi `cmd/seed` mới tạo công ty. Kết quả là công ty mới
-- không có khung giờ làm việc và không có tham số tính lương.
--
-- Hậu quả không hiện ra ngay. Hệ thống dựng lên trông bình thường, nhân viên
-- vào làm việc bình thường, và tới cuối tháng đầu tiên thì việc tính lương
-- thất bại với "Chưa cấu hình tham số tính lương cho công ty" — một thông
-- báo không nói gì về nguyên nhân thật.
--
-- Đổi thẳng hai migration cũ không sửa được gì: chúng đã chạy rồi và sẽ
-- không chạy lại.
--
-- CÁCH SỬA
--
-- Một hàm, hai chỗ gọi: migration này gọi cho mọi công ty đang có (vá các
-- cài đặt đã lỡ), và `cmd/seed` gọi ngay sau khi tạo công ty (đúng cho mọi
-- cài đặt về sau). Giá trị mặc định vì vậy chỉ có MỘT nguồn.

CREATE OR REPLACE FUNCTION seed_company_defaults(p_company_id UUID)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    v_settings_id UUID;
BEGIN
    -- --- Khung giờ làm việc mức công ty ---
    --
    -- Đây là mốc cuối cùng để tính đi muộn và thiếu giờ. Không có nó thì mọi
    -- phép tính im lặng trả về 0 và bảng công trông vẫn bình thường trong
    -- khi đã mất hết ý nghĩa.
    IF NOT EXISTS (
        SELECT 1 FROM work_schedules
        WHERE company_id = p_company_id
          AND department_id IS NULL AND employee_id IS NULL
    ) THEN
        INSERT INTO work_schedules (company_id, name, work_start, work_end)
        VALUES (p_company_id, 'Khung giờ mặc định', '08:00', '17:30');
    END IF;

    -- --- Tham số tính lương ---
    SELECT id INTO v_settings_id
    FROM payroll_settings
    WHERE company_id = p_company_id
    ORDER BY effective_from
    LIMIT 1;

    IF v_settings_id IS NULL THEN
        INSERT INTO payroll_settings (company_id, effective_from, social_cap, unemployment_cap)
        VALUES (
            p_company_id,
            DATE '2000-01-01',
            -- Trần BHXH/BHYT: 20 lần mức lương cơ sở.
            20 * 2340000,
            -- Trần BHTN: 20 lần mức lương tối thiểu vùng I.
            20 * 4960000
        )
        RETURNING id INTO v_settings_id;
    END IF;

    -- --- Biểu thuế luỹ tiến từng phần ---
    --
    -- Thiếu biểu thuế thì KHÔNG có lỗi nào được báo: mọi người im lặng được
    -- tính thuế 0 đồng, và sai sót chỉ lộ ra khi cơ quan thuế đối chiếu.
    IF NOT EXISTS (SELECT 1 FROM tax_brackets WHERE settings_id = v_settings_id) THEN
        INSERT INTO tax_brackets (settings_id, ordinal, from_amount, to_amount, rate)
        SELECT v_settings_id, b.ordinal, b.from_amount, b.to_amount, b.rate
        FROM (VALUES
            (1,         0::BIGINT,   5000000::BIGINT, 0.05),
            (2,   5000000::BIGINT,  10000000::BIGINT, 0.10),
            (3,  10000000::BIGINT,  18000000::BIGINT, 0.15),
            (4,  18000000::BIGINT,  32000000::BIGINT, 0.20),
            (5,  32000000::BIGINT,  52000000::BIGINT, 0.25),
            (6,  52000000::BIGINT,  80000000::BIGINT, 0.30),
            (7,  80000000::BIGINT,            NULL,   0.35)
        ) AS b(ordinal, from_amount, to_amount, rate);
    END IF;
END;
$$;

COMMENT ON FUNCTION seed_company_defaults(UUID) IS
'Tạo khung giờ làm việc, tham số lương và biểu thuế mặc định cho một công ty. '
'Chạy được nhiều lần: mỗi phần chỉ tạo khi chưa có. '
'Gọi từ cmd/seed ngay sau khi tạo công ty.';

-- Vá những công ty đã tồn tại mà còn thiếu.
DO $$
DECLARE
    c RECORD;
BEGIN
    FOR c IN SELECT id FROM companies LOOP
        PERFORM seed_company_defaults(c.id);
    END LOOP;
END;
$$;
