# PHASE 1 — Xác thực, phân quyền, nhân sự

> Tài liệu hướng dẫn chi tiết. Đọc kèm [TASKS.md](./TASKS.md) và [PHASE-0-SETUP.md](./PHASE-0-SETUP.md).
> Cập nhật: 2026-09-15 — **chưa triển khai, đang ở giai đoạn thiết kế.**

---

## 1. Mục tiêu

Phase 0 dựng bộ khung. Phase 1 đưa **người dùng thật** vào hệ thống: đăng nhập được, phân quyền hoạt động, quản lý đầy đủ nhân viên và phòng ban.

Đây là phase **nhạy cảm nhất về bảo mật** trong cả dự án. Mọi phase sau đều dựa vào phân quyền viết ở đây: nếu kiểm tra quyền sai, thì tới Phase 4 một nhân viên thường sẽ xem được bảng lương của cả công ty. Vì vậy tài liệu này dành phần lớn dung lượng cho **lý do** chọn từng cách làm, chứ không chỉ hướng dẫn gõ code.

### Tiêu chí nghiệm thu

- [ ] Đăng nhập bằng tài khoản admin được seed sẵn, nhận access token + refresh token
- [ ] Access token hết hạn sau 15 phút, frontend tự gọi refresh mà người dùng không thấy gì
- [ ] Đăng xuất một thiết bị không ảnh hưởng thiết bị khác; "đăng xuất tất cả" cắt sạch mọi phiên **ngay lập tức**
- [ ] Dùng lại một refresh token đã tiêu → hệ thống coi là bị đánh cắp, huỷ toàn bộ phiên của tài khoản đó
- [ ] Nhân viên thường gọi `GET /api/v1/employees` chỉ thấy chính mình; trưởng phòng thấy cả phòng; giám đốc thấy toàn công ty
- [ ] Sai mật khẩu 5 lần liên tiếp → tài khoản bị khoá tạm, thông báo rõ thời gian mở lại
- [ ] Tìm "nguyen van a" ra được "Nguyễn Văn A" (không dấu)
- [ ] Tạo phòng ban con của chính nó → bị từ chối, không làm hỏng cây
- [ ] Vô hiệu hoá nhân viên → không đăng nhập được nữa, nhưng dữ liệu lịch sử vẫn còn
- [ ] Quên mật khẩu → nhận mail trong MailHog, đặt lại được, link chỉ dùng được một lần
- [ ] `golangci-lint` 0 issues

---

## 2. Bước 0 — Đổi module path

Việc này nên làm **trước tiên**, trước khi Phase 1 sinh thêm hàng chục file. Càng để lâu càng nhiều chỗ phải sửa.

Hiện `go.mod` khai `github.com/yourorg/manage`, còn repo thật là `github.com/PhamVanPhuc2k2/manage`.

```bash
cd backend
go mod edit -module github.com/PhamVanPhuc2k2/manage
grep -rl "github.com/yourorg/manage" --include="*.go" . \
  | xargs sed -i 's|github.com/yourorg/manage|github.com/PhamVanPhuc2k2/manage|g'
go mod tidy
go build ./...
```

Sửa nốt tên image trong `Makefile`, `dev.ps1` và `docker-compose.prod.yml` (`ghcr.io/yourorg/...` → `ghcr.io/PhamVanPhuc2k2/...`).

---

## 3. Quyết định thiết kế

Phần này là phần quan trọng nhất của tài liệu. Đọc kỹ trước khi code.

### 3.1 Chiến lược token

| Loại | Dạng | Hạn | Lưu ở client | Lưu ở server |
|---|---|---|---|---|
| Access token | JWT có chữ ký | 15 phút | **trong bộ nhớ JS** | không lưu |
| Refresh token | chuỗi ngẫu nhiên 32 byte | 7 ngày | **cookie httpOnly** | hash trong Redis |

**Vì sao access token là JWT còn refresh token thì không?**

Access token đi kèm mọi request. Nếu nó là chuỗi ngẫu nhiên thì mỗi request phải tra database hoặc Redis để biết nó là của ai — tốn kém. JWT tự mang thông tin nên chỉ cần xác minh chữ ký.

Refresh token thì ngược lại: mỗi 15 phút mới dùng một lần, và **bắt buộc phải thu hồi được**. Đã phải tra Redis thì JWT không còn lợi thế gì, mà lại để lộ thông tin bên trong. Chuỗi ngẫu nhiên đơn giản và kín hơn.

**Vì sao access token để trong bộ nhớ, không để `localStorage`?**

`localStorage` đọc được bằng JavaScript. Chỉ cần một lỗ hổng XSS ở bất kỳ đâu trong ứng dụng là kẻ tấn công lấy được token. Để trong biến JavaScript thì mất khi tải lại trang — nhưng đó chính là lúc refresh token trong cookie `httpOnly` phát huy tác dụng: trang vừa tải xong gọi `/auth/refresh`, lấy access token mới, người dùng không thấy gì cả.

Cookie `httpOnly` thì JavaScript không đọc được, kể cả khi có XSS.

### 3.2 Thu hồi tức thì — vấn đề cố hữu của JWT

JWT không thu hồi được: đã ký rồi thì nó hợp lệ tới khi hết hạn. Nghĩa là bấm "đăng xuất tất cả" xong, kẻ trộm vẫn dùng được access token thêm 15 phút nữa. Với dữ liệu lương thì 15 phút là quá dài.

**Cách xử lý: nhúng `session_id` vào access token.**

```
Access token chứa: { user_id, session_id, roles, permissions, scope, exp }

Middleware mỗi request:
  1. Xác minh chữ ký JWT          (không chạm mạng)
  2. GET session:{session_id}     (một lệnh Redis, dưới 1ms)
  3. Không tồn tại → 401
```

Đăng xuất chỉ cần xoá key `session:{session_id}`. Đăng xuất tất cả thì xoá mọi session của user. Cả access token lẫn refresh token chết ngay lập tức.

Cái giá: một lệnh Redis mỗi request. Với hệ thống nội bộ vài trăm người, đây là cái giá không đáng kể — đổi lấy khả năng thu hồi tức thì thì quá rẻ. Đừng nghe lời khuyên "JWT phải stateless" một cách máy móc: stateless chỉ đáng giá khi bạn có hàng triệu request mỗi giây, mà bạn thì không.

### 3.3 Xoay vòng refresh token và phát hiện đánh cắp

Mỗi lần gọi `/auth/refresh`:

```
1. Nhận refresh token cũ từ cookie
2. Băm nó, tra Redis
3. Không có → 401
4. Có, nhưng đã ĐÁNH DẤU LÀ ĐÃ DÙNG → BỊ ĐÁNH CẮP, xem bước 7
5. Đánh dấu token cũ là đã dùng (giữ lại 7 ngày để phát hiện tái sử dụng)
6. Phát hành cặp token mới, trả về
7. Nếu phát hiện tái sử dụng: HUỶ TOÀN BỘ phiên của user này, ghi audit log,
   gửi mail cảnh báo
```

**Vì sao bước 4 và 7 quan trọng?** Refresh token dùng một lần rồi bỏ. Nếu một token đã tiêu lại xuất hiện lần nữa, chỉ có thể là ai đó đã sao chép nó. Lúc này chưa biết token đang dùng là của người thật hay kẻ trộm — nên cắt cả hai và bắt đăng nhập lại. Bất tiện một lần, nhưng chặn được kẻ trộm.

### Thời gian ân hạn — bắt buộc phải có

Bản xoay vòng "thuần" ở trên có một lỗ hổng về trải nghiệm, và nó xảy ra thật:

- **Hai tab cùng F5.** Cả hai gọi `/auth/refresh` với cùng một cookie. Một tab thắng, tab kia bị coi là đánh cắp → hệ thống huỷ sạch phiên → **cả hai tab đều bị đăng xuất**, kể cả tab vừa lấy được token mới. Cơ chế gộp request trong `api-client` chỉ hoạt động trong phạm vi một tab, không chặn được nhiều tab.
- **Phản hồi rơi mạng.** Client gửi refresh, server xoay vòng và trả lời, nhưng phản hồi mất giữa đường. Client gửi lại token cũ và bị coi là kẻ trộm.

Cách xử lý: cho một **khoảng ân hạn ngắn** sau lần dùng đầu tiên. Token dùng lại trong khoảng đó vẫn được chấp nhận; dùng lại sau đó mới bị coi là đánh cắp.

```
Chủ nhân dùng token lúc T
    ├─ dùng lại trong khoảng [T, T+10s]  → chấp nhận, bám vào phiên vừa tạo
    └─ dùng lại sau T+10s                → ĐÁNH CẮP, huỷ toàn bộ phiên
```

Đánh đổi: khoảng này càng dài, kẻ trộm càng có nhiều thời gian dùng token đã lộ. 10 giây đủ cho hai tab và một lần gửi lại do mạng chập, nhưng quá ngắn để khai thác trong thực tế. Các nhà cung cấp lớn cũng làm vậy — Auth0 gọi là *rotation leeway*.

**Lần dùng lại phải bám vào phiên vừa tạo, không được cấp phiên mới.** Bản ân hạn đầu tiên xử lý bằng cách cấp hẳn một phiên mới cho tab thứ hai. Người dùng không bị đá ra, nhưng kéo theo ba chuyện:

1. Mỗi chu kỳ refresh với N tab đẻ ra N phiên mà chỉ xoá 1. Ba tab mở cả ngày là vài trăm phiên rác trong Redis, sống tới hết 7 ngày.
2. Trang "thiết bị đang đăng nhập" đầy dòng trùng nhau, tên thiết bị rỗng. Bấm đăng xuất một dòng chỉ cắt được một tab.
3. Nghiêm trọng nhất: **đăng xuất bị vô hiệu**. Đăng xuất xong, tab khác gửi lại token cũ trong vòng 10 giây là có ngay một phiên mới hợp lệ — phiên vừa cắt sống lại dưới id khác.

Cách sửa nằm ở chỗ *khi nào* ghi id phiên mới. Ghi bổ sung sau khi tạo phiên là vô dụng: hai tab F5 gần như cùng một thời điểm, lần ghi đó luôn đến sau lần đọc của tab kia. Nên id được **đặt chỗ ngay trong lệnh Lua tiêu token**:

```lua
redis.call('HSET', KEYS[1], 'used', '1', 'used_at', ARGV[1], 'next_session', ARGV[3])
```

Tab thứ hai đọc `next_session` ra ngay, rồi chờ tối đa 300 ms cho bản ghi phiên hiện ra — vì id có trước, bản ghi có sau vài mili giây. Hết 300 ms mà vẫn không có nghĩa là phiên đã bị cắt thật: từ chối. **Đăng xuất phải thắng ân hạn.**

Cả bốn chiều đều có trong `scripts/smoke-auth.sh`: hai tab cùng F5 phải cùng sống, phải vẫn chỉ một phiên, đăng xuất rồi dùng lại trong ân hạn phải 401, và dùng lại sau ân hạn phải huỷ sạch phiên.

Đây là cơ chế duy nhất giúp phát hiện token bị đánh cắp mà không cần thiết bị theo dõi gì thêm. Bỏ nó đi thì refresh token bị lộ có thể dùng vô thời hạn mà không ai biết.

### Đăng nhập hai bước: mã xác minh qua email

Mật khẩu đúng **không** còn đồng nghĩa với đăng nhập xong. Sau bước mật khẩu, hệ thống gửi mã 6 chữ số tới email của nhân viên; token chỉ ra đời khi nhập đúng mã.

```
POST /auth/login       email + mật khẩu
   → 200 { otp_required: true, challenge_id, masked_email, expires_at }
     KHÔNG token, KHÔNG cookie, KHÔNG phiên nào được tạo

POST /auth/verify-otp  challenge_id + mã
   → 200 { access_token, ... } + cookie refresh   ← phiên ra đời Ở ĐÂY

POST /auth/resend-otp  challenge_id
   → 200, hoặc 429 nếu bấm quá sớm / quá nhiều lần
```

Điểm quan trọng nhất của thiết kế này nằm ở chỗ bước một **không để lại gì có thể dùng được**. Đặt cookie refresh ở bước một, hay tạo sẵn phiên rồi đánh dấu "chưa xác minh", là tự tay dựng lại đúng lỗ hổng mà OTP sinh ra để bịt: kẻ có mật khẩu đã cầm được nửa phiên.

Các tham số và lý do chọn (`internal/domain/auth/entity.go`):

| Tham số | Giá trị | Vì sao |
|---|---|---|
| Số chữ số | 6 | Một triệu khả năng. Tăng lên 8 chỉ làm người dùng gõ sai nhiều hơn, trong khi tấm chắn thật là giới hạn số lần thử |
| Hạn mã | 5 phút | Đủ để mở hộp thư, ngắn để mã lọt ra ngoài cũng vô dụng |
| Số lần nhập sai | 5 | **Điều kiện tiên quyết.** OTP 6 chữ số không giới hạn lần thử thì vét cạn xong trong vài phút |
| Gửi lại | cách nhau 60 giây, tối đa 3 lần | Không chặn thì kẻ tấn công cứ bấm gửi lại là bộ đếm 5 lần không bao giờ chạm trần |

Ba chi tiết dễ làm sai:

**Mã lưu dưới dạng băm, id thử thách cũng vậy.** Giống hệt refresh token: ai đọc được Redis cũng không đi tiếp được. Mã không bao giờ xuất hiện trong log — log thường được gom về một nơi mà nhiều người đọc được.

**So mã và đếm số lần sai phải nằm trong một lệnh Lua.** Đọc rồi so ở Go thì nhiều request song song cùng đọc thấy `attempts = 0`, và giới hạn 5 lần trở thành trang trí.

**Đọc lại tài khoản từ database ở bước hai, không tin bản chụp của bước một.** Giữa hai bước có thể tới 5 phút; trong 5 phút đó nhân viên có thể đã bị cho nghỉ.

Bước hai cũng phải nằm trong vùng giới hạn tốc độ của nginx cùng với `/auth/login` — bỏ nó ra ngoài là để hở đúng chỗ cần chặn nhất, vì kẻ tấn công chỉ việc mở thử thách mới để làm mới bộ đếm.

Công tắc `AUTH_OTP_ENABLED` mặc định **bật**. Tắt được để chạy kiểm thử tự động và để cứu hoả khi SMTP chết — nhưng tắt nghĩa là mật khẩu lộ là vào được hệ thống.

Cái giá phải trả, nói thẳng: **hàng đợi tắc thì cả công ty không đăng nhập được.** Mail đăng nhập là mail duy nhất người dùng đang ngồi chờ ngay lúc đó. Đây là lý do có công tắc, và là việc cần theo dõi ở Phase 6.

Bước tiếp theo tự nhiên (chưa làm): **nhớ thiết bị tin cậy**. Chỉ hỏi mã khi đăng nhập từ thiết bị lạ, đặt một cookie ký sống 30 ngày cho thiết bị đã xác minh. Google và GitHub đều làm vậy. Nó biến OTP từ phiền hà hàng ngày thành phiền hà mỗi tháng một lần, mà vẫn giữ nguyên tác dụng.

### 3.4 Mô hình phân quyền

Ba tầng, kiểm tra theo thứ tự:

```
1. Xác thực   — anh là ai?              (middleware RequireAuth)
2. Quyền      — anh được làm gì?        (middleware RequirePermission)
3. Phạm vi    — trên dữ liệu của ai?    (usecase, không phải middleware)
```

**Tầng 3 là chỗ hay bị bỏ sót và là nguồn lỗi bảo mật nghiêm trọng nhất.** Middleware chỉ biết "người này có quyền `employee:read`", nó không biết người này định đọc nhân viên nào. Nếu dừng ở tầng 2, một trưởng phòng có quyền `employee:read` sẽ đọc được hồ sơ của toàn công ty.

Mã quyền đặt theo dạng `tài_nguyên:hành_động`:

```
employee:read      employee:create    employee:update    employee:delete
department:read    department:create  department:update  department:delete
position:read      position:manage
role:read          role:assign
payroll:read       payroll:manage      (Phase 4)
attendance:read    attendance:approve  (Phase 3)
```

Năm vai trò mặc định:

| Vai trò | Phạm vi dữ liệu | Ghi chú |
|---|---|---|
| `admin` | `all` | Quản trị kỹ thuật, có quyền đổi vai trò |
| `director` | `all` | Giám đốc, xem toàn bộ trừ quản trị kỹ thuật |
| `manager` | `department` | Trưởng phòng, chỉ phòng mình và phòng con |
| `hr` | `all` | Nhân sự, quản lý hồ sơ và lương |
| `employee` | `self` | Nhân viên thường, chỉ hồ sơ của chính mình |

Phạm vi (`data_scope`) là thuộc tính của **vai trò**, không phải của quyền. Một người có nhiều vai trò thì lấy phạm vi rộng nhất.

**Cách áp phạm vi vào truy vấn** — usecase dựng điều kiện lọc rồi đưa xuống repository:

```go
switch actor.Scope {
case ScopeAll:
    // không thêm điều kiện
case ScopeDepartment:
    filter.DepartmentIDs = actor.ManagedDepartmentIDs  // gồm cả phòng con
case ScopeSelf:
    filter.EmployeeID = &actor.EmployeeID
}
```

Quy tắc bất di bất dịch: **phạm vi áp ở tầng usecase, không áp ở frontend.** Ẩn nút trên giao diện chỉ là trang trí — ai cũng gọi được API bằng `curl`.

### 3.5 Cây phòng ban

Dùng danh sách kề (`parent_id`) chứ không dùng `ltree` hay materialized path.

Lý do: công ty có vài chục phòng ban, truy vấn đệ quy bằng `WITH RECURSIVE` trả về tức thì. `ltree` nhanh hơn khi cây có hàng vạn nút, nhưng đổi lại phải cập nhật đường dẫn của toàn bộ nhánh mỗi khi di chuyển một phòng — phức tạp không cần thiết ở quy mô này.

**Bắt buộc chặn vòng lặp khi sửa `parent_id`.** Nếu để phòng A thành con của phòng B mà B lại là con cháu của A, thì `WITH RECURSIVE` sẽ chạy vô hạn và treo cả database. Kiểm tra bằng cách đi ngược lên tổ tiên của cha mới; gặp lại chính nó thì từ chối.

### 3.6 Xoá mềm, không xoá cứng

Nhân viên **không bao giờ bị xoá khỏi database**. Lý do: phiếu lương, chấm công, task đã giao đều tham chiếu tới họ. Xoá cứng sẽ làm hỏng dữ liệu lịch sử, hoặc buộc phải xoá lan sang cả lịch sử — không chấp nhận được với dữ liệu kế toán.

Dùng `deleted_at TIMESTAMPTZ NULL`. Nhân viên bị vô hiệu hoá thì:
- không đăng nhập được,
- không hiện trong danh sách chọn người thực hiện,
- vẫn hiện trong báo cáo lịch sử và phiếu lương cũ.

Chỉ số duy nhất trên email phải tính đến điều này:

```sql
CREATE UNIQUE INDEX idx_users_email_active
  ON users (lower(email)) WHERE deleted_at IS NULL;
```

Nhờ mệnh đề `WHERE`, một email đã bị vô hiệu hoá có thể dùng lại cho người mới.

### 3.7 Tìm kiếm tiếng Việt không dấu

Yêu cầu: gõ `nguyen van a` phải ra `Nguyễn Văn A`.

`unaccent()` bỏ dấu, `pg_trgm` cho phép tìm gần đúng. Nhưng có một cạm bẫy: **`unaccent()` không phải hàm IMMUTABLE** nên PostgreSQL từ chối đánh chỉ số trực tiếp lên nó. Phải bọc lại:

```sql
CREATE OR REPLACE FUNCTION f_unaccent(text)
RETURNS text
LANGUAGE sql
IMMUTABLE PARALLEL SAFE STRICT
AS $$ SELECT public.unaccent('public.unaccent'::regdictionary, $1) $$;

CREATE INDEX idx_employees_name_trgm
  ON employees USING gin (f_unaccent(lower(full_name)) gin_trgm_ops);
```

Truy vấn phải dùng **đúng biểu thức** đã đánh chỉ số, nếu không PostgreSQL sẽ quét toàn bảng:

```sql
SELECT * FROM employees
WHERE f_unaccent(lower(full_name)) LIKE f_unaccent(lower($1)) || '%'
  AND deleted_at IS NULL;
```

> Lưu ý: bọc `unaccent` thành IMMUTABLE là một lời hứa với PostgreSQL. Nếu sau này ai đó sửa từ điển `unaccent`, chỉ số sẽ sai và phải `REINDEX`. Thực tế không ai sửa, nên đánh đổi này an toàn — nhưng phải biết mình đang đánh đổi cái gì.

### 3.8 Những gì KHÔNG làm ở Phase 1

Cố ý để lại, kèm lý do:

| Việc | Vì sao hoãn |
|---|---|
| ~~Xác thực hai lớp (2FA)~~ | **Đã làm** — mã OTP 6 chữ số gửi qua email ở mỗi lần đăng nhập. Xem mục "Đăng nhập hai bước" ở trên |
| 2FA bằng ứng dụng (TOTP) | OTP qua email đã chặn được trường hợp lộ mật khẩu. TOTP chỉ hơn khi chính hộp thư bị chiếm — cân nhắc cho tài khoản admin ở Phase 6 |
| Nhớ thiết bị tin cậy | Cần cookie ký riêng và bảng thiết bị. Làm cùng lúc với trang quản lý thiết bị ở Phase 6 |
| Đăng nhập bằng Google/Microsoft | Chờ xem công ty dùng Workspace hay M365 rồi mới quyết |
| Nhập nhân viên hàng loạt từ Excel | Cần hạ tầng báo tiến độ (Phase 5). Làm cuối Phase 1 nếu còn thời gian |
| Sơ đồ tổ chức dạng đồ hoạ | API cây đã có ở Phase 1; phần vẽ để sau, không chặn việc gì |
| Lịch sử thay đổi hồ sơ | Bảng `audit_logs` làm chung ở Phase 4 khi đụng dữ liệu lương |

---

## 4. Mô hình dữ liệu

### 4.1 Migration `000002_auth_and_hr.up.sql`

```sql
-- =========================================================================
-- CÔNG TY
-- =========================================================================
CREATE TABLE companies (
    id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name         VARCHAR(255) NOT NULL,
    tax_code     VARCHAR(50),
    address      TEXT,
    timezone     VARCHAR(64)  NOT NULL DEFAULT 'Asia/Ho_Chi_Minh',
    -- Khung giờ làm việc mặc định, Phase 3 dùng để tính đi muộn về sớm.
    work_start   TIME         NOT NULL DEFAULT '08:00',
    work_end     TIME         NOT NULL DEFAULT '17:30',
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- =========================================================================
-- PHÒNG BAN — cây bằng danh sách kề
-- =========================================================================
CREATE TABLE departments (
    id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    company_id   UUID NOT NULL REFERENCES companies(id) ON DELETE RESTRICT,
    parent_id    UUID REFERENCES departments(id) ON DELETE RESTRICT,
    code         VARCHAR(50)  NOT NULL,
    name         VARCHAR(255) NOT NULL,
    description  TEXT,
    -- Trưởng phòng. Đặt NULL được vì lúc tạo phòng có thể chưa có người.
    -- Khoá ngoại thêm sau khi bảng employees tồn tại (xem cuối file).
    manager_id   UUID,
    deleted_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_departments_code
  ON departments (company_id, lower(code)) WHERE deleted_at IS NULL;
CREATE INDEX idx_departments_parent ON departments (parent_id);

-- =========================================================================
-- CHỨC VỤ
-- =========================================================================
CREATE TABLE positions (
    id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    company_id   UUID NOT NULL REFERENCES companies(id) ON DELETE RESTRICT,
    code         VARCHAR(50)  NOT NULL,
    name         VARCHAR(255) NOT NULL,
    -- Dải lương tham khảo, Phase 4 dùng để cảnh báo khi đặt lương ngoài khung.
    salary_min   NUMERIC(15,2),
    salary_max   NUMERIC(15,2),
    deleted_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_positions_salary_range
        CHECK (salary_min IS NULL OR salary_max IS NULL OR salary_min <= salary_max)
);

CREATE UNIQUE INDEX idx_positions_code
  ON positions (company_id, lower(code)) WHERE deleted_at IS NULL;

-- =========================================================================
-- NHÂN VIÊN
-- =========================================================================
CREATE TYPE work_mode AS ENUM ('onsite', 'remote', 'hybrid');
CREATE TYPE employee_status AS ENUM ('probation', 'official', 'resigned');

CREATE TABLE employees (
    id             UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    company_id     UUID NOT NULL REFERENCES companies(id) ON DELETE RESTRICT,
    employee_code  VARCHAR(50)  NOT NULL,
    full_name      VARCHAR(255) NOT NULL,
    email          VARCHAR(255) NOT NULL,
    phone          VARCHAR(20),
    date_of_birth  DATE,
    gender         VARCHAR(10),
    address        TEXT,

    department_id  UUID REFERENCES departments(id) ON DELETE RESTRICT,
    position_id    UUID REFERENCES positions(id)   ON DELETE RESTRICT,
    -- Cấp trên trực tiếp. Tự tham chiếu, cũng cần chặn vòng lặp như phòng ban.
    manager_id     UUID REFERENCES employees(id)   ON DELETE SET NULL,

    work_mode      work_mode       NOT NULL DEFAULT 'onsite',
    status         employee_status NOT NULL DEFAULT 'probation',
    joined_at      DATE NOT NULL,
    resigned_at    DATE,

    avatar_key     VARCHAR(500),   -- khoá object trên Cloudflare R2, không phải URL

    deleted_at     TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_employees_resigned
        CHECK (resigned_at IS NULL OR resigned_at >= joined_at),
    -- Không cho phép tự làm cấp trên của chính mình.
    CONSTRAINT chk_employees_self_manager
        CHECK (manager_id IS NULL OR manager_id <> id)
);

CREATE UNIQUE INDEX idx_employees_code
  ON employees (company_id, lower(employee_code)) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX idx_employees_email
  ON employees (lower(email)) WHERE deleted_at IS NULL;
CREATE INDEX idx_employees_department ON employees (department_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_employees_manager    ON employees (manager_id)    WHERE deleted_at IS NULL;

-- Giờ mới gắn được khoá ngoại manager_id của departments.
ALTER TABLE departments
  ADD CONSTRAINT fk_departments_manager
  FOREIGN KEY (manager_id) REFERENCES employees(id) ON DELETE SET NULL;

-- =========================================================================
-- TÌM KIẾM TIẾNG VIỆT KHÔNG DẤU
-- =========================================================================
-- unaccent() không IMMUTABLE nên không đánh chỉ số trực tiếp được.
-- Bọc lại thành hàm IMMUTABLE — xem giải thích ở mục 3.7.
CREATE OR REPLACE FUNCTION f_unaccent(text)
RETURNS text
LANGUAGE sql
IMMUTABLE PARALLEL SAFE STRICT
AS $$ SELECT public.unaccent('public.unaccent'::regdictionary, $1) $$;

CREATE INDEX idx_employees_name_trgm
  ON employees USING gin (f_unaccent(lower(full_name)) gin_trgm_ops);
CREATE INDEX idx_employees_code_trgm
  ON employees USING gin (lower(employee_code) gin_trgm_ops);

-- =========================================================================
-- TÀI KHOẢN ĐĂNG NHẬP
-- =========================================================================
CREATE TABLE users (
    id               UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    employee_id      UUID NOT NULL UNIQUE REFERENCES employees(id) ON DELETE RESTRICT,
    email            VARCHAR(255) NOT NULL,
    password_hash    VARCHAR(255) NOT NULL,
    is_active        BOOLEAN NOT NULL DEFAULT TRUE,

    -- Bắt đổi mật khẩu ở lần đăng nhập đầu (tài khoản do HR tạo hộ).
    must_change_password BOOLEAN NOT NULL DEFAULT TRUE,

    last_login_at    TIMESTAMPTZ,
    -- Đếm số lần sai liên tiếp. Redis lo phần khoá tạm thời, cột này
    -- giữ lại để HR nhìn thấy lịch sử bất thường.
    failed_attempts  INT NOT NULL DEFAULT 0,
    locked_until     TIMESTAMPTZ,

    deleted_at       TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_users_email_active
  ON users (lower(email)) WHERE deleted_at IS NULL;

-- =========================================================================
-- RBAC
-- =========================================================================
CREATE TYPE data_scope AS ENUM ('all', 'department', 'self');

CREATE TABLE roles (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    code        VARCHAR(50)  NOT NULL UNIQUE,
    name        VARCHAR(255) NOT NULL,
    description TEXT,
    scope       data_scope   NOT NULL DEFAULT 'self',
    -- Vai trò hệ thống thì không cho sửa hay xoá qua API.
    is_system   BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE permissions (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    code        VARCHAR(100) NOT NULL UNIQUE,   -- dạng "employee:update"
    resource    VARCHAR(50)  NOT NULL,
    action      VARCHAR(50)  NOT NULL,
    description TEXT
);

CREATE TABLE role_permissions (
    role_id       UUID NOT NULL REFERENCES roles(id)       ON DELETE CASCADE,
    permission_id UUID NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,
    PRIMARY KEY (role_id, permission_id)
);

CREATE TABLE user_roles (
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id     UUID NOT NULL REFERENCES roles(id) ON DELETE RESTRICT,
    assigned_by UUID REFERENCES users(id) ON DELETE SET NULL,
    assigned_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, role_id)
);

CREATE INDEX idx_user_roles_user ON user_roles (user_id);

-- =========================================================================
-- TRIGGER CẬP NHẬT updated_at
-- =========================================================================
CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END $$;

CREATE TRIGGER trg_companies_updated   BEFORE UPDATE ON companies
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trg_departments_updated BEFORE UPDATE ON departments
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trg_positions_updated   BEFORE UPDATE ON positions
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trg_employees_updated   BEFORE UPDATE ON employees
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trg_users_updated       BEFORE UPDATE ON users
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
```

> **Vì sao dùng trigger cho `updated_at` thay vì để Go tự đặt?** Vì trigger không quên. Sau này sẽ có migration sửa dữ liệu trực tiếp bằng SQL, có script dọn dẹp, có người sửa tay trong Adminer — tất cả đều bỏ qua code Go. Trigger là chỗ duy nhất bảo đảm cột này luôn đúng.

### 4.2 Cấu trúc khoá Redis

Đặt tên nhất quán ngay từ đầu, vì Phase 3 và Phase 5 sẽ thêm nhiều khoá nữa:

| Khoá | Kiểu | TTL | Dùng để |
|---|---|---|---|
| `session:{session_id}` | Hash | 7 ngày | Phiên đang hoạt động: user_id, device, ip, ua |
| `user_sessions:{user_id}` | Set | 7 ngày | Danh sách session_id, phục vụ "đăng xuất tất cả" |
| `refresh:{token_hash}` | Hash | 7 ngày | session_id, user_id, used, used_at, next_session |
| `otp:{challenge_hash}` | Hash | 5 phút | user_id, code_hash, attempts, resends — đăng nhập bước hai |
| `pwreset:{token_hash}` | String | 30 phút | user_id, dùng một lần |
| `login_fail:ip:{ip}` | String | 15 phút | Đếm số lần sai theo IP |
| `login_fail:user:{email}` | String | 15 phút | Đếm số lần sai theo tài khoản |
| `perm_cache:{user_id}` | String | 5 phút | Quyền đã tính sẵn, tránh JOIN lặp lại |

---

## 5. Bước 1 — Seed dữ liệu ban đầu

Migration `000003_seed_rbac.up.sql` nạp quyền và vai trò. **Không** seed tài khoản admin bằng migration — mật khẩu sẽ nằm trong git.

```sql
INSERT INTO permissions (code, resource, action, description) VALUES
  ('employee:read',      'employee',   'read',    'Xem thông tin nhân viên'),
  ('employee:create',    'employee',   'create',  'Thêm nhân viên mới'),
  ('employee:update',    'employee',   'update',  'Sửa thông tin nhân viên'),
  ('employee:delete',    'employee',   'delete',  'Vô hiệu hoá nhân viên'),
  ('department:read',    'department', 'read',    'Xem phòng ban'),
  ('department:create',  'department', 'create',  'Tạo phòng ban'),
  ('department:update',  'department', 'update',  'Sửa phòng ban'),
  ('department:delete',  'department', 'delete',  'Xoá phòng ban'),
  ('position:read',      'position',   'read',    'Xem chức vụ'),
  ('position:manage',    'position',   'manage',  'Quản lý chức vụ'),
  ('role:read',          'role',       'read',    'Xem vai trò'),
  ('role:assign',        'role',       'assign',  'Gán vai trò cho người dùng')
ON CONFLICT (code) DO NOTHING;

INSERT INTO roles (code, name, scope, is_system) VALUES
  ('admin',    'Quản trị hệ thống', 'all',        TRUE),
  ('director', 'Giám đốc',          'all',        TRUE),
  ('hr',       'Nhân sự',           'all',        TRUE),
  ('manager',  'Trưởng phòng',      'department', TRUE),
  ('employee', 'Nhân viên',         'self',       TRUE)
ON CONFLICT (code) DO NOTHING;

-- admin: tất cả quyền
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'admin'
ON CONFLICT DO NOTHING;

-- hr: toàn quyền nhân sự, không đụng vai trò
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'hr' AND p.resource IN ('employee', 'department', 'position')
ON CONFLICT DO NOTHING;

-- director: xem tất cả
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'director' AND p.action = 'read'
ON CONFLICT DO NOTHING;

-- manager: xem, và sửa được nhân viên trong phạm vi phòng mình
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'manager' AND p.code IN
      ('employee:read', 'employee:update', 'department:read', 'position:read')
ON CONFLICT DO NOTHING;

-- employee: chỉ xem
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'employee' AND p.code IN
      ('employee:read', 'department:read', 'position:read')
ON CONFLICT DO NOTHING;
```

### Lệnh tạo tài khoản admin đầu tiên

Thêm binary thứ ba `cmd/seed/main.go`, chạy một lần khi cài đặt:

```
docker compose run --rm api go run ./cmd/seed \
  --company "Công ty ABC" \
  --email admin@abc.vn \
  --name "Quản trị viên"
```

Lệnh này:
1. Tạo công ty nếu chưa có
2. Tạo phòng "Ban giám đốc" và chức vụ "Giám đốc"
3. Tạo nhân viên + tài khoản, **sinh mật khẩu ngẫu nhiên và in ra màn hình một lần duy nhất**
4. Gán vai trò `admin`
5. Đặt `must_change_password = TRUE`

**Mật khẩu chỉ in ra stdout, không ghi vào file, không ghi vào log.** Nếu chạy lại lần hai mà tài khoản đã tồn tại thì báo lỗi và dừng, không âm thầm đổi mật khẩu.

---

## 6. Bước 2 — Tiện ích mật khẩu và JWT

### `pkg/hash/password.go`

```go
// Package hash băm và kiểm tra mật khẩu bằng bcrypt.
package hash

import (
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// Cost 12 là điểm cân bằng hiện nay: khoảng 250ms mỗi lần băm trên máy chủ
// thông thường — đủ chậm để chặn dò mật khẩu hàng loạt, đủ nhanh để người
// dùng không thấy đợi. Cost 10 (mặc định của thư viện) đã quá yếu.
//
// KHÔNG hạ cost để "cho nhanh". 250ms chỉ tốn đúng một lần lúc đăng nhập.
const bcryptCost = 12

var ErrMismatch = errors.New("mật khẩu không đúng")

func Password(plain string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("băm mật khẩu: %w", err)
	}
	return string(b), nil
}

// Verify so sánh mật khẩu. bcrypt tự so sánh theo kiểu chống đo thời gian.
func Verify(hashed, plain string) error {
	err := bcrypt.CompareHashAndPassword([]byte(hashed), []byte(plain))
	if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		return ErrMismatch
	}
	return err
}

// NeedsRehash cho biết hash cũ có được tạo bằng cost thấp hơn hiện tại không.
// Gọi sau khi đăng nhập thành công: nếu đúng thì băm lại bằng cost mới và
// cập nhật âm thầm. Nhờ vậy nâng cost sau này không cần bắt ai đổi mật khẩu.
func NeedsRehash(hashed string) bool {
	cost, err := bcrypt.Cost([]byte(hashed))
	return err == nil && cost < bcryptCost
}
```

### `pkg/jwt/jwt.go`

```go
// Package jwt phát hành và xác minh access token.
package jwt

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var (
	ErrExpired = errors.New("token đã hết hạn")
	ErrInvalid = errors.New("token không hợp lệ")
)

// Claims là nội dung access token.
//
// Nhét sẵn roles và permissions để middleware khỏi truy vấn database mỗi
// request. Đánh đổi: đổi quyền cho ai đó thì phải chờ tối đa 15 phút token
// cũ hết hạn — hoặc chủ động huỷ phiên của họ để bắt đăng nhập lại.
type Claims struct {
	UserID      uuid.UUID `json:"uid"`
	EmployeeID  uuid.UUID `json:"eid"`
	SessionID   uuid.UUID `json:"sid"`
	Roles       []string  `json:"roles"`
	Permissions []string  `json:"perms"`
	Scope       string    `json:"scope"`
	jwt.RegisteredClaims
}

type Manager struct {
	secret    []byte
	accessTTL time.Duration
	issuer    string
}

func NewManager(secret string, accessTTL time.Duration, issuer string) (*Manager, error) {
	// HS256 với khoá ngắn là mời gọi tấn công vét cạn.
	if len(secret) < 32 {
		return nil, fmt.Errorf("JWT_SECRET phải dài ít nhất 32 ký tự, hiện có %d", len(secret))
	}
	return &Manager{secret: []byte(secret), accessTTL: accessTTL, issuer: issuer}, nil
}

func (m *Manager) Issue(c Claims) (string, time.Time, error) {
	now := time.Now()
	expiresAt := now.Add(m.accessTTL)

	c.RegisteredClaims = jwt.RegisteredClaims{
		Issuer:    m.issuer,
		Subject:   c.UserID.String(),
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(expiresAt),
		ID:        uuid.NewString(),
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(m.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("ký token: %w", err)
	}
	return signed, expiresAt, nil
}

func (m *Manager) Verify(tokenString string) (*Claims, error) {
	var claims Claims

	_, err := jwt.ParseWithClaims(tokenString, &claims,
		func(t *jwt.Token) (any, error) {
			// BẮT BUỘC kiểm tra thuật toán.
			//
			// Thiếu bước này là lỗ hổng kinh điển của JWT: kẻ tấn công đổi
			// header sang alg=none hoặc alg=RS256 rồi tự ký bằng "khoá công
			// khai" chính là secret của ta — thư viện sẽ chấp nhận.
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("thuật toán ký không hợp lệ: %v", t.Header["alg"])
			}
			return m.secret, nil
		},
		jwt.WithIssuer(m.issuer),
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
	)

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrExpired
		}
		return nil, ErrInvalid
	}
	return &claims, nil
}
```

### `pkg/token/random.go`

```go
// Package token sinh và băm token ngẫu nhiên (refresh token, token đặt lại
// mật khẩu). Những token này KHÔNG phải JWT: chúng không mang thông tin,
// chỉ là khoá tra cứu trong Redis.
package token

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// New sinh token ngẫu nhiên 32 byte, mã hoá base64url (43 ký tự).
func New() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("sinh token ngẫu nhiên: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Hash băm token trước khi lưu.
//
// Vì sao phải băm? Nếu ai đó đọc được Redis (dump bộ nhớ, bản sao lưu,
// lệnh KEYS lọt ra ngoài), họ vẫn không mạo danh được ai.
//
// Dùng SHA-256 chứ không dùng bcrypt: token đã là 32 byte ngẫu nhiên nên
// không thể vét cạn, không cần làm chậm. bcrypt ở đây chỉ tổ tốn CPU.
func Hash(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}
```

---

## 7. Bước 3 — Tầng domain

### `internal/domain/auth/entity.go`

```go
package auth

import (
	"time"

	"github.com/google/uuid"
)

type Scope string

const (
	ScopeAll        Scope = "all"
	ScopeDepartment Scope = "department"
	ScopeSelf       Scope = "self"
)

// Rank dùng để chọn phạm vi rộng nhất khi một người có nhiều vai trò.
func (s Scope) Rank() int {
	switch s {
	case ScopeAll:
		return 3
	case ScopeDepartment:
		return 2
	default:
		return 1
	}
}

// Actor là danh tính của người đang thực hiện request.
// Middleware dựng nó từ access token và đặt vào context.
type Actor struct {
	UserID      uuid.UUID
	EmployeeID  uuid.UUID
	SessionID   uuid.UUID
	DepartmentID *uuid.UUID
	Roles       []string
	Permissions map[string]struct{}
	Scope       Scope

	// Phòng ban mà người này quản lý, GỒM CẢ phòng con.
	// Chỉ có giá trị khi Scope == ScopeDepartment.
	ManagedDepartmentIDs []uuid.UUID
}

func (a *Actor) Can(permission string) bool {
	_, ok := a.Permissions[permission]
	return ok
}

// CanSeeEmployee quyết định actor có được xem hồ sơ của một nhân viên cụ thể
// hay không. Đây là hàm phải gọi TRƯỚC MỌI thao tác trên một nhân viên —
// cả đọc lẫn ghi — nếu không sẽ dính lỗ hổng IDOR.
func (a *Actor) CanSeeEmployee(employeeID uuid.UUID, departmentID *uuid.UUID) bool {
	switch a.Scope {
	case ScopeAll:
		return true
	case ScopeSelf:
		return a.EmployeeID == employeeID
	case ScopeDepartment:
		if a.EmployeeID == employeeID {
			return true
		}
		if departmentID == nil {
			return false
		}
		for _, id := range a.ManagedDepartmentIDs {
			if id == *departmentID {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// Session là một lần đăng nhập trên một thiết bị.
type Session struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	DeviceName string
	IP         string
	UserAgent  string
	CreatedAt  time.Time
	LastSeenAt time.Time
}
```

### `internal/domain/auth/port.go`

```go
package auth

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type SessionStore interface {
	Create(ctx context.Context, s Session, ttl time.Duration) error
	Get(ctx context.Context, sessionID uuid.UUID) (*Session, error)
	Touch(ctx context.Context, sessionID uuid.UUID) error
	Delete(ctx context.Context, sessionID uuid.UUID) error
	DeleteAllOfUser(ctx context.Context, userID uuid.UUID) error
	ListOfUser(ctx context.Context, userID uuid.UUID) ([]Session, error)
}

type RefreshStore interface {
	Save(ctx context.Context, tokenHash string, sessionID uuid.UUID, ttl time.Duration) error
	// Consume đánh dấu token đã dùng và trả về session gắn với nó.
	// Nếu token đã được dùng trước đó, trả về ErrTokenReused.
	Consume(ctx context.Context, tokenHash string) (uuid.UUID, error)
	DeleteAllOfSession(ctx context.Context, sessionID uuid.UUID) error
}

type LoginThrottle interface {
	// Check trả về thời gian còn phải chờ. Bằng 0 nghĩa là được phép thử.
	Check(ctx context.Context, email, ip string) (time.Duration, error)
	RecordFailure(ctx context.Context, email, ip string) error
	Reset(ctx context.Context, email, ip string) error
}
```

---

## 8. Bước 4 — Usecase đăng nhập và làm mới token

Đây là phần nhiều bẫy nhất. Đọc kỹ từng khối.

```go
package auth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	domainuser "github.com/PhamVanPhuc2k2/manage/internal/domain/user"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
	"github.com/PhamVanPhuc2k2/manage/pkg/hash"
	"github.com/PhamVanPhuc2k2/manage/pkg/jwt"
	"github.com/PhamVanPhuc2k2/manage/pkg/logger"
	"github.com/PhamVanPhuc2k2/manage/pkg/token"
)

type LoginInput struct {
	Email      string
	Password   string
	IP         string
	UserAgent  string
	DeviceName string
}

type TokenPair struct {
	AccessToken      string
	AccessExpiresAt  time.Time
	RefreshToken     string
	RefreshExpiresAt time.Time
	MustChangePassword bool
}

func (u *Usecase) Login(ctx context.Context, in LoginInput) (*TokenPair, error) {
	log := logger.FromContext(ctx)

	// --- 1. Chặn dò mật khẩu TRƯỚC KHI chạm database ---
	wait, err := u.throttle.Check(ctx, in.Email, in.IP)
	if err != nil {
		return nil, apperror.Internal(err)
	}
	if wait > 0 {
		return nil, apperror.New(apperror.KindRateLimited,
			"Đăng nhập sai quá nhiều lần. Vui lòng thử lại sau "+
				formatDuration(wait))
	}

	// --- 2. Tra tài khoản ---
	user, err := u.users.FindByEmail(ctx, in.Email)

	// QUAN TRỌNG: email không tồn tại và mật khẩu sai phải trả về CÙNG MỘT
	// thông báo. Phân biệt hai trường hợp sẽ cho phép kẻ tấn công dò xem
	// email nào có trong hệ thống.
	if err != nil || user == nil {
		// Vẫn băm một mật khẩu giả để thời gian phản hồi không khác biệt.
		// Không có bước này, thời gian trả lời nhanh bất thường sẽ tiết lộ
		// rằng email không tồn tại.
		_, _ = hash.Password(in.Password)
		_ = u.throttle.RecordFailure(ctx, in.Email, in.IP)
		return nil, errInvalidCredentials()
	}

	// --- 3. Kiểm tra mật khẩu ---
	if err := hash.Verify(user.PasswordHash, in.Password); err != nil {
		_ = u.throttle.RecordFailure(ctx, in.Email, in.IP)
		_ = u.users.IncrementFailedAttempts(ctx, user.ID)

		log.Warn().
			Str("email", in.Email).
			Str("ip", in.IP).
			Msg("đăng nhập thất bại")

		return nil, errInvalidCredentials()
	}

	// --- 4. Tài khoản có còn dùng được không ---
	//
	// Kiểm tra SAU khi xác minh mật khẩu, không phải trước. Kiểm tra trước
	// sẽ để lộ trạng thái tài khoản cho người không biết mật khẩu.
	if !user.IsActive || user.DeletedAt != nil {
		return nil, apperror.New(apperror.KindForbidden,
			"Tài khoản đã bị vô hiệu hoá. Liên hệ bộ phận nhân sự.")
	}
	if user.Employee.DeletedAt != nil || user.Employee.Status == domainuser.StatusResigned {
		return nil, apperror.New(apperror.KindForbidden,
			"Nhân viên đã nghỉ việc.")
	}

	// --- 5. Thành công: xoá bộ đếm sai ---
	_ = u.throttle.Reset(ctx, in.Email, in.IP)
	_ = u.users.ResetFailedAttempts(ctx, user.ID)

	// Nâng cost bcrypt âm thầm nếu hash cũ yếu hơn hiện tại.
	if hash.NeedsRehash(user.PasswordHash) {
		if newHash, err := hash.Password(in.Password); err == nil {
			_ = u.users.UpdatePasswordHash(ctx, user.ID, newHash)
		}
	}

	return u.issueSession(ctx, user, in)
}

// issueSession tạo phiên mới và phát hành cặp token.
// Dùng chung cho Login và cho Refresh.
func (u *Usecase) issueSession(
	ctx context.Context,
	user *domainuser.User,
	in LoginInput,
) (*TokenPair, error) {
	sessionID := uuid.New()

	perms, scope, managedDepts, err := u.loadAuthorization(ctx, user.ID, user.EmployeeID)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	// --- Lưu phiên vào Redis TRƯỚC khi phát hành token ---
	//
	// Thứ tự này quan trọng. Nếu phát hành token trước rồi mới lưu phiên,
	// và bước lưu lỗi, thì đã có một token hợp lệ ngoài kia mà server
	// không biết gì về nó.
	session := domainauth.Session{
		ID:         sessionID,
		UserID:     user.ID,
		DeviceName: in.DeviceName,
		IP:         in.IP,
		UserAgent:  in.UserAgent,
		CreatedAt:  time.Now(),
		LastSeenAt: time.Now(),
	}
	if err := u.sessions.Create(ctx, session, u.cfg.JWTRefreshTTL); err != nil {
		return nil, apperror.Internal(err)
	}

	accessToken, accessExp, err := u.jwt.Issue(jwt.Claims{
		UserID:      user.ID,
		EmployeeID:  user.EmployeeID,
		SessionID:   sessionID,
		Roles:       rolesOf(perms),
		Permissions: permCodes(perms),
		Scope:       string(scope),
	})
	if err != nil {
		_ = u.sessions.Delete(ctx, sessionID)
		return nil, apperror.Internal(err)
	}

	refreshToken, err := token.New()
	if err != nil {
		_ = u.sessions.Delete(ctx, sessionID)
		return nil, apperror.Internal(err)
	}
	refreshExp := time.Now().Add(u.cfg.JWTRefreshTTL)

	if err := u.refresh.Save(ctx, token.Hash(refreshToken), sessionID, u.cfg.JWTRefreshTTL); err != nil {
		_ = u.sessions.Delete(ctx, sessionID)
		return nil, apperror.Internal(err)
	}

	_ = u.users.UpdateLastLogin(ctx, user.ID)
	_ = managedDepts // đã nhét vào token qua loadAuthorization

	return &TokenPair{
		AccessToken:        accessToken,
		AccessExpiresAt:    accessExp,
		RefreshToken:       refreshToken,
		RefreshExpiresAt:   refreshExp,
		MustChangePassword: user.MustChangePassword,
	}, nil
}

// Refresh xoay vòng cặp token và phát hiện token bị đánh cắp.
func (u *Usecase) Refresh(ctx context.Context, refreshToken, ip, ua string) (*TokenPair, error) {
	log := logger.FromContext(ctx)

	sessionID, err := u.refresh.Consume(ctx, token.Hash(refreshToken))

	// --- PHÁT HIỆN ĐÁNH CẮP ---
	//
	// Refresh token dùng một lần. Nếu một token đã tiêu lại xuất hiện lần
	// nữa, chỉ có thể là ai đó đã sao chép nó. Chưa biết ai là người thật,
	// nên cắt hết và bắt mọi thiết bị đăng nhập lại.
	if errors.Is(err, domainauth.ErrTokenReused) {
		if s, gerr := u.sessions.Get(ctx, sessionID); gerr == nil && s != nil {
			log.Error().
				Str("user_id", s.UserID.String()).
				Str("ip", ip).
				Msg("PHÁT HIỆN REFRESH TOKEN BỊ TÁI SỬ DỤNG — huỷ toàn bộ phiên")

			_ = u.sessions.DeleteAllOfUser(ctx, s.UserID)
			u.notifySuspiciousActivity(ctx, s.UserID, ip, ua)
		}
		return nil, apperror.New(apperror.KindUnauthorized,
			"Phiên đăng nhập không hợp lệ. Vui lòng đăng nhập lại.")
	}

	if err != nil {
		return nil, apperror.New(apperror.KindUnauthorized, "Phiên đăng nhập đã hết hạn.")
	}

	session, err := u.sessions.Get(ctx, sessionID)
	if err != nil || session == nil {
		return nil, apperror.New(apperror.KindUnauthorized, "Phiên đăng nhập đã kết thúc.")
	}

	user, err := u.users.FindByID(ctx, session.UserID)
	if err != nil || user == nil || !user.IsActive || user.DeletedAt != nil {
		_ = u.sessions.Delete(ctx, sessionID)
		return nil, apperror.New(apperror.KindUnauthorized, "Tài khoản không còn hiệu lực.")
	}

	// Huỷ phiên cũ rồi tạo phiên mới — không tái dùng session_id.
	_ = u.sessions.Delete(ctx, sessionID)

	return u.issueSession(ctx, user, LoginInput{
		IP:         ip,
		UserAgent:  ua,
		DeviceName: session.DeviceName,
	})
}

func errInvalidCredentials() error {
	// Một thông báo duy nhất cho mọi trường hợp sai.
	return apperror.New(apperror.KindUnauthorized,
		"Email hoặc mật khẩu không đúng")
}
```

---

## 9. Bước 5 — Middleware

### `internal/delivery/http/middleware/auth.go`

```go
package middleware

import (
	"net/http"
	"strings"

	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	"github.com/PhamVanPhuc2k2/manage/pkg/jwt"
)

type actorKey struct{}

// RequireAuth xác minh access token và nạp Actor vào context.
func RequireAuth(jwtMgr *jwt.Manager, sessions domainauth.SessionStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := bearerToken(r)
			if raw == "" {
				unauthorized(w, r, "Thiếu token xác thực")
				return
			}

			claims, err := jwtMgr.Verify(raw)
			if err != nil {
				unauthorized(w, r, "Token không hợp lệ hoặc đã hết hạn")
				return
			}

			// Kiểm tra phiên còn sống. Đây là bước biến JWT từ "không thu hồi
			// được" thành "thu hồi tức thì" — xem mục 3.2.
			//
			// Bỏ bước này thì đăng xuất không có tác dụng thật: token vẫn
			// dùng được tới khi hết hạn.
			session, err := sessions.Get(r.Context(), claims.SessionID)
			if err != nil || session == nil {
				unauthorized(w, r, "Phiên đăng nhập đã kết thúc")
				return
			}

			perms := make(map[string]struct{}, len(claims.Permissions))
			for _, p := range claims.Permissions {
				perms[p] = struct{}{}
			}

			actor := &domainauth.Actor{
				UserID:      claims.UserID,
				EmployeeID:  claims.EmployeeID,
				SessionID:   claims.SessionID,
				Roles:       claims.Roles,
				Permissions: perms,
				Scope:       domainauth.Scope(claims.Scope),
			}

			// Cập nhật LastSeenAt để người dùng thấy thiết bị nào đang hoạt
			// động. Chạy nền, lỗi cũng không chặn request.
			go func() { _ = sessions.Touch(contextWithoutCancel(r.Context()), claims.SessionID) }()

			next.ServeHTTP(w, r.WithContext(
				context.WithValue(r.Context(), actorKey{}, actor)))
		})
	}
}

// RequirePermission kiểm tra quyền chi tiết. Đặt SAU RequireAuth.
func RequirePermission(permission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			actor := ActorFrom(r.Context())
			if actor == nil {
				unauthorized(w, r, "Chưa xác thực")
				return
			}
			if !actor.Can(permission) {
				forbidden(w, r, "Bạn không có quyền thực hiện thao tác này")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ActorFrom lấy Actor từ context. Trả về nil nếu route không qua RequireAuth.
func ActorFrom(ctx context.Context) *domainauth.Actor {
	a, _ := ctx.Value(actorKey{}).(*domainauth.Actor)
	return a
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if after, ok := strings.CutPrefix(h, "Bearer "); ok {
		return strings.TrimSpace(after)
	}
	return ""
}
```

> **Vì sao `RequirePermission` không kiểm tra luôn phạm vi dữ liệu?** Vì middleware chỉ nhìn thấy đường dẫn và token, nó không biết bản ghi cụ thể đang bị đụng tới. Kiểm tra phạm vi bắt buộc phải nằm ở usecase, nơi đã biết mình đang xử lý nhân viên nào. Xem mục 3.4.

### Định tuyến

```go
r.Route("/api/v1", func(r chi.Router) {
	// --- Công khai ---
	r.Group(func(r chi.Router) {
		r.Post("/auth/login", authHandler.Login)
		r.Post("/auth/refresh", authHandler.Refresh)
		r.Post("/auth/forgot-password", authHandler.ForgotPassword)
		r.Post("/auth/reset-password", authHandler.ResetPassword)
	})

	// --- Cần đăng nhập ---
	r.Group(func(r chi.Router) {
		r.Use(appmw.RequireAuth(jwtMgr, sessionStore))

		r.Get("/auth/me", authHandler.Me)
		r.Post("/auth/logout", authHandler.Logout)
		r.Post("/auth/logout-all", authHandler.LogoutAll)
		r.Get("/auth/sessions", authHandler.ListSessions)
		r.Post("/auth/change-password", authHandler.ChangePassword)

		r.Route("/employees", func(r chi.Router) {
			r.With(appmw.RequirePermission("employee:read")).
				Get("/", employeeHandler.List)
			r.With(appmw.RequirePermission("employee:read")).
				Get("/{id}", employeeHandler.Get)
			r.With(appmw.RequirePermission("employee:create")).
				Post("/", employeeHandler.Create)
			r.With(appmw.RequirePermission("employee:update")).
				Put("/{id}", employeeHandler.Update)
			r.With(appmw.RequirePermission("employee:delete")).
				Delete("/{id}", employeeHandler.Deactivate)
		})

		r.Route("/departments", func(r chi.Router) {
			r.With(appmw.RequirePermission("department:read")).
				Get("/", departmentHandler.List)
			r.With(appmw.RequirePermission("department:read")).
				Get("/tree", departmentHandler.Tree)
			r.With(appmw.RequirePermission("department:create")).
				Post("/", departmentHandler.Create)
			r.With(appmw.RequirePermission("department:update")).
				Put("/{id}", departmentHandler.Update)
			r.With(appmw.RequirePermission("department:delete")).
				Delete("/{id}", departmentHandler.Delete)
		})
	})
})
```

---

## 10. Bước 6 — Cây phòng ban

### Lấy toàn bộ phòng con bằng truy vấn đệ quy

```sql
-- Dùng cho phạm vi "department": trưởng phòng thấy phòng mình VÀ mọi phòng con.
WITH RECURSIVE subtree AS (
    SELECT id, parent_id, 0 AS depth
    FROM departments
    WHERE id = $1 AND deleted_at IS NULL

    UNION ALL

    SELECT d.id, d.parent_id, s.depth + 1
    FROM departments d
    JOIN subtree s ON d.parent_id = s.id
    WHERE d.deleted_at IS NULL
      AND s.depth < 10   -- van an toàn, xem giải thích bên dưới
)
SELECT id FROM subtree;
```

> **Vì sao có `depth < 10`?** Đây là van an toàn phòng trường hợp dữ liệu đã lỡ có vòng lặp (do lỗi cũ, do sửa tay trong Adminer, do một migration sai). Không có nó, `WITH RECURSIVE` sẽ chạy vô hạn và treo cả connection pool. Công ty không có cơ cấu sâu quá 10 cấp, nên giới hạn này không bao giờ chạm tới trong dữ liệu đúng.

### Chặn vòng lặp khi đổi cha

```go
// validateNoCycle kiểm tra việc đặt newParentID làm cha của departmentID có
// tạo ra vòng lặp hay không.
//
// Vòng lặp trong cây phòng ban là lỗi nghiêm trọng: mọi truy vấn đệ quy sẽ
// chạy vô hạn. Phải chặn ở tầng ghi, vì database không tự chặn được.
func (u *Usecase) validateNoCycle(
	ctx context.Context,
	departmentID uuid.UUID,
	newParentID *uuid.UUID,
) error {
	if newParentID == nil {
		return nil // thành phòng gốc, luôn an toàn
	}
	if *newParentID == departmentID {
		return apperror.Invalid("Phòng ban không thể là cấp trên của chính nó", nil)
	}

	// Đi ngược lên tổ tiên của cha mới. Nếu gặp lại chính phòng đang sửa
	// thì tức là ta đang tạo vòng lặp.
	ancestors, err := u.repo.ListAncestorIDs(ctx, *newParentID)
	if err != nil {
		return apperror.Internal(err)
	}
	for _, id := range ancestors {
		if id == departmentID {
			return apperror.Invalid(
				"Không thể chuyển phòng ban vào bên trong chính phòng con của nó", nil)
		}
	}
	return nil
}
```

Áp dụng y hệt cho `employees.manager_id` — chuỗi cấp trên cũng là một cây và cũng vòng lặp được.

---

## 11. Bước 7 — Phân trang và tìm kiếm

### Quy ước phân trang

```
GET /api/v1/employees?page=1&page_size=20&search=nguyen&department_id=...&status=official
```

```json
{
  "data": [ ... ],
  "meta": {
    "page": 1,
    "page_size": 20,
    "total_items": 137,
    "total_pages": 7
  }
}
```

Dùng offset (`LIMIT/OFFSET`) chứ không dùng cursor, vì giao diện quản trị cần nhảy tới trang bất kỳ. Cursor chỉ cần cho cuộn vô hạn — sẽ dùng ở Phase 5 cho tin nhắn.

**Bắt buộc chặn trên `page_size`:**

```go
const (
	defaultPageSize = 20
	maxPageSize     = 100
)

func normalizePageSize(n int) int {
	// Không chặn thì ai đó gửi page_size=1000000 là kéo sập database.
	if n <= 0 {
		return defaultPageSize
	}
	if n > maxPageSize {
		return maxPageSize
	}
	return n
}
```

### Truy vấn tìm kiếm

```sql
SELECT e.id, e.employee_code, e.full_name, e.email, e.status,
       d.name AS department_name,
       p.name AS position_name,
       COUNT(*) OVER() AS total_count
FROM employees e
LEFT JOIN departments d ON d.id = e.department_id
LEFT JOIN positions   p ON p.id = e.position_id
WHERE e.deleted_at IS NULL
  AND ($1::uuid IS NULL OR e.department_id = ANY($2::uuid[]))
  AND ($3::text IS NULL OR
       f_unaccent(lower(e.full_name))    LIKE '%' || f_unaccent(lower($3)) || '%'
    OR lower(e.employee_code)            LIKE '%' || lower($3) || '%')
ORDER BY e.full_name
LIMIT $4 OFFSET $5;
```

> `COUNT(*) OVER()` lấy tổng số bản ghi trong cùng một truy vấn, khỏi chạy hai lần. Đổi lại PostgreSQL phải quét hết tập kết quả — với vài nghìn nhân viên thì không đáng kể. Khi nào bảng lên hàng trăm nghìn dòng thì tách thành truy vấn đếm riêng, hoặc bỏ tổng số đi và chuyển sang cursor.

---

## 12. Bước 8 — Tải ảnh đại diện

Luồng ba bước, **không** cho file đi xuyên qua backend Go:

```
1. Client  →  POST /api/v1/employees/{id}/avatar/upload-url
              Server trả presigned PUT URL (hết hạn 5 phút) + object key

2. Client  →  PUT thẳng lên Cloudflare R2 bằng URL đó

3. Client  →  POST /api/v1/employees/{id}/avatar/confirm { "key": "..." }
              Server kiểm tra object có thật, đúng kiểu, đúng dung lượng,
              rồi mới ghi avatar_key vào database
```

**Vì sao phải có bước 3?** Presigned URL chỉ cho phép ghi, nó không kiểm tra nội dung. Không có bước xác nhận, ai đó có thể tải lên một file thực thi rồi gọi API gán nó làm avatar.

Bước 3 phải kiểm tra:
- Object tồn tại (`StatObject`)
- Kích thước ≤ 2MB
- **Kiểu file xác định bằng magic bytes, không tin `Content-Type`** — header do client gửi, sửa được tuỳ ý
- Chỉ chấp nhận `image/jpeg`, `image/png`, `image/webp`

Khi trả về hồ sơ, sinh presigned GET URL hạn 1 giờ chứ không lưu URL vào database — URL có chữ ký và sẽ hết hạn.

Ngoài ra, khoá object phải nằm đúng thư mục của nhân viên đó (`avatars/{employee_id}/...`) và server phải kiểm tra tiền tố đó ở bước 3. Thiếu kiểm tra này, ai đó gọi confirm với khoá của người khác là gán được ảnh bất kỳ trong bucket.

### Cloudflare R2 khác AWS S3 ở đâu

R2 dùng giao thức S3 nên `aws-sdk-go-v2` chạy được, nhưng có ba chỗ sai là hỏng, và lỗi sinh ra đều khó đoán:

| Việc phải làm | Sai thì sao |
|---|---|
| `Region` đặt đúng chuỗi `"auto"` | Điền tên vùng của AWS (`us-east-1`…) làm chữ ký sai, R2 trả `SignatureDoesNotMatch` |
| `UsePathStyle = true` | Mặc định SDK ghép bucket vào tên miền (`<bucket>.<account>.r2.cloudflarestorage.com`) — R2 không hỗ trợ dạng đó, mọi request lỗi phân giải tên miền |
| `RequestChecksumCalculation = WhenRequired` | Từ giữa 2025 SDK tự thêm header `x-amz-checksum-crc32` vào PutObject. Trình duyệt không gửi header đó khi dùng presigned URL nên chữ ký không khớp — lỗi trông y hệt sai credentials |

Địa chỉ có dạng `https://<ACCOUNT_ID>.r2.cloudflarestorage.com`, lấy cùng chỗ với access key trong Cloudflare Dashboard → R2 → Manage R2 API Tokens.

**Chưa cấu hình R2 thì hệ thống vẫn chạy.** `storage.New` trả về `nil` và mọi chỗ dùng đều kiểm tra `nil` rồi báo lỗi 422 kèm thông báo rõ ràng. Nhờ vậy phát triển phần không liên quan tới tệp không cần tài khoản Cloudflare. Endpoint `/ready` hiện `"r2": "chưa cấu hình"` nhưng **không** vì thế mà báo cả hệ thống chưa sẵn sàng — mất chỗ lưu tệp không đáng để cắt toàn bộ traffic.

---

## 13. Bước 9 — Quên mật khẩu

```
1. POST /auth/forgot-password { email }
   → LUÔN trả về 200 kèm thông báo chung, dù email có tồn tại hay không
   → Nếu tồn tại: sinh token, lưu pwreset:{hash} trong Redis TTL 30 phút,
     đẩy job gửi mail sang worker

2. Worker gửi mail chứa link:
   {PUBLIC_BASE_URL}/reset-password?token=xxx

3. POST /auth/reset-password { token, new_password }
   → Tra Redis, XOÁ KEY NGAY (dùng một lần)
   → Đổi mật khẩu
   → HUỶ TOÀN BỘ PHIÊN của user đó
```

Ba điểm bắt buộc:

**Luôn trả 200 ở bước 1.** Trả 404 khi email không tồn tại là biến endpoint này thành công cụ dò danh sách email nhân viên.

**Xoá token ngay khi dùng, trước cả khi đổi mật khẩu thành công.** Để sau thì hai request đồng thời có thể cùng dùng một token.

**Huỷ hết phiên sau khi đổi mật khẩu.** Người dùng đổi mật khẩu thường là vì nghi bị lộ. Không cắt phiên cũ thì kẻ tấn công vẫn đang đăng nhập.

Mail gửi ở môi trường dev vào MailHog, xem tại `http://localhost:8025`.

---

## 14. Bước 10 — Frontend

### Quản lý token

```
lib/auth/token-store.ts   — giữ access token trong BIẾN JS, không localStorage
lib/auth/api-client.ts    — tự gắn token, tự refresh khi gặp 401
lib/auth/AuthProvider.tsx — context React, gọi refresh lúc khởi động
```

**Chi tiết quan trọng nhất: gộp các lần refresh đồng thời.**

Khi access token hết hạn, thường có nhiều request cùng lỗi 401 một lúc (trang đang tải 5 widget). Nếu mỗi request tự gọi refresh, sẽ có 5 lần refresh song song — và vì có xoay vòng token, 4 trong 5 lần sẽ dùng token đã tiêu, kích hoạt cơ chế phát hiện đánh cắp và **đăng xuất người dùng vô cớ**.

```ts
let refreshPromise: Promise<string> | null = null;

async function getValidToken(): Promise<string> {
  const token = tokenStore.get();
  if (token && !isExpiringSoon(token)) return token;

  // Mọi request cùng chờ MỘT lời gọi refresh duy nhất.
  // Thiếu đoạn gộp này, cơ chế chống đánh cắp token sẽ liên tục
  // đá người dùng ra ngoài — lỗi rất khó lần ra nguyên nhân.
  refreshPromise ??= refreshAccessToken().finally(() => {
    refreshPromise = null;
  });

  return refreshPromise;
}
```

### Vì sao trạng thái đăng nhập dùng Zustand chứ không phải React Context

Bản đầu tiên tách làm đôi: access token nằm trong một biến module (`tokenStore`), còn `user` nằm trong React state của `AuthProvider`. Lý do của sự chia đôi là `api-client` cần đọc token nhưng nó không phải component React nên không dùng được Context.

**Chia đôi thì hai nửa lệch nhau được, và đã lệch thật.** `api-client` xoá token ở ba chỗ khi refresh thất bại, nhưng không đụng được vào `user`. Hậu quả: phiên bị huỷ từ nơi khác (bấm "đăng xuất mọi thiết bị" ở máy khác, hoặc admin vô hiệu hoá tài khoản) thì token bị xoá còn `user` vẫn còn — `AppShell` tưởng đang đăng nhập nên tiếp tục render, người dùng nhìn thấy một trang hỏng với mọi bảng báo lỗi thay vì được đưa về trang đăng nhập.

Zustand đọc ghi được ở cả hai phía:

```ts
// trong component — có selector nên chỉ render lại khi đúng phần đó đổi
const user = useAuthStore((s) => s.user);

// ở api-client, ngoài React
useAuthStore.getState().clear();
```

Nhờ vậy chỉ còn **một** đường xoá phiên, và nó xoá token lẫn `user` trong cùng một thao tác — không còn cách nào làm lệch.

Bài học chung: khi một phần trạng thái phải đọc được từ ngoài React, đừng tách nó ra khỏi phần còn lại. Cái giá không phải là thêm một thư viện mà là một lỗi rất khó tái hiện.

### Bảo vệ route

`middleware.ts` của Next.js chỉ kiểm tra **sự tồn tại** của cookie refresh và chuyển hướng. Nó không được coi là kiểm soát bảo mật — đó chỉ là để trải nghiệm mượt. Quyết định thật nằm ở backend.

### Hiển thị theo quyền

```tsx
const { can } = usePermission();

{can("employee:create") && <Button>Thêm nhân viên</Button>}
```

Nhắc lại: **đây chỉ là trang trí.** Backend vẫn phải kiểm tra đầy đủ.

### Danh sách trang

| Đường dẫn | Nội dung |
|---|---|
| `/login` | Đăng nhập |
| `/forgot-password`, `/reset-password` | Quên và đặt lại mật khẩu |
| `/change-password` | Bắt buộc khi `must_change_password` |
| `/employees` | Danh sách: bảng, lọc, tìm kiếm, phân trang |
| `/employees/new`, `/employees/{id}/edit` | Form, validate bằng zod |
| `/employees/{id}` | Chi tiết hồ sơ |
| `/departments` | Danh sách và cây phòng ban |
| `/profile` | Hồ sơ cá nhân, đổi mật khẩu, danh sách thiết bị đang đăng nhập |

---

## 15. Kiểm chứng bảo mật

Chạy tay từng mục, đừng tin là "chắc đúng".

| Thử | Kết quả phải ra |
|---|---|
| `curl` API mà không kèm token | 401 |
| Dùng token của nhân viên thường gọi `POST /employees` | 403 |
| Nhân viên thường gọi `GET /employees` | Chỉ thấy chính mình |
| Nhân viên thường gọi `GET /employees/{id_người_khác}` | 404 hoặc 403, **không phải 200** |
| Trưởng phòng A xem nhân viên phòng B | 404 hoặc 403 |
| Sửa `alg` trong JWT thành `none` | 401 |
| Sửa payload JWT (đổi `scope` thành `all`) | 401 (sai chữ ký) |
| Đăng xuất rồi dùng lại access token cũ | 401 **ngay lập tức**, không chờ 15 phút |
| Gửi lại refresh token đã dùng | 401 + toàn bộ phiên bị huỷ |
| Sai mật khẩu 6 lần | Bị chặn, thông báo thời gian chờ |
| Đăng nhập email không tồn tại | Cùng thông báo và **thời gian phản hồi tương đương** với sai mật khẩu |
| Đặt lại mật khẩu bằng link đã dùng | Từ chối |
| `page_size=999999` | Bị cắt về 100 |
| Đặt phòng ban làm con của chính nó | 400, cây không hỏng |

---

## 16. Cạm bẫy đã biết

| Cạm bẫy | Hậu quả | Cách tránh |
|---|---|---|
| Quên kiểm tra phạm vi ở usecase | Nhân viên đọc được hồ sơ cả công ty (IDOR) | Gọi `actor.CanSeeEmployee()` trước **mọi** thao tác trên nhân viên cụ thể |
| Không kiểm tra thuật toán khi verify JWT | Giả mạo token bằng `alg=none` | Dùng `jwt.WithValidMethods` và kiểm tra kiểu trong keyfunc |
| Không gộp refresh đồng thời ở frontend | Người dùng bị đăng xuất ngẫu nhiên | Gộp về một promise duy nhất |
| Thông báo lỗi phân biệt "email sai" và "mật khẩu sai" | Dò được danh sách email | Một thông báo chung, thời gian phản hồi tương đương |
| Không huỷ phiên khi đổi mật khẩu | Kẻ tấn công vẫn đăng nhập | `DeleteAllOfUser` sau khi đổi |
| `unaccent` không IMMUTABLE | Không tạo được chỉ số, tìm kiếm quét toàn bảng | Bọc thành `f_unaccent` |
| Truy vấn không dùng đúng biểu thức đã đánh chỉ số | Chỉ số bị bỏ qua | Viết `WHERE f_unaccent(lower(col)) LIKE ...` đúng như lúc `CREATE INDEX` |
| Quên `WHERE deleted_at IS NULL` | Nhân viên đã nghỉ vẫn hiện, vẫn đăng nhập được | Viết vào mọi truy vấn; cân nhắc thêm view `active_employees` |
| Chỉ số unique trên email không có mệnh đề WHERE | Không tái sử dụng được email của người đã nghỉ | `CREATE UNIQUE INDEX ... WHERE deleted_at IS NULL` |
| Cho phép tạo vòng lặp trong cây | `WITH RECURSIVE` treo database | Kiểm tra tổ tiên trước khi ghi + van `depth < 10` |
| Tin `Content-Type` khi tải file lên | Tải được file thực thi | Kiểm tra magic bytes phía server |
| Seed mật khẩu admin trong migration | Mật khẩu nằm trong git vĩnh viễn | Dùng lệnh `cmd/seed`, in ra stdout một lần |
| Xoá mềm nhân viên mà quên đặt `users.deleted_at` | Email bị khoá vĩnh viễn: nhân viên nghỉ rồi quay lại không tạo được tài khoản, lỗi hiện ra là 500 khó hiểu | Đặt cả `deleted_at` chứ không chỉ `is_active` — chỉ số unique là chỉ số một phần `WHERE deleted_at IS NULL` |
| Xoay vòng refresh token không có thời gian ân hạn | Hai tab cùng F5 làm cả hai bị đăng xuất; phản hồi rơi mạng cũng bị coi là đánh cắp | Cho ân hạn 10 giây sau lần dùng đầu — xem `RefreshGracePeriod` |
| Tra tài khoản SAU khi xoá mềm nhân viên | Truy vấn lọc `deleted_at IS NULL` nên không ra kết quả, nhánh cắt phiên bị bỏ qua âm thầm: người vừa cho nghỉ vẫn dùng hệ thống thêm 15 phút với đầy đủ quyền cũ | Tra TRƯỚC khi xoá, giữ lại `user.ID` để cắt phiên sau |
| Ân hạn xử lý bằng cách cấp phiên MỚI | Mỗi chu kỳ refresh với N tab đẻ ra N phiên rác sống 7 ngày; trang thiết bị đầy dòng trùng; và đăng xuất bị vô hiệu — gửi lại token cũ trong 10 giây là phiên vừa cắt sống lại dưới id khác | Đặt chỗ id phiên mới ngay trong lệnh Lua tiêu token, lần dùng lại bám vào đúng phiên đó |

Hai dòng cuối là hậu quả dây chuyền của hai dòng ngay trên chúng — bản vá
nào cũng nên đọc lại chỗ nó vừa đổi giả định.

---

## 16.1. Vì sao phiên đăng nhập lại sinh nhiều lỗi ngầm đến thế

Nhìn lại bảng trên: quá nửa số dòng thuộc về phiên và token. Không phải ngẫu nhiên. Phiên đăng nhập có bốn đặc điểm mà hầu như không phần nào khác của hệ thống có đủ cả bốn.

**1. Nó nằm rải ở năm nơi cùng lúc.** Một danh sách nhân viên chỉ có một bản gốc: hàng trong PostgreSQL. Một phiên đăng nhập thì nằm ở: access token trong bộ nhớ trình duyệt, cookie refresh trong trình duyệt, bản ghi phiên trong Redis, bản ghi refresh token trong Redis, và hàng `users` trong PostgreSQL. Mọi thao tác động tới danh tính đều phải giữ năm nơi đó khớp nhau. Lỗi sinh ra từ việc cập nhật được ba, quên hai.

**2. Yêu cầu của nó mang dấu âm.** Phần lớn tính năng là "sau khi làm X, điều Y phải đúng" — sai thì màn hình trắng, API trả 500, ai cũng thấy. Phiên thì ngược lại: "sau khi làm X, điều Y phải **thôi** hoạt động". Thu hồi hỏng **không tạo ra lỗi nào cả**. Không exception, không log, không cảnh báo. Một thứ đáng lẽ phải chết thì vẫn sống, và cách duy nhất để biết là có phép thử khẳng định "chỗ này phải trả 401".

> Đúng hai lỗi ở hai dòng cuối bảng trên đều thuộc loại này. Cả hai đều chạy trơn tru, không một dòng log bất thường.

**3. Thời gian là tham số thật.** TTL, thời gian ân hạn, lệch đồng hồ giữa các máy, đua nhau lúc token sắp hết hạn. Phép thử không điều khiển được thời gian thì không nhìn thấy nhóm lỗi này.

**4. Đồng thời là chuyện thường ngày, không phải ngoại lệ.** Người dùng mở năm tab. Điện thoại mất sóng rồi gửi lại. Trình duyệt khôi phục phiên làm việc và bắn mười request một lúc. Suy nghĩ theo kiểu "một request tại một thời điểm" ở đây là sai ngay từ đầu.

Và điều nguy hiểm nhất: **mọi module khác đều có quyền phải huỷ phiên.** Đổi mật khẩu, đổi vai trò, cho nghỉ việc, khoá tài khoản, xoá nhân viên — mỗi thứ là một nơi phải nhớ gọi thu hồi. Module phiên không biết ai đang phụ thuộc vào mình. Thêm một tính năng ở Phase 4 mà quên gọi, không có gì báo cho bạn biết.

### Dự án lớn giải quyết bằng cách nào

Không ai giải bằng "cẩn thận hơn". Họ đổi cấu trúc để cái sai không còn chỗ tồn tại.

**Số hiệu phiên bản thay cho việc đi tìm từng phiên.** Đây là kỹ thuật quan trọng nhất và cũng rẻ nhất. Thêm một cột vào `users`, ví dụ `sessions_valid_after TIMESTAMPTZ`. Nhúng thời điểm phát hành vào token. Middleware từ chối mọi token phát hành trước mốc đó.

Từ đó, "huỷ toàn bộ phiên" không còn là đi liệt kê và xoá từng phiên nữa, mà là **một phép gán**:

```sql
UPDATE users SET sessions_valid_after = NOW() WHERE id = $1;
```

Cái hay nằm ở chỗ nó gộp được vào cùng transaction với hành động gây ra nó:

```sql
-- Cho nghỉ việc: xoá mềm VÀ huỷ phiên, trong một giao dịch, không thể lệch
UPDATE users SET is_active = FALSE, deleted_at = NOW(), sessions_valid_after = NOW()
WHERE employee_id = $1;
```

Cả hai lỗi ở cuối bảng cạm bẫy đều **biến mất về mặt cấu trúc** dưới mô hình này: không còn bước "tra tài khoản rồi gọi thu hồi" để mà quên, cũng không còn phiên nào sống sót được. Django (`get_session_auth_hash`), Rails/Devise, Firebase (`tokensValidAfterTime`), GitHub đều dùng ý tưởng này.

**Thu hồi là sự kiện, không phải lời gọi hàm.** Thay vì mỗi module tự nhớ gọi `LogoutAll`, module phiên lắng nghe các sự kiện miền: `user.password_changed`, `user.deactivated`, `user.roles_changed`. Thêm tính năng mới chỉ cần phát sự kiện đúng tên. Dự án này đã đi nửa đường — callback `onEmployeeDeactivated` chính là hình thức sơ khai của nó, và RabbitMQ đã sẵn sàng cho phần còn lại.

**Một nơi duy nhất trả lời "token này còn hiệu lực không".** OAuth2 gọi là token introspection (RFC 7662). Mỗi dịch vụ tự đoán lấy là bắt đầu lệch nhau.

**Access token ngắn + refresh xoay vòng + phát hiện tái sử dụng + thời gian ân hạn.** Chính là thứ dự án này đang làm; đây là khuyến nghị của OAuth 2.1 BCP, và "ân hạn" là tên gọi chuẩn trong ngành (Auth0 gọi là *rotation leeway*).

**Token gắn với thiết bị** (DPoP, mTLS) cho hệ thống giá trị cao: token bị đánh cắp đem sang máy khác cũng vô dụng. Đắt và phức tạp, chỉ đáng khi dữ liệu đủ quý.

**Kiểm chứng chiều âm, tự động, chạy mọi lần.** Vì thu hồi hỏng trong im lặng, mỗi đường thu hồi phải có một phép thử khẳng định 401. Đó chính xác là việc `scripts/smoke-auth.sh` đang làm, và là lý do nó bắt được cả hai lỗi trên.

**Ghi log mọi lần thu hồi, kèm lý do.** Rồi cảnh báo khi thấy "token được dùng sau thời điểm bị thu hồi" — dấu hiệu hoặc có lỗ hổng, hoặc đang bị tấn công.

### Việc nên làm cho dự án này

Thêm `sessions_valid_after` vào bảng `users` ở Phase 2, trước khi có thêm module nào đụng tới danh tính. Càng nhiều nơi phải nhớ gọi thu hồi thì càng chắc chắn có một nơi quên — và nơi quên đó sẽ không báo cho ai biết.

---

## 17. Thứ tự triển khai đề xuất

Chia nhỏ để lúc nào cũng có thứ chạy được, không phải viết hai tuần rồi mới biết sai.

| Bước | Nội dung | Kiểm chứng được gì |
|---|---|---|
| 1 | Đổi module path | `go build ./...` chạy |
| 2 | Migration + seed RBAC | Bảng có, `make psql` xem được |
| 3 | `pkg/hash`, `pkg/jwt`, `pkg/token` | Phát hành và xác minh token |
| 4 | `cmd/seed` | Tạo được tài khoản admin |
| 5 | Repository + SessionStore Redis | Lưu và đọc phiên |
| 6 | Usecase login + `POST /auth/login` | **Đăng nhập được bằng curl** |
| 7 | Middleware RequireAuth + `GET /auth/me` | Token bảo vệ được route |
| 8 | Refresh, logout, logout-all | Vòng đời phiên đầy đủ |
| 9 | RequirePermission + phạm vi dữ liệu | **Chạy hết bảng ở mục 15** |
| 10 | CRUD phòng ban + chức vụ | Có dữ liệu để gắn nhân viên |
| 11 | CRUD nhân viên + tìm kiếm | Nghiệp vụ chính xong |
| 12 | Frontend đăng nhập + danh sách nhân viên | Dùng được bằng trình duyệt |
| 13 | Avatar, quên mật khẩu, quản lý thiết bị | Hoàn thiện |

**Mốc quan trọng nhất là bước 9.** Đừng viết tiếp CRUD khi phân quyền chưa chạy đúng — mọi endpoint viết sau đó đều dựa vào nó, và sửa sau sẽ phải rà lại toàn bộ.

---

## 18. Sau Phase 1

Sang [Phase 2](./TASKS.md#phase-2--dự-án--công-việc): dự án, giao việc, bảng Kanban.

Phase 2 là phase đầu tiên có **một module gọi sang module khác**: `usecase/project` cần kiểm tra nhân viên được giao việc có tồn tại và còn làm việc không. Cách làm đúng — cũng là quy tắc giữ cho monolith không rối:

```go
// internal/usecase/project/usecase.go

// EmployeeLookup khai báo ĐÚNG những gì module project cần từ module
// employee — không hơn. Khai báo ở ĐÂY, phía người dùng.
//
// Nhờ interface hẹp này, module project test được mà không cần module
// employee thật, và sau này đổi cách lấy dữ liệu nhân viên cũng không
// phải sửa gì trong project.
type EmployeeLookup interface {
	Exists(ctx context.Context, id uuid.UUID) (bool, error)
	IsActive(ctx context.Context, id uuid.UUID) (bool, error)
}
```

Module `employee` đáp ứng interface đó. Tuyệt đối không cho `usecase/project` gọi thẳng `repository/postgres.EmployeeRepository`.
