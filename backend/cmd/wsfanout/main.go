// Binary wsfanout: kiểm chứng fan-out realtime qua NHIỀU bản api.
//
// Chạy:
//
//	go run ./cmd/wsfanout -url ws://localhost:8088/ws \
//	    -api http://localhost:8088/api/v1 -token <access_token> -n 3
//
// # VẤN ĐỀ NÓ KIỂM
//
// Hub WebSocket nằm trong bộ nhớ của từng tiến trình api. Khi chạy nhiều bản,
// người gửi và người nhận gần như chắc chắn nối vào hai bản KHÁC NHAU, nên
// tin nhắn phải đi vòng qua exchange fanout của RabbitMQ mới tới được người
// nhận. Nếu đường vòng đó hỏng, hệ thống vẫn trông bình thường với một bản
// api và chỉ im lặng mất tin khi scale lên — đúng lúc không ai còn nhìn.
//
// # CÁCH KIỂM
//
// Mở N kết nối WebSocket. nginx luân phiên nên chúng rải ra các bản api khác
// nhau. Sau đó gửi MỘT tin nhắn bằng REST, rồi đếm xem bao nhiêu kết nối nhận
// được nó. Tất cả cùng một tài khoản, nên mọi kết nối đều là thành viên hội
// thoại và đều phải nhận.
//
// Kết quả N/N nghĩa là fan-out chạy đúng. Kết quả 1/N nghĩa là chỉ bản api
// trực tiếp xử lý lời gọi REST đẩy được tin đi — tức RabbitMQ không nối các
// bản lại với nhau.
//
// Vì sao là binary riêng chứ không phải hàm test: cùng lý do với wsload — thứ
// cần đo là hệ thống đang chạy thật, gồm cả nginx, RabbitMQ và nhiều tiến
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
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// envelope là khung bản tin realtime. Chỉ đọc phần cần dùng: công cụ này
// không nên hỏng mỗi khi payload thêm trường mới.
type envelope struct {
	Type string `json:"type"`
	// Tên trường là "payload", không phải "data" — khác với vỏ bọc của REST.
	// Xem domain/realtime.Envelope.
	Payload json.RawMessage `json:"payload"`
}

type listener struct {
	idx  int
	conn *websocket.Conn
	got  atomic.Bool
}

func main() {
	var (
		wsURL   = flag.String("url", "ws://localhost:8088/ws", "địa chỉ WebSocket")
		apiURL  = flag.String("api", "http://localhost:8088/api/v1", "gốc REST API")
		token   = flag.String("token", "", "access token (bắt buộc)")
		n       = flag.Int("n", 3, "số kết nối, nên bằng số bản api")
		convID  = flag.String("conv", "", "id hội thoại; bỏ trống thì tự mở hội thoại với chính mình bị từ chối, xem -peer")
		peerID  = flag.String("peer", "", "id nhân viên để mở hội thoại 1-1 nếu chưa có -conv")
		timeout = flag.Duration("timeout", 15*time.Second, "thời gian chờ tin nhắn tới")
	)
	flag.Parse()

	if *token == "" {
		fmt.Fprintln(os.Stderr, "thiếu -token")
		os.Exit(2)
	}
	if *convID == "" && *peerID == "" {
		fmt.Fprintln(os.Stderr, "cần -conv <id hội thoại> hoặc -peer <id nhân viên>")
		os.Exit(2)
	}

	client := &http.Client{Timeout: 20 * time.Second}

	conversation := *convID
	if conversation == "" {
		id, err := openDirect(client, *apiURL, *token, *peerID)
		if err != nil {
			fmt.Fprintln(os.Stderr, "không mở được hội thoại:", err)
			os.Exit(1)
		}
		conversation = id
		fmt.Printf("Đã mở hội thoại %s\n", conversation)
	}

	fmt.Printf("Mở %d kết nối tới %s\n", *n, *wsURL)

	listeners := make([]*listener, 0, *n)
	for i := 0; i < *n; i++ {
		conn, _, err := websocket.DefaultDialer.Dial(*wsURL+"?token="+*token, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "kết nối %d hỏng: %v\n", i+1, err)
			os.Exit(1)
		}
		defer conn.Close()
		listeners = append(listeners, &listener{idx: i + 1, conn: conn})
	}

	// Mốc nhận diện duy nhất, để không nhầm với tin nhắn của lần chạy trước
	// còn sót trong hội thoại.
	marker := "wsfanout " + uuid.NewString()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	var wg sync.WaitGroup
	for _, l := range listeners {
		wg.Add(1)
		go func(l *listener) {
			defer wg.Done()
			waitFor(ctx, l, marker)
		}(l)
	}

	// Chờ một nhịp để mọi kết nối vào đủ hub trước khi gửi. Gửi ngay lập tức
	// sẽ đo nhầm một cuộc đua khởi động thành lỗi fan-out.
	time.Sleep(500 * time.Millisecond)

	if err := sendMessage(client, *apiURL, *token, conversation, marker); err != nil {
		fmt.Fprintln(os.Stderr, "không gửi được tin nhắn:", err)
		os.Exit(1)
	}
	fmt.Println("Đã gửi tin nhắn, đang chờ fan-out...")

	wg.Wait()

	received := 0
	var missing []int
	for _, l := range listeners {
		if l.got.Load() {
			received++
		} else {
			missing = append(missing, l.idx)
		}
	}

	fmt.Println()
	fmt.Println("══════════════════════════════════════")
	fmt.Printf("  Kết nối nhận được tin : %d / %d\n", received, len(listeners))
	if len(missing) > 0 {
		fmt.Printf("  Không nhận được       : %v\n", missing)
	}
	fmt.Println("══════════════════════════════════════")

	if received != len(listeners) {
		fmt.Fprintln(os.Stderr,
			"\nFAN-OUT HỎNG: có kết nối không nhận được tin nhắn.")
		fmt.Fprintln(os.Stderr,
			"Kiểm tra exchange 'manage.realtime' trên RabbitMQ và log của từng bản api.")
		os.Exit(1)
	}
	fmt.Println("\nFan-out đúng trên mọi kết nối.")
}

// waitFor đọc tới khi thấy bản tin chat mang đúng mốc, hoặc hết giờ.
func waitFor(ctx context.Context, l *listener, marker string) {
	deadline, _ := ctx.Deadline()
	_ = l.conn.SetReadDeadline(deadline)

	for {
		_, raw, err := l.conn.ReadMessage()
		if err != nil {
			return
		}

		var e envelope
		if json.Unmarshal(raw, &e) != nil {
			continue
		}
		if e.Type != "chat.message" {
			continue
		}
		// So khớp trên chuỗi thô: đủ để nhận diện và không phụ thuộc vào hình
		// dạng payload, vốn có thể đổi.
		if bytes.Contains(e.Payload, []byte(marker)) {
			l.got.Store(true)
			return
		}
	}
}

func openDirect(c *http.Client, api, token, peer string) (string, error) {
	// Một endpoint duy nhất cho cả 1-1 lẫn nhóm; `kind` quyết định kiểu.
	body, err := post(c, api+"/chat/conversations", token,
		map[string]string{"kind": "direct", "peer_id": peer})
	if err != nil {
		return "", err
	}

	var out struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	if out.Data.ID == "" {
		return "", fmt.Errorf("phản hồi không có id hội thoại: %s", body)
	}
	return out.Data.ID, nil
}

func sendMessage(c *http.Client, api, token, conv, content string) error {
	_, err := post(c, api+"/chat/conversations/"+conv+"/messages", token,
		map[string]string{
			"content":           content,
			"client_message_id": uuid.NewString(),
		})
	return err
}

func post(c *http.Client, url, token string, payload any) ([]byte, error) {
	buf, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	res, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("%s trả %d: %s", url, res.StatusCode, body)
	}
	return body, nil
}
