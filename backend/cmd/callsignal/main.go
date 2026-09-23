// Binary callsignal: kiểm chứng đường signaling của cuộc gọi qua WebSocket.
//
// Chạy:
//
//	go run ./cmd/callsignal -url ws://localhost:8088/ws \
//	    -api http://localhost:8088/api/v1 \
//	    -caller <token_A> -callee <token_B> -conversation <id>
//
// # VẤN ĐỀ NÓ KIỂM
//
// Cuộc gọi chạy được hay không phụ thuộc vào hai thứ mà REST không chạm
// tới: bản tin đổ chuông có tới thiết bị người nhận không, và SDP/ICE có
// được chuyển đúng người không.
//
// Không có bộ kiểm này thì lỗi ở đường signaling chỉ lộ ra khi có người
// thật bấm gọi, và triệu chứng là "màn hình đen, không ai nghe thấy gì" —
// mô tả đúng như nhau cho mười nguyên nhân khác nhau.
//
// # PHÉP THỬ QUAN TRỌNG NHẤT
//
// Bản tin signaling mang trường `from`. Nếu máy chủ tin vào giá trị client
// gửi lên, bất kỳ ai cũng mạo danh được người khác giữa lúc thương lượng
// kết nối. Công cụ này cố ý gửi một `from` GIẢ và kiểm tra rằng bên nhận
// thấy danh tính THẬT của người gửi.
//
// Vì sao là binary riêng chứ không phải hàm test: cùng lý do với wsfanout —
// thứ cần kiểm là hệ thống đang chạy thật, gồm cả nginx và hub trong tiến
// trình api, không phải một bản dựng trong bộ nhớ.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type envelope struct {
	Type string `json:"type"`
	// Tên trường là "payload", không phải "data" — khác với vỏ bọc của REST.
	Payload json.RawMessage `json:"payload"`
}

type peer struct {
	name  string
	token string
	conn  *websocket.Conn
	in    chan envelope
}

var (
	failures int
	verbose  bool
)

func check(name string, ok bool, detail string) {
	if ok {
		fmt.Printf("  \033[32m✓\033[0m %s\n", name)
		return
	}
	failures++
	fmt.Printf("  \033[31m✗\033[0m %s — %s\n", name, detail)
}

func main() {
	var (
		wsURL   = flag.String("url", "ws://localhost:8088/ws", "địa chỉ WebSocket")
		apiURL  = flag.String("api", "http://localhost:8088/api/v1", "gốc REST")
		caller  = flag.String("caller", "", "access token của người gọi")
		callee  = flag.String("callee", "", "access token của người nhận")
		convID  = flag.String("conversation", "", "id hội thoại 1-1 giữa hai người")
		timeout = flag.Duration("timeout", 15*time.Second, "hạn chờ mỗi bản tin")
	)
	flag.BoolVar(&verbose, "v", false, "in mọi bản tin nhận được")
	flag.Parse()

	if *caller == "" || *callee == "" || *convID == "" {
		fmt.Fprintln(os.Stderr, "thiếu -caller, -callee hoặc -conversation")
		os.Exit(2)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := connect(ctx, "người gọi", *wsURL, *caller)
	b := connect(ctx, "người nhận", *wsURL, *callee)
	defer a.conn.Close()
	defer b.conn.Close()

	// Chờ một nhịp cho cả hai kết nối đăng ký xong vào hub. Không chờ thì
	// bản tin đổ chuông có thể phát ra trước lúc người nhận có mặt, và
	// phép thử hỏng vì lý do không liên quan tới thứ đang kiểm.
	time.Sleep(500 * time.Millisecond)

	fmt.Println("\n── Đổ chuông ──")

	start, err := post(*apiURL+"/calls", *caller, map[string]string{
		"conversation_id": *convID,
		"kind":            "video",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "không mở được cuộc gọi: %v\n", err)
		os.Exit(1)
	}
	callID := dig(start, "data", "call", "id")
	fmt.Printf("  cuộc gọi %s\n", callID)

	incoming := wait(b, "call.incoming", *timeout)
	check("Người nhận nhận được call.incoming", incoming != nil,
		"không có bản tin nào tới trong hạn chờ")

	if incoming != nil {
		var p struct {
			CallID    string `json:"call_id"`
			ExpiresAt string `json:"expires_at"`
			Initiator string `json:"initiator_name"`
		}
		_ = json.Unmarshal(incoming.Payload, &p)
		check("Bản tin đổ chuông mang đúng id cuộc gọi",
			p.CallID == callID, "được "+p.CallID)
		// Thiếu hạn thì client không tự tắt chuông được, và một cái chuông
		// không chịu tắt là thứ người dùng nhớ rất lâu.
		check("Bản tin đổ chuông có mốc hết hạn", p.ExpiresAt != "", "trống")
		check("Bản tin đổ chuông có tên người gọi", p.Initiator != "", "trống")
	}

	ringing := wait(a, "call.ringing", *timeout)
	check("Người gọi nhận được call.ringing", ringing != nil, "không có bản tin")

	fmt.Println("\n── Bắt máy ──")

	if _, err := post(*apiURL+"/calls/"+callID+"/accept", *callee, nil); err != nil {
		fmt.Fprintf(os.Stderr, "không bắt máy được: %v\n", err)
		os.Exit(1)
	}

	accepted := wait(a, "call.accepted", *timeout)
	check("Người gọi nhận được call.accepted", accepted != nil, "không có bản tin")

	// Người bắt máy cũng phải nhận call.cancelled, để các THIẾT BỊ KHÁC
	// của chính họ ngừng đổ chuông. Đây là phần hay bị quên nhất: không có
	// nó thì điện thoại vẫn reo sau khi đã bắt máy trên máy tính.
	cancelled := wait(b, "call.cancelled", 3*time.Second)
	check("Người bắt máy nhận call.cancelled cho thiết bị khác",
		cancelled != nil, "không có bản tin")

	fmt.Println("\n── Chuyển tiếp SDP và ICE ──")

	callerID := meID(*apiURL, *caller)
	calleeID := meID(*apiURL, *callee)

	// Gửi kèm một `from` GIẢ. Máy chủ phải ghi đè bằng danh tính thật.
	sdp := map[string]any{
		"call_id": callID,
		"to":      calleeID,
		"from":    uuid.New().String(),
		"data":    json.RawMessage(`{"type":"offer","sdp":"v=0 gia-lap"}`),
	}
	send(a, "call.sdp", sdp)

	got := wait(b, "call.sdp", *timeout)
	check("Người nhận nhận được call.sdp", got != nil, "không có bản tin")

	if got != nil {
		var p struct {
			CallID string          `json:"call_id"`
			From   string          `json:"from"`
			Data   json.RawMessage `json:"data"`
		}
		_ = json.Unmarshal(got.Payload, &p)

		check("Máy chủ GHI ĐÈ trường from bằng danh tính thật",
			p.From == callerID,
			fmt.Sprintf("được %s, muốn %s", p.From, callerID))
		check("Nội dung SDP đi qua nguyên vẹn",
			bytes.Contains(p.Data, []byte("gia-lap")), string(p.Data))
		check("Bản tin mang đúng id cuộc gọi", p.CallID == callID, p.CallID)
	}

	// Chiều ngược lại: ICE candidate từ người nhận về người gọi.
	send(b, "call.ice", map[string]any{
		"call_id": callID,
		"to":      callerID,
		"data":    json.RawMessage(`{"candidate":"candidate:1 1 udp 2130706431 10.0.0.1 54321 typ host"}`),
	})
	ice := wait(a, "call.ice", *timeout)
	check("ICE candidate đi được chiều ngược lại", ice != nil, "không có bản tin")

	// Gửi cho người KHÔNG ở trong hội thoại phải bị từ chối. Thiếu chốt này
	// thì một thành viên hợp lệ vẫn bắn được bản tin tới người ngoài cuộc.
	send(a, "call.sdp", map[string]any{
		"call_id": callID,
		"to":      uuid.New().String(),
		"data":    json.RawMessage(`{"type":"offer"}`),
	})
	errEnv := wait(a, "error", 5*time.Second)
	check("Gửi tới người ngoài hội thoại bị từ chối",
		errEnv != nil, "máy chủ im lặng chấp nhận")

	fmt.Println("\n── Kết thúc ──")

	if _, err := post(*apiURL+"/calls/"+callID+"/end", *caller, nil); err != nil {
		fmt.Fprintf(os.Stderr, "không kết thúc được: %v\n", err)
	}

	ended := wait(b, "call.ended", *timeout)
	check("Người nhận nhận được call.ended", ended != nil, "không có bản tin")

	fmt.Println()
	if failures > 0 {
		fmt.Printf("\033[31m%d phép thử hỏng\033[0m\n", failures)
		os.Exit(1)
	}
	fmt.Println("\033[32mTất cả phép thử signaling đều đạt\033[0m")
}

/* ------------------------------------------------------------------ *
 * WebSocket
 * ------------------------------------------------------------------ */

func connect(ctx context.Context, name, wsURL, token string) *peer {
	d := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, res, err := d.DialContext(ctx, wsURL+"?token="+token, nil)
	if err != nil {
		status := ""
		if res != nil {
			status = fmt.Sprintf(" (HTTP %d)", res.StatusCode)
		}
		fmt.Fprintf(os.Stderr, "%s không nối được WebSocket%s: %v\n", name, status, err)
		os.Exit(1)
	}

	p := &peer{name: name, token: token, conn: conn, in: make(chan envelope, 64)}

	go func() {
		defer close(p.in)
		for {
			var e envelope
			if err := conn.ReadJSON(&e); err != nil {
				return
			}
			if verbose {
				fmt.Printf("    [%s] %s %s\n", name, e.Type, e.Payload)
			}
			select {
			case p.in <- e:
			case <-ctx.Done():
				return
			}
		}
	}()

	return p
}

// wait chờ một loại bản tin, BỎ QUA mọi loại khác.
//
// Bỏ qua chứ không báo hỏng: kết nối này cũng nhận nhịp tim, thông báo và
// bản tin chat, và một phép thử signaling không nên hỏng vì có người vừa
// nhắn tin.
func wait(p *peer, typ string, d time.Duration) *envelope {
	deadline := time.After(d)
	for {
		select {
		case e, ok := <-p.in:
			if !ok {
				return nil
			}
			if e.Type == typ {
				return &e
			}
		case <-deadline:
			return nil
		}
	}
}

func send(p *peer, typ string, payload any) {
	msg := map[string]any{
		"type":    typ,
		"payload": payload,
		"ts":      time.Now().UTC().Format(time.RFC3339),
	}
	if err := p.conn.WriteJSON(msg); err != nil {
		fmt.Fprintf(os.Stderr, "%s không gửi được %s: %v\n", p.name, typ, err)
	}
}

/* ------------------------------------------------------------------ *
 * REST
 * ------------------------------------------------------------------ */

func post(url, token string, body any) (map[string]any, error) {
	var buf io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		buf = bytes.NewReader(b)
	}

	req, err := http.NewRequest(http.MethodPost, url, buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", res.StatusCode, raw)
	}

	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out, nil
}

func meID(apiURL, token string) string {
	req, _ := http.NewRequest(http.MethodGet, apiURL+"/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer res.Body.Close()

	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return dig(out, "data", "employee_id")
}

// dig lấy một chuỗi lồng sâu trong map, trả về "" nếu đường dẫn hỏng.
func dig(m map[string]any, path ...string) string {
	cur := any(m)
	for _, k := range path {
		obj, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = obj[k]
	}
	s, _ := cur.(string)
	return s
}
