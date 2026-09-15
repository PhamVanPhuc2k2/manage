# dev.ps1 — bản PowerShell của Makefile, dùng trên Windows khi chưa có `make`.
#
# Cách dùng:   .\dev.ps1 <lệnh> [tham số]
# Xem lệnh:    .\dev.ps1 help
#
# Makefile vẫn giữ nguyên để dùng trong WSL, CI, Linux và macOS.
# Hai file phải luôn khớp nhau về danh sách lệnh.

param(
    [Parameter(Position = 0)]
    [string]$Command = "help",

    [Parameter(Position = 1, ValueFromRemainingArguments = $true)]
    [string[]]$Rest
)

$ErrorActionPreference = "Stop"
Set-Location -Path $PSScriptRoot

# ---------------------------------------------------------------- tiện ích

function Read-DotEnv {
    $vars = @{}
    if (-not (Test-Path ".env")) { return $vars }
    foreach ($line in Get-Content ".env") {
        $t = $line.Trim()
        if ($t -eq "" -or $t.StartsWith("#")) { continue }
        $i = $t.IndexOf("=")
        if ($i -lt 1) { continue }
        $vars[$t.Substring(0, $i).Trim()] = $t.Substring($i + 1).Trim()
    }
    return $vars
}

function Get-EnvValue([hashtable]$vars, [string]$key, [string]$fallback) {
    if ($vars.ContainsKey($key) -and $vars[$key] -ne "") { return $vars[$key] }
    return $fallback
}

function Get-Arg([string]$name) {
    # Hỗ trợ cả hai kiểu:  .\dev.ps1 logs api    và    .\dev.ps1 logs s=api
    if (-not $Rest -or $Rest.Count -eq 0) { return "" }
    foreach ($a in $Rest) {
        if ($a -like "$name=*") { return $a.Substring($name.Length + 1) }
    }
    if ($Rest[0] -notlike "*=*") { return $Rest[0] }
    return ""
}

function Invoke-Compose { docker compose @args }

$env:MSYS_NO_PATHCONV = "1"
$dotenv = Read-DotEnv
$publicUrl = Get-EnvValue $dotenv "PUBLIC_BASE_URL" "http://localhost"
$apiPort = Get-EnvValue $dotenv "API_PORT" "8080"
$pgUser = Get-EnvValue $dotenv "POSTGRES_USER" "manage"
$pgDb = Get-EnvValue $dotenv "POSTGRES_DB" "manage"
$redisPass = Get-EnvValue $dotenv "REDIS_PASSWORD" ""

# ----------------------------------------------------------------- lệnh

switch ($Command) {

    "help" {
        @"
Lệnh có sẵn:

  MÔI TRƯỜNG
    init                 Thiết lập lần đầu: tạo .env, build image, khởi động
    up                   Khởi động toàn bộ (dev)
    down                 Dừng toàn bộ, GIỮ NGUYÊN dữ liệu
    destroy              Dừng và XOÁ SẠCH dữ liệu (hỏi xác nhận)
    rebuild <svc>        Build lại image của một service
    restart <svc>        Khởi động lại một service
    logs [svc]           Xem log (bỏ trống để xem tất cả)
    ps                   Trạng thái container
    sh <svc>             Vào shell trong container

  MIGRATION
    migrate              Chạy migration
    migrate-down         Lùi 1 bước migration
    migrate-create <tên> Tạo cặp file migration mới

  CHẤT LƯỢNG
    lint                 Kiểm tra code Go (golangci-lint)
    tidy                 Dọn go.mod
    fmt                  Format code Go
    test                 Chạy go test

  TIỆN ÍCH
    psql                 Mở psql
    redis-cli            Mở redis-cli
    queues               Xem hàng đợi RabbitMQ
    smoke                Kiểm tra nhanh toàn hệ thống
    urls                 In các địa chỉ truy cập

  PRODUCTION
    build-images         Build image production tại máy
    prod-up              Khởi động production

Ví dụ:
    .\dev.ps1 logs worker
    .\dev.ps1 migrate-create create_users
"@
    }

    "init" {
        if (-not (Test-Path ".env")) {
            Copy-Item ".env.example" ".env"
            Write-Host "Đã tạo .env từ .env.example" -ForegroundColor Green
        }
        Invoke-Compose build
        if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
        Invoke-Compose up -d
        if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
        Write-Host ""
        Write-Host "Xong. Kiểm tra bằng: .\dev.ps1 smoke" -ForegroundColor Green
        & $PSCommandPath urls
    }

    "up" { Invoke-Compose up -d }

    "down" { Invoke-Compose down }

    "destroy" {
        $ans = Read-Host "Xoá toàn bộ volume, MẤT HẾT dữ liệu. Gõ 'yes' để xác nhận"
        if ($ans -eq "yes") { Invoke-Compose down -v } else { Write-Host "Đã huỷ" }
    }

    "rebuild" {
        $s = Get-Arg "s"
        if ($s -eq "") { Write-Host "Thiếu tên service. Ví dụ: .\dev.ps1 rebuild api" -ForegroundColor Red; exit 1 }
        Invoke-Compose build $s
        if ($LASTEXITCODE -eq 0) { Invoke-Compose up -d $s }
    }

    "restart" {
        $s = Get-Arg "s"
        if ($s -eq "") { Write-Host "Thiếu tên service. Ví dụ: .\dev.ps1 restart api" -ForegroundColor Red; exit 1 }
        Invoke-Compose restart $s
    }

    "logs" {
        $s = Get-Arg "s"
        if ($s -eq "") { Invoke-Compose logs -f --tail=100 }
        else { Invoke-Compose logs -f --tail=100 $s }
    }

    "ps" { Invoke-Compose ps }

    "sh" {
        $s = Get-Arg "s"
        if ($s -eq "") { Write-Host "Thiếu tên service. Ví dụ: .\dev.ps1 sh api" -ForegroundColor Red; exit 1 }
        Invoke-Compose exec $s sh
    }

    "migrate" { Invoke-Compose run --rm migrate }

    "migrate-down" {
        $dsn = "postgres://$pgUser`:$(Get-EnvValue $dotenv 'POSTGRES_PASSWORD' '')@postgres:5432/$pgDb`?sslmode=disable"
        Invoke-Compose run --rm migrate "-path=/migrations" "-database=$dsn" down 1
    }

    "migrate-create" {
        $n = Get-Arg "n"
        if ($n -eq "") { Write-Host "Thiếu tên migration. Ví dụ: .\dev.ps1 migrate-create create_users" -ForegroundColor Red; exit 1 }
        Invoke-Compose run --rm --entrypoint migrate migrate create -ext sql -dir /migrations -seq $n
    }

    "lint" {
        docker run --rm -v "${PSScriptRoot}\backend:/app" -w /app `
            golangci/golangci-lint:v2.13.2 golangci-lint run ./... --timeout 5m
    }

    "tidy" { Invoke-Compose exec -T api sh -c "cd /app && go mod tidy" }

    "fmt" { Invoke-Compose exec -T api sh -c "cd /app && gofmt -l -w ." }

    "test" { Invoke-Compose exec -T api sh -c "cd /app && go test ./... -count=1" }

    "fe-lint" { Invoke-Compose exec -T frontend sh -c "pnpm lint" }

    "fe-build" {
        # BẮT BUỘC đặt NODE_ENV=production. Container dev chạy với
        # NODE_ENV=development, và build production trong môi trường đó khiến
        # React nạp nhầm bundle — lỗi hiện ra rất khó hiểu:
        #   Error occurred prerendering page "/_global-error"
        #   TypeError: Cannot read properties of null (reading 'useContext')
        Invoke-Compose exec -T -e NODE_ENV=production frontend sh -c "pnpm build"
    }

    "smoke-auth" {
        # Chạy bảng kiểm chứng bảo mật Phase 1.
        if (-not $env:ADMIN_PASS) {
            Write-Host "Đặt mật khẩu admin trước: `$env:ADMIN_PASS='...'" -ForegroundColor Red
            exit 1
        }
        bash scripts/smoke-auth.sh
    }

    "psql" { Invoke-Compose exec postgres psql -U $pgUser -d $pgDb }

    "redis-cli" { Invoke-Compose exec redis redis-cli -a $redisPass }

    "queues" {
        Invoke-Compose exec -T rabbitmq rabbitmqctl list_queues name messages consumers |
            Select-String "manage"
    }

    "smoke" {
        Write-Host "--- nginx ---" -ForegroundColor Cyan
        try { (Invoke-WebRequest "$publicUrl/health" -UseBasicParsing).Content } catch { Write-Host "LỖI: $_" -ForegroundColor Red }

        Write-Host "--- api /ready (postgres + redis + rabbitmq) ---" -ForegroundColor Cyan
        try { (Invoke-WebRequest "http://localhost:$apiPort/ready" -UseBasicParsing).Content } catch { Write-Host "LỖI: $_" -ForegroundColor Red }

        Write-Host "--- /api/v1/ping qua nginx ---" -ForegroundColor Cyan
        try { (Invoke-WebRequest "$publicUrl/api/v1/ping" -UseBasicParsing).Content } catch { Write-Host "LỖI: $_" -ForegroundColor Red }

        Write-Host ""
        Write-Host "--- log worker (job vừa nhận) ---" -ForegroundColor Cyan
        Start-Sleep -Seconds 2
        $log = docker compose logs --tail=30 worker 2>$null | Select-String "worker da xu ly job|worker đã xử lý job"
        if ($log) { $log[-1].Line }
        else { Write-Host "CHƯA THẤY JOB — kiểm tra bằng: .\dev.ps1 logs worker" -ForegroundColor Yellow }
    }

    "urls" {
        $ui = Get-EnvValue $dotenv "MINIO_UI_PORT" "9001"
        @"

  Ứng dụng        $publicUrl
  API trực tiếp   http://localhost:$apiPort
  Adminer (DB)    http://localhost:$(Get-EnvValue $dotenv 'ADMINER_PORT' '8081')
  RabbitMQ UI     http://localhost:$(Get-EnvValue $dotenv 'RABBITMQ_UI_PORT' '15672')
  MinIO Console   http://localhost:$ui
  MailHog         http://localhost:$(Get-EnvValue $dotenv 'MAILHOG_UI_PORT' '8025')

"@
    }

    "build-images" {
        $sha = (git rev-parse --short HEAD 2>$null)
        if (-not $sha) { $sha = "unknown" }
        $bt = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")

        foreach ($b in @("api", "worker")) {
            docker build -f docker/backend/Dockerfile --target prod `
                --build-arg BINARY=$b --build-arg GIT_SHA=$sha --build-arg BUILD_TIME=$bt `
                -t "ghcr.io/PhamVanPhuc2k2/manage-$b`:$sha" ./backend
            if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
        }
        docker build -f docker/frontend/Dockerfile --target prod `
            -t "ghcr.io/PhamVanPhuc2k2/manage-frontend`:$sha" ./frontend
    }

    "prod-up" {
        docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d
    }

    default {
        Write-Host "Lệnh không tồn tại: $Command" -ForegroundColor Red
        Write-Host "Xem danh sách: .\dev.ps1 help"
        exit 1
    }
}
