//go:build integration

// Integration test cho tầng repository, chạy trên PostgreSQL THẬT.
//
// Chạy:
//
//	go test -tags=integration ./internal/repository/postgres/...
//
// # VÌ SAO CẦN DATABASE THẬT
//
// Tầng repository gần như chỉ có SQL. Một bản giả lập của nó sẽ kiểm thử
// chính bản giả lập đó, không kiểm được thứ duy nhất đáng nghi: câu SQL.
// Những lỗi sống ở tầng này không có cách nào bắt bằng unit test —
// ràng buộc khoá ngoại, ON DELETE, truy vấn đệ quy, chỉ mục UNIQUE một
// phần, kiểu dữ liệu không khớp, và cách COALESCE che mất NULL.
//
// # VÌ SAO CÓ BUILD TAG
//
// `go test ./...` phải chạy được trên máy không có Docker và phải xong trong
// vài giây. Bộ này cần kéo image và dựng container, mất hàng chục giây.
// Tách bằng build tag để hai nhu cầu đó không giẫm lên nhau: CI chạy unit
// test ở mọi commit, integration test ở nhánh chính.
//
// # CÁCH CÔ LẬP GIỮA CÁC PHÉP THỬ
//
// Một container cho CẢ gói, dựng một lần trong TestMain. Trước mỗi phép thử,
// `reset` cắt sạch các bảng nghiệp vụ nhưng GIỮ nguyên dữ liệu tham chiếu
// do migration seed (vai trò, quyền, khung giờ mặc định, tham số lương).
//
// Không dùng "chạy trong giao dịch rồi rollback": repository nhận một pool
// chứ không nhận giao dịch, nên cách đó buộc phải viết lại SQL trong phép
// thử — tức là kiểm thử một bản sao của mã, không phải chính mã.
//
// Dựng container cho từng phép thử thì sạch hơn nữa nhưng mất vài chục giây
// mỗi lần, và một bộ kiểm thử chạy lâu là một bộ kiểm thử không ai chạy.
//
// Các phép thử trong gói này KHÔNG chạy song song được: chúng dùng chung một
// database và `reset` cắt bảng của nhau. Đừng thêm t.Parallel().
package postgres

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/PhamVanPhuc2k2/manage/pkg/postgres"
)

// testPool là pool dùng chung cho cả gói. Chỉ TestMain ghi vào nó.
var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	ctx := context.Background()

	container, err := tcpostgres.Run(ctx,
		// Cùng phiên bản với docker-compose.yml. Kiểm thử trên một phiên bản
		// khác với production là tự tạo ra một lớp khác biệt vô hình.
		"postgres:16-alpine",
		tcpostgres.WithDatabase("manage_test"),
		tcpostgres.WithUsername("manage"),
		tcpostgres.WithPassword("manage"),
		// CỐ Ý KHÔNG nạp docker/postgres/init/01-extensions.sql.
		//
		// Container này dựng một database trống rỗi chỉ chạy migration, giống
		// hệt mọi môi trường mới khác: dịch vụ postgres của GitHub Actions,
		// một database mới trên cùng máy chủ, hay một lần khôi phục vào chỗ
		// khác. Nếu migration không tự tạo đủ extension thì bộ này phải hỏng
		// ngay — đó là điểm của nó.
		testcontainers.WithWaitStrategy(
			// Chờ ĐÚNG HAI LẦN dòng "ready to accept connections".
			//
			// PostgreSQL in dòng đó một lần khi khởi tạo cụm dữ liệu rồi tắt
			// đi, và một lần nữa khi thật sự sẵn sàng. Chờ một lần là nối vào
			// đúng lúc nó sắp tắt, và lỗi đó chỉ xuất hiện lúc máy chạy chậm.
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(2*time.Minute),
		),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, "không dựng được container postgres:", err)
		os.Exit(1)
	}

	code := run(ctx, container, m)

	if err := testcontainers.TerminateContainer(container); err != nil {
		fmt.Fprintln(os.Stderr, "không dọn được container:", err)
	}
	os.Exit(code)
}

// run tách khỏi TestMain để mọi đường thoát đều đi qua cùng một chỗ dọn dẹp.
// os.Exit trong TestMain bỏ qua defer, nên một container bị bỏ lại sẽ nằm đó
// tới khi có người dọn tay.
func run(ctx context.Context, c *tcpostgres.PostgresContainer, m *testing.M) int {
	dsn, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		fmt.Fprintln(os.Stderr, "không lấy được DSN:", err)
		return 1
	}

	db, err := postgres.New(ctx, dsn, 8, zerolog.Nop())
	if err != nil {
		fmt.Fprintln(os.Stderr, "không kết nối được:", err)
		return 1
	}
	defer db.Close()
	testPool = db.Pool

	if err := applyMigrations(ctx, db.Pool); err != nil {
		fmt.Fprintln(os.Stderr, "chạy migration thất bại:", err)
		return 1
	}

	return m.Run()
}

// applyMigrations chạy các tệp .up.sql theo đúng thứ tự tên.
//
// Chạy CHÍNH các tệp migration của dự án chứ không dựng schema riêng cho
// kiểm thử. Một schema riêng sẽ trôi khỏi schema thật, và khi đó bộ kiểm thử
// vừa mất ý nghĩa vừa không ai nhận ra.
func applyMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	dir := filepath.Join("..", "..", "..", "migrations")

	files, err := filepath.Glob(filepath.Join(dir, "*.up.sql"))
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("không tìm thấy migration nào trong %s", dir)
	}
	// Glob trả về đã sắp theo tên, mà tên có tiền tố số thứ tự — đúng thứ tự
	// cần chạy.

	for _, f := range files {
		sql, err := os.ReadFile(f)
		if err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(f), err)
		}
		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(f), err)
		}
	}
	return nil
}

// seededTables là những bảng do migration đổ sẵn dữ liệu tham chiếu.
//
// Cắt chúng đi thì vai trò và quyền biến mất, và mọi phép thử về phân
// quyền sau đó sẽ đạt vì lý do sai: không ai có quyền gì cả.
var seededTables = map[string]bool{
	"roles":             true,
	"permissions":       true,
	"role_permissions":  true,
	"payroll_settings":  true,
	"tax_brackets":      true,
	"work_schedules":    true,
	"system_info":       true,
	"schema_migrations": true,
}

// reset đưa database về trạng thái ngay sau migration.
//
// Gọi ở ĐẦU mỗi phép thử chứ không ở cuối: phép thử hỏng giữa chừng thì
// bước dọn ở cuối không chạy, và phép thử kế tiếp hỏng theo vì dữ liệu
// còn sót — che mất lỗi thật sau một lỗi giả.
func reset(t *testing.T) {
	t.Helper()
	ctx := context.Background()

	rows, err := testPool.Query(ctx, `
		SELECT tablename FROM pg_tables WHERE schemaname = 'public'`)
	if err != nil {
		t.Fatalf("không liệt kê được bảng: %v", err)
	}

	var targets []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			t.Fatalf("đọc tên bảng: %v", err)
		}
		if !seededTables[name] {
			targets = append(targets, pgx.Identifier{name}.Sanitize())
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("liệt kê bảng: %v", err)
	}
	if len(targets) == 0 {
		return
	}

	// MỘT lệnh TRUNCATE cho tất cả: cắt từng bảng một sẽ vướng khoá ngoại trừ
	// khi đoán đúng thứ tự, mà thứ tự đó đổi mỗi lần thêm bảng mới.
	// CASCADE lo nốt phần còn lại.
	_, err = testPool.Exec(ctx,
		"TRUNCATE TABLE "+strings.Join(targets, ", ")+" RESTART IDENTITY CASCADE")
	if err != nil {
		t.Fatalf("không cắt được bảng: %v", err)
	}
}

// testDB trả về pool đã được đưa về trạng thái sạch, dạng mà repository nhận.
func testDB(t *testing.T) *postgres.DB {
	t.Helper()
	reset(t)
	return &postgres.DB{Pool: testPool}
}
