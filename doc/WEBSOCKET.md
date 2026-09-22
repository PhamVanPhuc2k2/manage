# Giao thức WebSocket

Tài liệu đầy đủ các bản tin đi qua `/ws`. Đây là hợp đồng giữa backend và
client — đổi một tên loại bản tin ở một bên là làm hỏng bên kia trong im lặng:
bản tin vẫn tới nơi, không ai xử lý, và **không có lỗi nào được báo**.

## Kết nối

```
GET /ws?token=<access_token>
```

Xác thực bằng token trên **query string**, không phải header và không phải
cookie:

- Trình duyệt **không** cho đặt header tuỳ ý khi mở WebSocket, nên
  `Authorization: Bearer ...` là bất khả thi từ JavaScript.
- Cookie thì trình duyệt tự gửi kèm, và đó chính là điều khiến
  **Cross-Site WebSocket Hijacking** khai thác được: một trang độc hại mở kết
  nối, cookie tự bay theo, và kẻ tấn công đọc được luồng dữ liệu.

Token trên query string có nhược điểm riêng — nó lọt vào access log. Bù lại,
access token chỉ sống 15 phút và phiên có thể thu hồi tức thì.

Server kiểm tra ba thứ lúc bắt tay:

1. Chữ ký và hạn của token.
2. **Phiên còn sống** trong Redis. Bỏ bước này thì đăng xuất không cắt được
   kết nối WebSocket: nó đã mở rồi và sẽ sống tiếp hàng giờ.
3. `Origin` nằm trong `CORS_ALLOWED_ORIGINS`. WebSocket **không** được CORS
   bảo vệ như request thường, nên đây là hàng rào duy nhất ở tầng trình duyệt.

Quyền được dựng **một lần** lúc bắt tay và giữ trên kết nối. Mọi bản tin
nghiệp vụ gửi lên đều kiểm tra quyền y như REST — thiếu bước đó thì WebSocket
thành cửa sau đi vòng qua toàn bộ phân quyền HTTP.

## Định dạng bản tin

Một hình dạng duy nhất cho **mọi** bản tin, cả hai chiều:

```json
{
  "type": "chat.message",
  "payload": { },
  "ts": "2026-09-22T10:30:00+07:00",
  "trace_id": "abc123/xyz-000042"
}
```

| Trường | Chiều | Ghi chú |
|---|---|---|
| `type` | cả hai | Định tuyến. Xem bảng bên dưới |
| `payload` | cả hai | Tuỳ loại; có thể thiếu |
| `ts` | cả hai | RFC 3339 |
| `trace_id` | server → client | Nối bản tin với request HTTP đã sinh ra nó |

Một hình dạng chung là có chủ ý: client chỉ cần viết một bộ giải mã và một bộ
định tuyến theo `type`. Mỗi loại một hình dạng riêng sẽ khiến phần xử lý ở
frontend phình ra theo số loại.

`trace_id` là thứ khiến một thông báo đến sai người trở thành chuyện lần ngược
được. Không có nó thì không.

## Giới hạn

| Giới hạn | Giá trị | Lý do |
|---|---|---|
| Kích thước bản tin | 32 KB | Đủ cho mọi bản tin điều khiển và một tin chat dài. Không giới hạn thì một client độc hại gửi vài bản tin trăm MB là hết RAM của instance |
| Tốc độ gửi | 60 bản tin / 10 giây | Chống spam, và chặn cả client lỗi gửi vòng lặp vô hạn do bug |
| Hàng đợi gửi mỗi client | 64 bản tin | Đầy thì **đóng kết nối**, không chặn: chặn sẽ kéo chậm mọi người nhận khác của cùng một bản tin |
| Nhịp tim | 30 giây | Client gửi |
| Ping của server | 54 giây | Server chủ động, vì nhiều proxy đóng kết nối im lặng sau một khoảng không có dữ liệu |
| Hạn đọc | 60 giây | Không nhận được gì trong khoảng này thì đóng |

Vượt giới hạn tốc độ thì server trả `error` với `code: "RATE_LIMITED"` và
**không** đóng kết nối.

## Client → Server

Danh sách này cố ý rất hẹp. Mọi loại khác bị từ chối với
`code: "UNKNOWN_TYPE"`: WebSocket không phải một cổng API thứ hai, nghiệp vụ
đi qua REST nơi đã có sẵn phân quyền và nhật ký.

### `heartbeat`

Nhịp tim, mỗi 30 giây. **Đây là nguồn dữ liệu chấm công của hệ thống.**

```json
{ "type": "heartbeat", "payload": { "is_active": true } }
```

`is_active` phân biệt "mở tab" với "đang làm việc". Client tự đặt `false` khi
tab bị ẩn hoặc không có chuột/bàn phím quá 5 phút.

Phân biệt này là điều kiện để dữ liệu chấm công không vô nghĩa: máy để đó qua
đêm vẫn online, nhưng không ai làm việc.

Thiếu `payload` thì server coi như `is_active: true` — client cũ chưa biết gửi
cờ vẫn phải được tính là online.

Server **không** ghi database ở đây, chỉ ghi presence vào Redis. Một job nền
quét Redis mỗi phút để dựng dữ liệu chấm công. Ghi thẳng vào database sẽ là
một lệnh INSERT cho mỗi nhân viên mỗi 30 giây — với 200 người là 400 lượt ghi
mỗi phút để thu được đúng lượng thông tin mà một lần quét gom lại được.

### `ping`

Thăm dò. Server trả `pong`. Không có payload.

### `chat.send`

Gửi tin nhắn.

```json
{
  "type": "chat.send",
  "payload": {
    "conversation_id": "uuid",
    "content": "xin chào",
    "kind": "text",
    "reply_to_id": "uuid",
    "client_message_id": "uuid-do-client-sinh"
  }
}
```

`client_message_id` **chống trùng**. Mất mạng giữa chừng, client không biết
tin đã tới hay chưa nên gửi lại; server tra mã đó trước khi ghi và trả về đúng
tin cũ. Cùng một mã dùng được cho cả đường REST, nên gửi lại bằng đường kia
sau khi WebSocket đứt cũng không sinh tin đôi.

`kind` không được là `system`: loại đó chỉ server sinh. Cho client gửi nó là
để nó giả được những dòng "X đã rời nhóm" mà không ai phân biệt nổi.

Server phát `chat.message` cho **mọi** thành viên, kể cả người gửi. Nhờ vậy
tin nhắn về tới mọi tab của người gửi theo đúng một đường, và client chỉ cần
một chỗ xử lý thay vì hai.

Cần quyền `chat:read`.

### `chat.typing`

Chỉ báo "đang nhập".

```json
{ "type": "chat.typing", "payload": { "conversation_id": "uuid" } }
```

**Không lưu database**: nó hết giá trị sau vài giây, và ghi mỗi lần gõ phím sẽ
tạo ra lượng ghi lớn hơn cả tin nhắn thật.

Client phải **tự tiết chế**, tối đa một lần mỗi 2 giây. Gửi mỗi lần gõ phím là
hàng chục bản tin mỗi câu, và với giới hạn 60 bản tin / 10 giây thì gõ nhanh
là tự làm mình bị chặn.

Lỗi ở loại này bị bỏ qua trong im lặng: nó là tín hiệu trang trí, và một bản
tin lỗi cho mỗi lần gõ phím còn phiền hơn việc thiếu nó.

### `chat.read`

Báo đã đọc tới một tin.

```json
{
  "type": "chat.read",
  "payload": { "conversation_id": "uuid", "message_id": "uuid" }
}
```

Mốc đã đọc chỉ dời **về phía trước**. Hai tab cùng mở một hội thoại sẽ báo đọc
theo thứ tự bất kỳ, và không có điều kiện đó thì tab cuộn ngược lên sẽ làm tin
mới "chưa đọc" trở lại.

## Server → Client

### `welcome`

Bản tin đầu tiên sau khi bắt tay.

```json
{
  "type": "welcome",
  "payload": {
    "conn_id": "uuid",
    "employee_id": "uuid",
    "heartbeat_interval": 30,
    "instance_id": "ecc91196c6d0"
  }
}
```

`heartbeat_interval` do **server** quyết định, tính bằng giây. Client nên đọc
nó thay vì hằng số cứng: đổi chu kỳ ở server là đổi cho mọi client, không phải
triển khai lại frontend.

`instance_id` là instance api đang giữ kết nối. Dùng để chẩn đoán khi chạy
nhiều replica.

### `pong`

Trả lời `ping`. Không có payload.

### `error`

Báo lỗi mà **không** đóng kết nối.

```json
{ "type": "error", "payload": { "code": "RATE_LIMITED", "message": "..." } }
```

| `code` | Nghĩa |
|---|---|
| `BAD_MESSAGE` | JSON sai định dạng, hoặc thiếu trường bắt buộc |
| `UNKNOWN_TYPE` | Loại bản tin không được hỗ trợ |
| `RATE_LIMITED` | Vượt 60 bản tin / 10 giây |
| `FORBIDDEN` | Không có quyền cho thao tác này |
| `CHAT_UNAVAILABLE` | Module chat chưa được cắm vào hub |
| `SEND_FAILED` | Gửi tin thất bại; `message` mang lý do nghiệp vụ |

### `notification`

Một thông báo mới. Payload là **đúng** hình dạng mà
`GET /api/v1/notifications` trả về:

```json
{
  "type": "notification",
  "payload": {
    "id": "uuid",
    "type": "task_assigned",
    "title": "Quản trị viên đã giao việc cho bạn",
    "body": "WSRT-1 — Viec realtime",
    "link": "/tasks/uuid",
    "actor_id": "uuid",
    "actor_name": "Quản trị viên",
    "resource": "task",
    "resource_id": "uuid",
    "read": false,
    "created_at": "2026-09-22T10:30:00+07:00"
  }
}
```

Dùng **chung một hình dạng** với REST là bắt buộc, không phải tiện tay. Client
dựng một thẻ thông báo duy nhất; hai hình dạng khác nhau sẽ buộc nó có hai
nhánh vẽ, và nhánh realtime là nhánh ít được kiểm thử hơn nên cũng là nhánh
hay lệch.

Đây từng là một lỗi thật ở Phase 5: tầng nghiệp vụ đẩy thẳng entity domain
(không có json tag) nên client nhận `{"ID":...}` thay vì `{"id":...}` và im
lặng bỏ qua. **Không phép thử REST nào bắt được** — chỉ lộ ra khi nối một
client WebSocket thật và đọc bản tin.

### `notification.badge`

Chỉ mang số chưa đọc.

```json
{ "type": "notification.badge", "payload": { "unread": 3 } }
```

Tách khỏi `notification` vì đó là hai việc khác nhau: một thông báo mới làm số
tăng, nhưng **đọc ở tab khác cũng làm số đổi mà không có thông báo nào mới**.
Gộp lại thì tab kia sẽ hiện một thẻ thông báo ma.

### `chat.message`

Tin nhắn mới (hoặc tin hệ thống). Payload giống hệt phần tử trong
`GET /api/v1/chat/conversations/{id}/messages`:

```json
{
  "type": "chat.message",
  "payload": {
    "id": "uuid",
    "conversation_id": "uuid",
    "sender_id": "uuid",
    "sender_name": "Trần Văn A",
    "kind": "text",
    "content": "xin chào",
    "reply_to_id": "uuid",
    "reply_to_sender": "Nguyễn Thị B",
    "reply_to_content": "câu được trả lời, cắt 120 ký tự",
    "client_message_id": "uuid-do-client-sinh",
    "attachments": [],
    "deleted": false,
    "edited_at": null,
    "created_at": "2026-09-22T10:30:00+07:00"
  }
}
```

`client_message_id` được trả **ngược về** để client khớp tin lạc quan đang
hiện trên màn hình với tin thật từ server, thay vì vẽ thêm một tin trùng.

Tin hệ thống có `kind: "system"` và `sender_id: null`.

### `chat.message_edited`

Cùng payload với `chat.message`, `edited_at` khác `null`.

### `chat.message_deleted`

```json
{
  "type": "chat.message_deleted",
  "payload": { "id": "uuid", "conversation_id": "uuid" }
}
```

Chỉ mang id. Nội dung đã bị xoá, và gửi lại cả tin nhắn chỉ để nói "nó không
còn nữa" là gửi đúng thứ vừa phải giấu đi.

### `chat.typing`

```json
{
  "type": "chat.typing",
  "payload": {
    "conversation_id": "uuid",
    "employee_id": "uuid",
    "employee_name": "Trần Văn A"
  }
}
```

**Không** gửi lại cho chính người đang gõ.

Client nên tự xoá chỉ báo sau 4 giây. Server gửi mỗi khi nhận được, không gửi
bản tin "đã ngừng gõ" — quá hai chu kỳ tiết chế mà im nghĩa là họ đã ngừng.
Không có hạn đó thì chữ "đang nhập" đứng lại vĩnh viễn khi ai đó đóng tab giữa
chừng.

### `chat.read`

```json
{
  "type": "chat.read",
  "payload": {
    "conversation_id": "uuid",
    "employee_id": "uuid",
    "message_id": "uuid"
  }
}
```

Gửi cho **cả** người vừa đọc: họ có thể đang mở nhiều tab, và tab kia cần biết
để hạ số chưa đọc mà không phải hỏi lại server.

### `chat.badge`

```json
{ "type": "chat.badge", "payload": { "unread": 5 } }
```

Tổng số tin chưa đọc trên **mọi** hội thoại.

### `presence.changed`

```json
{
  "type": "presence.changed",
  "payload": {
    "employee_id": "uuid",
    "status": "online",
    "last_seen_at": "2026-09-22T10:30:00+07:00"
  }
}
```

`status` có ba giá trị, không phải hai:

| Giá trị | Nghĩa |
|---|---|
| `online` | Đang kết nối **và** có hoạt động |
| `idle` | Đang kết nối nhưng không hoạt động |
| `offline` | Không có kết nối nào |

Gộp `idle` vào `online` sẽ hiện chấm xanh cho người đã bỏ máy đó cả buổi.

### `task.updated`

Công việc vừa đổi. Dành cho bảng Kanban đang mở cập nhật mà không phải tải lại.

## Định tuyến giữa nhiều instance

Client nối vào **một** instance api, nhưng bản tin có thể sinh ra ở instance
khác — hoặc ở worker, nơi không có kết nối WebSocket nào.

```
        api-1                    api-2                   worker
          │                        │                        │
          └────────┬───────────────┴────────────┬───────────┘
                   ▼                            ▼
          exchange fanout  manage.realtime  (RabbitMQ)
                   │
        ┌──────────┴──────────┐
        ▼                     ▼
   queue của api-1       queue của api-2
   (exclusive,           (exclusive,
    auto-delete)          auto-delete)
        │                     │
        ▼                     ▼
   lọc theo người nhận   lọc theo người nhận
```

Mỗi instance có một queue **riêng, không tên, auto-delete** — nó tự biến mất
khi instance ngắt kết nối. Đây là điểm khác biệt cốt lõi so với queue job:
job dùng **chung** một queue để chia việc (mỗi message một worker xử lý), còn
realtime cần **mọi** instance đều nhận được **bản sao** của cùng một bản tin.

Việc giao bản tin dùng **fan-out rồi lọc tại chỗ**, không định tuyến theo bản
đồ `user → instance`. Bản đồ đó vẫn tồn tại (trường `instance_id` trong bản
ghi presence trên Redis) nhưng chỉ dùng để chẩn đoán: định tuyến theo nó cần
bản đồ luôn đúng, và một instance bị `kill -9` sẽ để lại bản đồ cũ trỏ tới nơi
không còn ai — **bản tin biến mất trong im lặng**.

Bản tin realtime dùng `DeliveryMode: Transient` và `autoAck`, ngược với job
nghiệp vụ. Nó chỉ có giá trị ngay lúc đó: nếu RabbitMQ khởi động lại, thứ
người dùng cần không phải thông báo cũ được phát lại, mà là client tự nối lại
và tải trạng thái mới.

## Hành vi client

`frontend/src/lib/ws/client.ts` là bản hiện thực tham chiếu.

| Việc | Cách làm |
|---|---|
| Nối lại | Backoff tăng dần, có jitter, chặn trên ở 30 giây. Jitter để 200 client không nối lại cùng một lúc sau khi api khởi động lại |
| Xếp hàng khi mất mạng | `send()` trả `false` khi kết nối chưa mở; nơi gọi rơi sang REST với cùng `client_message_id` |
| Nhịp tim | Mỗi `heartbeat_interval` giây, `is_active` tính từ `document.visibilityState` và thời gian không thao tác |
| Đồng bộ nhiều tab | `BroadcastChannel` cho trạng thái đã đọc. Mỗi tab có kết nối riêng, nhưng tab ẩn có thể bị trình duyệt ngắt |
| Chống trùng | Khớp theo `id`, rồi theo `client_message_id` cho tin lạc quan của chính mình |

## Kiểm chứng

Những thứ chỉ kiểm được bằng một kết nối WebSocket **thật**, không kiểm được
bằng script HTTP:

- fan-out giữa nhiều instance
- hình dạng payload đẩy xuống (xem lỗi json tag ở trên)
- chỉ báo "đang nhập" tới đúng người
- chống trùng theo `client_message_id`

Cách đã dùng ở Phase 5: dựng thêm một container api thứ hai, nối client vào
instance 2, phát bản tin qua instance 1.

```bash
docker compose run -d --rm --name manage-api-extra -p 8090:8080 api
docker logs manage-api-extra | grep instance_id
```
