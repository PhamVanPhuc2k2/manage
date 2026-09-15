// Package storage lưu tệp trên Cloudflare R2.
//
// R2 dùng giao thức S3 nên SDK của AWS chạy được, nhưng có vài khác biệt bắt
// buộc phải xử lý đúng, nếu không lỗi sinh ra rất khó đoán. Xem ghi chú
// trong hàm New.
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

var (
	// ErrNotConfigured trả về khi chưa khai báo thông tin R2. Hệ thống vẫn
	// chạy bình thường, chỉ chức năng tệp là không dùng được — nhờ vậy phát
	// triển phần không liên quan tới tệp không cần tài khoản Cloudflare.
	ErrNotConfigured = errors.New("chưa cấu hình Cloudflare R2")

	ErrNotFound = errors.New("không tìm thấy tệp")
)

type Config struct {
	AccountID       string
	AccessKeyID     string
	SecretAccessKey string
	Bucket          string
	// PublicURL là tên miền công khai của bucket nếu có bật.
	// Để trống thì mỗi lần xem sẽ sinh presigned URL có hạn.
	PublicURL string
}

func (c Config) configured() bool {
	return c.AccountID != "" && c.AccessKeyID != "" &&
		c.SecretAccessKey != "" && c.Bucket != ""
}

type Storage struct {
	client    *s3.Client
	presign   *s3.PresignClient
	bucket    string
	publicURL string
}

// New tạo client R2. Trả về (nil, nil) khi chưa cấu hình — nơi gọi kiểm tra
// nil để biết chức năng tệp có dùng được không.
func New(ctx context.Context, cfg Config) (*Storage, error) {
	if !cfg.configured() {
		return nil, nil
	}

	endpoint := fmt.Sprintf("https://%s.r2.cloudflarestorage.com", cfg.AccountID)

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		// R2 KHÔNG có vùng địa lý. Phải đặt đúng chuỗi "auto";
		// điền tên vùng của AWS (us-east-1...) sẽ làm chữ ký sai.
		awsconfig.WithRegion("auto"),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(
				cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("nạp cấu hình AWS SDK: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)

		// BẮT BUỘC dùng path-style.
		//
		// Mặc định SDK ghép tên bucket vào tên miền:
		//     https://<bucket>.<account>.r2.cloudflarestorage.com
		// R2 KHÔNG hỗ trợ dạng đó — mọi request sẽ lỗi phân giải tên miền.
		// Path-style cho ra dạng R2 dùng:
		//     https://<account>.r2.cloudflarestorage.com/<bucket>/<key>
		o.UsePathStyle = true

		// Tắt checksum tự động.
		//
		// Từ giữa 2025, aws-sdk-go-v2 mặc định thêm header
		// x-amz-checksum-crc32 vào mọi PutObject. Với presigned URL, trình
		// duyệt không gửi header đó nên chữ ký không khớp và R2 trả về
		// lỗi SignatureDoesNotMatch — rất khó lần ra vì chữ ký "trông đúng".
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
	})

	return &Storage{
		client:    client,
		presign:   s3.NewPresignClient(client),
		bucket:    cfg.Bucket,
		publicURL: strings.TrimRight(cfg.PublicURL, "/"),
	}, nil
}

// PresignPut sinh URL cho phép client tải tệp lên THẲNG R2.
//
// Vì sao không cho tệp đi qua backend? Vì mỗi tệp sẽ chiếm một luồng và một
// lượng RAM của api trong suốt thời gian tải. Với ảnh đại diện thì không sao,
// nhưng tới Phase 2 khi đính kèm tài liệu vài chục MB thì đó là cách chắc
// chắn làm nghẽn hệ thống.
func (s *Storage) PresignPut(
	ctx context.Context,
	key, contentType string,
	ttl time.Duration,
) (string, error) {
	if s == nil {
		return "", ErrNotConfigured
	}

	req, err := s.presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		ContentType: aws.String(contentType),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("sinh URL tải lên: %w", err)
	}
	return req.URL, nil
}

// PresignGet sinh URL xem tệp có hạn.
//
// Nếu bucket có tên miền công khai thì trả thẳng URL đó — nhanh hơn, cache
// được, và không phải ký lại mỗi lần hiển thị.
func (s *Storage) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if s == nil {
		return "", ErrNotConfigured
	}
	if key == "" {
		return "", nil
	}
	if s.publicURL != "" {
		return s.publicURL + "/" + key, nil
	}

	req, err := s.presign.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("sinh URL xem tệp: %w", err)
	}
	return req.URL, nil
}

type ObjectInfo struct {
	Size        int64
	ContentType string
}

// Stat đọc thông tin tệp mà không tải nội dung.
func (s *Storage) Stat(ctx context.Context, key string) (*ObjectInfo, error) {
	if s == nil {
		return nil, ErrNotConfigured
	}

	out, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		var nf *types.NotFound
		if errors.As(err, &nf) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("đọc thông tin tệp: %w", err)
	}

	info := &ObjectInfo{}
	if out.ContentLength != nil {
		info.Size = *out.ContentLength
	}
	if out.ContentType != nil {
		info.ContentType = *out.ContentType
	}
	return info, nil
}

// DetectContentType đọc 512 byte đầu và đoán kiểu tệp từ nội dung thật.
//
// KHÔNG BAO GIỜ tin Content-Type do client khai báo: đó chỉ là một header,
// ai cũng sửa được. Kẻ tấn công có thể tải lên một tệp thực thi và khai là
// image/png. Kiểm tra magic bytes là cách duy nhất biết tệp thật sự là gì.
func (s *Storage) DetectContentType(ctx context.Context, key string) (string, error) {
	if s == nil {
		return "", ErrNotConfigured
	}

	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Range:  aws.String("bytes=0-511"),
	})
	if err != nil {
		var nf *types.NoSuchKey
		if errors.As(err, &nf) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("đọc đầu tệp: %w", err)
	}
	defer func() { _ = out.Body.Close() }()

	buf := make([]byte, 512)
	n, err := io.ReadFull(out.Body, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("đọc đầu tệp: %w", err)
	}

	return http.DetectContentType(buf[:n]), nil
}

func (s *Storage) Delete(ctx context.Context, key string) error {
	if s == nil {
		return ErrNotConfigured
	}
	if key == "" {
		return nil
	}

	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("xoá tệp: %w", err)
	}
	return nil
}

// HealthCheck kiểm tra bucket có truy cập được không. Dùng cho /ready.
func (s *Storage) HealthCheck(ctx context.Context) error {
	if s == nil {
		return ErrNotConfigured
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(s.bucket),
	})
	if err != nil {
		return fmt.Errorf("không truy cập được bucket %q: %w", s.bucket, err)
	}
	return nil
}
