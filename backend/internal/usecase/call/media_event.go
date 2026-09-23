package call

import (
	"context"

	"github.com/google/uuid"

	domaincall "github.com/PhamVanPhuc2k2/manage/internal/domain/call"
	"github.com/PhamVanPhuc2k2/manage/pkg/logger"
)

// HandleMediaEvent ghi nhận sự kiện từ SFU.
//
// # VÌ SAO CẦN ĐƯỜNG NÀY
//
// Ba cờ had_audio / had_video / had_screen là dấu vết phục vụ audit: "trong
// cuộc họp đó người này có chiếu màn hình không". Client hoàn toàn báo được
// ba cờ ấy, nhưng dữ liệu audit mà chính người bị audit tự khai thì không
// có giá trị. SFU là bên duy nhất THẤY luồng media thật sự đi qua.
//
// CỐ Ý KHÔNG XỬ LÝ participant_left
//
// SFU cũng báo khi một người rời phòng, và dùng nó để kết thúc cuộc gọi
// nghe có vẻ hợp lý. Nhưng khi đó có HAI đường cùng kết thúc một cuộc gọi
// — đường này và đường đóng kết nối WebSocket — chạy trên hai tiến trình
// khác nhau, không thứ tự. Một cuộc gọi kết thúc hai lần với hai lý do
// khác nhau là loại lỗi chỉ hiện ra trong báo cáo, vài tuần sau.
//
// Không trả lỗi cho những gì không xử lý được: SFU sẽ thử gửi lại, và gửi
// lại một bản tin mà ta cố tình bỏ qua chỉ tốn công cả hai bên.
func (u *Usecase) HandleMediaEvent(ctx context.Context, ev domaincall.MediaEvent) error {
	log := logger.FromContext(ctx)

	callID, ok := domaincall.CallIDFromRoom(ev.RoomName)
	if !ok {
		// Phòng của ứng dụng khác dùng chung máy chủ LiveKit. Không phải lỗi.
		return nil
	}

	switch ev.Kind {
	case domaincall.MediaTrackPublished:
		employeeID, err := uuid.Parse(ev.Identity)
		if err != nil {
			log.Warn().Str("identity", ev.Identity).
				Msg("webhook media mang danh tính không phải id nhân viên")
			return nil
		}

		if err := u.participants.SetTracks(ctx, callID, employeeID,
			ev.Track == domaincall.TrackAudio,
			ev.Track == domaincall.TrackVideo,
			ev.Track == domaincall.TrackScreen,
		); err != nil {
			return err
		}

		log.Debug().
			Str("call_id", callID.String()).
			Str("employee_id", employeeID.String()).
			Str("track", string(ev.Track)).
			Msg("ghi nhận luồng media")
		return nil

	case domaincall.MediaRoomFinished:
		// Lưới đỡ thứ hai, không phải đường chính.
		//
		// Cuộc gọi lẽ ra đã kết thúc trước khi phòng đóng — chính tầng
		// nghiệp vụ gọi CloseRoom. Nhưng nếu lời gọi đó thất bại và LiveKit
		// tự dọn phòng rỗng, cuộc gọi sẽ kẹt ở 'active' và chỉ mục một
		// phần chặn vĩnh viễn mọi cuộc gọi sau trong cùng hội thoại.
		c, err := u.calls.GetByID(ctx, callID)
		if err != nil {
			return nil //nolint:nilerr // cuộc gọi đã bị xoá thì không còn gì để dọn
		}
		if c.Status.Final() {
			return nil
		}

		log.Warn().Str("call_id", callID.String()).
			Msg("phòng SFU đóng trong khi cuộc gọi còn đang chạy — dọn lại")
		return u.finish(ctx, c, domaincall.StatusEnded, domaincall.ReasonEmpty, uuid.Nil)
	}

	return nil
}
