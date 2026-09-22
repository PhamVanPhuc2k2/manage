# Sơ đồ cơ sở dữ liệu

37 bảng, chia theo module. Nguồn sự thật là `backend/migrations/` — tài liệu
này là bản đồ để đọc chúng, không thay thế chúng.

Sinh lại sơ đồ từ database đang chạy:

```bash
docker compose exec postgres psql -U manage -d manage -c "\dt"
docker compose exec postgres pg_dump -U manage -d manage --schema-only \
  > /tmp/schema.sql
```

## Quy ước dùng chung

Áp dụng cho mọi bảng, nên không nhắc lại ở từng phần:

| Quy ước | Cách làm | Lý do |
|---|---|---|
| Khoá chính | `UUID DEFAULT uuid_generate_v4()` | Không lộ số lượng bản ghi, và gộp dữ liệu từ nhiều nguồn không đụng khoá |
| Xoá | `deleted_at TIMESTAMPTZ` (xoá mềm) | Dữ liệu nhân sự và lương phải giữ dấu vết; bảng công tham chiếu nhân viên đã nghỉ |
| Thời gian | `TIMESTAMPTZ`, không bao giờ `TIMESTAMP` | Không có múi giờ thì ca đêm rơi sai ngày |
| Tiền | `BIGINT` (đơn vị đồng) | `NUMERIC` chậm hơn và không cần phần thập phân; `FLOAT` thì sai số tích luỹ |
| `updated_at` | Trigger `set_updated_at()` | Để Go tự cập nhật thì chỉ cần một đường ghi quên là cột đó vô nghĩa |
| Chỉ mục duy nhất | `WHERE deleted_at IS NULL` (một phần) | Cho phép dùng lại mã của bản ghi đã xoá |
| Tìm kiếm tiếng Việt | `f_unaccent()` + `pg_trgm` / `tsvector` | Gõ không dấu vẫn tìm ra |

## Tổng quan quan hệ

```
                          companies
                              │
        ┌──────────┬──────────┼──────────┬───────────────┐
        ▼          ▼          ▼          ▼               ▼
  departments  positions  employees  projects    payroll_settings
        │          │          │          │               │
        │          │          │          │          tax_brackets
        └────┬─────┘          │          │
             ▼                │          │
         (employees)          │          │
                              │          │
   ┌──────────────┬───────────┼──────────┴──────┬─────────────┐
   ▼              ▼           ▼                 ▼             ▼
 users    attendance_*   leave_*        project_members    tasks
   │                                                          │
user_roles                                   ┌────────┬───────┴────┬──────────┐
   │                                         ▼        ▼            ▼          ▼
 roles ── role_permissions ── permissions  comments attachments activities timelogs
                              
   employees ──┬── salary_structures ── salary_components
               ├── payslips ── payslip_items
               ├── notifications · notification_mutes
               └── conversation_members
                            │
                      conversations ── messages ── message_attachments
```

## Phase 1 — Tổ chức và phân quyền

| Bảng | Vai trò |
|---|---|
| `companies` | Một dòng. Giữ `timezone` mà **toàn bộ** phép tính ngày công dựa vào |
| `departments` | Cây, tự tham chiếu qua `parent_id` |
| `positions` | Chức vụ |
| `employees` | Hồ sơ nhân sự. `manager_id` tự tham chiếu |
| `users` | Tài khoản đăng nhập. `employee_id` là **RESTRICT** |
| `roles` · `permissions` · `role_permissions` · `user_roles` | RBAC |

### Tách `employees` khỏi `users` — quyết định nền tảng

Đây là quyết định định hình cả hệ thống, nên nó ở đây thay vì trong một bình
luận SQL:

- Một nhân viên **có thể không có tài khoản** (công nhân, người mới chưa cấp
  máy). Gộp hai bảng buộc phải tạo tài khoản giả cho họ.
- Nghỉ việc thì **vô hiệu hoá tài khoản** nhưng **giữ hồ sơ**: bảng công,
  phiếu lương và tin nhắn cũ đều tham chiếu tới nhân viên đó.
- Mọi module nghiệp vụ làm việc với `employee_id`, không phải `user_id`. Kể cả
  người nhận bản tin WebSocket cũng là `employee_id`.

`users.employee_id` là `ON DELETE RESTRICT`: không xoá cứng được một nhân viên
còn tài khoản. Đó là hàng rào cuối cùng chống mất dấu vết.

### Phạm vi dữ liệu

`roles.scope` có ba giá trị: `all`, `department`, `self`. Nó **không** phải
một quyền — quyền trả lời "được làm loại việc này không", phạm vi trả lời
"được đụng vào bản ghi NÀO". Middleware chỉ kiểm tra quyền; phạm vi do tầng
usecase kiểm tra vì chỉ nó thấy bản ghi cụ thể.

## Phase 2 — Dự án và công việc

| Bảng | Ghi chú |
|---|---|
| `projects` | `owner_id` là **RESTRICT** — không xoá được người còn phụ trách dự án |
| `project_members` | `(project_id, employee_id)` là khoá chính. Vai trò trong dự án **khác** vai trò hệ thống |
| `tasks` | `parent_id` cho công việc con. `seq` sinh theo dự án, ghép với mã dự án thành `WSRT-1` |
| `task_comments` | `@mention` lưu trong nội dung, phân tích ở tầng nghiệp vụ |
| `task_attachments` | `storage_key` trên R2, **không** phải URL |
| `task_activities` | Nhật ký thay đổi. `actor_id` là SET NULL |
| `task_timelogs` | Thời gian ghi nhận |

`tasks.status` là enum `task_status`. **Mọi** tham số truyền vào so sánh với
cột này phải ép kiểu tường minh `$n::task_status` — thiếu ép kiểu thì
PostgreSQL suy ra `text` và câu lệnh lỗi lúc chạy. Đây là một lỗi thật đã gặp:
toàn bộ kéo-thả trên bảng Kanban trả 500.

`tasks.position` là số thực, không phải số nguyên. Chèn giữa hai công việc chỉ
cần lấy trung bình hai giá trị, không phải cập nhật lại mọi dòng phía sau.

## Phase 3 — Chấm công và nghỉ phép

| Bảng | Ghi chú |
|---|---|
| `work_schedules` | Khung giờ ở **ba mức**: công ty, phòng ban, cá nhân |
| `holidays` | Ngày lễ theo công ty |
| `attendance_sessions` | Phiên làm việc, dựng từ presence. `CHECK (ended_at >= started_at)` |
| `attendance_days` | Tổng hợp một ngày một dòng cho **mọi** nhân viên |
| `attendance_adjustments` | Yêu cầu điều chỉnh công |
| `leave_requests` | Đơn nghỉ phép |
| `leave_balances` | Quỹ ngày phép theo năm |

### Chuỗi dữ liệu chấm công

```
heartbeat WebSocket (30 giây)
        ▼
Redis  presence:{employee_id}          ← TTL 90 giây, KHÔNG bền
        ▼
job quét mỗi phút
        ▼
attendance_sessions                    ← gộp phiên nếu khoảng hở < 5 phút
        ▼
job tổng hợp (00:30 và 12:05)
        ▼
attendance_days                        ← nguồn cho tính lương
```

Presence trong Redis **không bền và không dựng lại được**. Job quét ngừng 10
phút là mất 10 phút làm việc của mọi người — xem
[RUNBOOK.md](RUNBOOK.md#job-quet-presence-ngung).

`attendance_days` có một dòng cho **mọi** nhân viên, kể cả người vắng. "Không
có dòng" rất khác "có dòng ghi vắng mặt" khi tính lương.

`work_schedules` ở ba mức, và luật ưu tiên (cá nhân > phòng ban > công ty,
cùng mức thì lấy bản hiệu lực muộn nhất) nằm ở **tầng nghiệp vụ**, không trong
SQL. Nó là luật nghiệp vụ: sẽ được hỏi tới, tranh luận và sửa đổi, nên nó cần
ở chỗ đọc được.

## Phase 4 — Lương

| Bảng | Ghi chú |
|---|---|
| `payroll_settings` | Tham số theo **phiên bản**: `UNIQUE (company_id, effective_from)` |
| `tax_brackets` | Bậc thuế luỹ tiến, gắn với một phiên bản tham số |
| `salary_structures` | Cấu hình lương theo **phiên bản** cho mỗi nhân viên |
| `salary_components` | Phụ cấp và khấu trừ |
| `payroll_periods` | Kỳ lương. Có trạng thái, khoá được |
| `payslips` | Một phiếu cho một người trong một kỳ. `employee_id` là **RESTRICT** |
| `payslip_items` | Chi tiết từng dòng của phiếu |
| `audit_logs` | Ghi **cả lượt XEM**, không chỉ lượt sửa |

### Vì sao cấu hình lương có phiên bản

Tính lại lương tháng 3 phải cho ra **đúng** con số của tháng 3, dù lương đã
tăng từ tháng 5. Ghi đè tại chỗ sẽ khiến mọi lần tính lại đều sai theo mức
lương hiện tại.

Đây từng là hai lỗi thật:

1. `payroll_settings` seed với `effective_from = CURRENT_DATE` → không tính
   được lương cho bất kỳ tháng nào trước ngày cài đặt. Sửa thành
   `DATE '2000-01-01'`.
2. `UpdateSettings` ghi đè tại chỗ, phá vỡ chính lời hứa của `Current(at)`.
   Sửa: đổi tên cổng thành `SaveVersion`, `ON CONFLICT (company_id,
   effective_from)`, và tầng nghiệp vụ ghi một phiên bản mới hiệu lực từ hôm
   nay rồi sao chép bậc thuế sang.

### Vì sao audit_logs ghi cả lượt xem

Rò rỉ bảng lương thường là do **đọc**, không phải do sửa. Chỉ ghi lượt sửa thì
không trả lời được câu "ai đã xem phiếu lương của giám đốc".

## Phase 5 — Thông báo và chat

| Bảng | Ghi chú |
|---|---|
| `notifications` | Ba chỉ mục, trong đó một chỉ mục **một phần** trên dòng chưa đọc |
| `notification_mutes` | **Chỉ** lưu loại đã TẮT |
| `conversations` | `direct_key` duy nhất để hội thoại 1-1 không bị tạo đôi |
| `conversation_members` | `last_read_message_id`, `is_pinned`, `is_muted`, `left_at` |
| `messages` | `search_vector` do trigger duy trì. `client_message_id` chống trùng |
| `message_attachments` | Có `width`/`height` để khung chat chừa sẵn chỗ cho ảnh |

### `direct_key` — chống hội thoại 1-1 bị tạo đôi

```
direct_key = hai employee_id đã SẮP XẾP rồi ghép bằng dấu hai chấm
```

Sắp xếp là phần cốt lõi: nhờ nó `(A,B)` và `(B,A)` cho **cùng** một khoá.
Thiếu bước đó thì hai người cùng bấm "nhắn tin" một lúc tạo ra hai hội thoại
song song, mỗi người thấy một nửa lịch sử, và không ai hiểu tin nhắn kia đi
đâu.

```sql
CREATE UNIQUE INDEX idx_conversations_direct
  ON conversations (direct_key)
  WHERE direct_key IS NOT NULL AND deleted_at IS NULL;
```

### `notification_mutes` chỉ lưu loại đã tắt

Mặc định là bật tất cả, nên bảng này **rỗng** với phần lớn người dùng — rẻ hơn
nhiều so với tạo sẵn một dòng cho mỗi người nhân mỗi loại (11 loại × N người).

Bật lại một loại là **xoá** dòng, không phải đặt cờ `false`.

### Chỉ mục một phần cho số chưa đọc

```sql
CREATE INDEX idx_notifications_unread
  ON notifications (employee_id) WHERE read_at IS NULL;
```

Chuông hỏi con số này ở **mọi** trang. Chỉ mục một phần nhỏ hơn nhiều lần chỉ
mục đầy đủ, vì thông báo đã đọc chiếm phần lớn bảng sau vài tháng.

### `search_vector` dùng trigger, không phải cột sinh sẵn

```sql
NEW.search_vector := to_tsvector('simple', f_unaccent(COALESCE(NEW.content, '')));
```

Cột `GENERATED` **không** dùng được vì `f_unaccent` là hàm `STABLE` chứ không
`IMMUTABLE`. Trigger là cách còn lại, và nó cũng cho phép đổi cách lập chỉ mục
sau này mà không phải viết lại cả bảng.

### `last_read_message_id` — một mốc, không phải một bảng

Đếm chưa đọc = số tin sau mốc này. Lưu **một** mốc mỗi thành viên thay vì một
dòng cho mỗi tin đã đọc: chat sinh ra rất nhiều tin, và bảng "ai đã đọc tin
nào" sẽ lớn gấp bội bảng tin nhắn.

So sánh theo `last_read_at` (thời điểm) chứ không theo id, vì
`uuid_generate_v4()` sinh id ngẫu nhiên nên **không sắp thứ tự được**.

## Hai trigger đáng biết

```sql
-- Nâng last_message_at mỗi khi có tin mới.
-- Làm bằng trigger chứ không để Go tự cập nhật: mọi đường ghi tin nhắn đều
-- phải nâng mốc này, và trigger không bao giờ quên.
CREATE TRIGGER trg_messages_touch_conversation
  AFTER INSERT ON messages
  FOR EACH ROW EXECUTE FUNCTION messages_touch_conversation();

-- Cập nhật vector tìm kiếm khi nội dung đổi.
CREATE TRIGGER trg_messages_search
  BEFORE INSERT OR UPDATE OF content ON messages
  FOR EACH ROW EXECUTE FUNCTION messages_update_search();
```

## Quy tắc khi thêm migration

1. **Chỉ thêm, không xoá.** Không xoá cột, không đổi tên cột, không thu hẹp
   kiểu. Đây là điều kiện để `scripts/rollback.sh` an toàn: code phiên bản cũ
   vẫn chạy được trên schema mới.
2. Mọi migration phải có `.down.sql` chạy được thật.
3. Chỉ mục duy nhất trên dữ liệu xoá mềm phải có `WHERE deleted_at IS NULL`.
4. Ràng buộc nghiệp vụ đặt ở **cả hai** nơi: `CHECK` trong database và kiểm
   tra ở tầng nghiệp vụ. Database là hàng rào không ai đi vòng được; tầng
   nghiệp vụ cho thông báo lỗi người dùng đọc được.
5. Cột enum: mọi tham số so sánh phải ép kiểu tường minh `$n::tên_enum`.
