#!/usr/bin/env bash
# Cập nhật digest của base image trong các Dockerfile.
#
# Chạy:
#   bash scripts/update-base-digests.sh          # chỉ xem, không sửa
#   bash scripts/update-base-digests.sh --write  # ghi vào Dockerfile
#
# VÌ SAO CẦN SCRIPT NÀY
#
# Ghim base image theo digest chặn được hai chuyện: bản dựng cho ra kết quả
# khác nhau theo thời gian, và một image bị thay đổi ở thượng nguồn đi thẳng
# vào production.
#
# Nhưng ghim rồi bỏ quên còn tệ hơn thẻ trôi nổi: image đóng băng luôn cả
# những lỗ hổng đã được vá. Thẻ trôi nổi ít ra còn tự nhận bản vá.
#
# Vì vậy việc ghim chỉ đúng khi ĐI KÈM một đường làm mới. Đây là đường đó.
# Lịch chạy đề xuất: mỗi khi quét bảo mật hàng tuần
# (.github/workflows/security.yml) báo lỗ hổng ở tầng hệ điều hành, hoặc
# định kỳ mỗi tháng một lần.
#
# Script KHÔNG tự commit và KHÔNG tự chạy trong CI. Đổi base image là việc
# cần người nhìn: bản mới có thể đổi phiên bản thư viện hệ thống và làm hỏng
# build theo những cách chỉ lộ ra lúc chạy.

set -uo pipefail
cd "$(dirname "$0")/.."

WRITE=0
[ "${1:-}" = "--write" ] && WRITE=1

# Danh sách image cần theo dõi và tệp chứa chúng.
FILES=(docker/backend/Dockerfile docker/frontend/Dockerfile)

red()   { printf '\033[31m%s\033[0m\n' "$1"; }
green() { printf '\033[32m%s\033[0m\n' "$1"; }
dim()   { printf '\033[2m%s\033[0m\n' "$1"; }

command -v docker >/dev/null || { red "cần docker để tra digest"; exit 1; }

# Gom mọi dòng FROM có ghim digest, dạng "tệp:dòng:image:thẻ@digest".
mapfile -t PINS < <(grep -Hn '^FROM [^ ]*@sha256:' "${FILES[@]}" 2>/dev/null)

if [ "${#PINS[@]}" -eq 0 ]; then
  red "Không tìm thấy dòng FROM nào đã ghim digest."
  echo "Ghim thủ công lần đầu rồi dùng script này để cập nhật."
  exit 1
fi

CHANGED=0
SEEN=""

for pin in "${PINS[@]}"; do
  file="${pin%%:*}"
  rest="${pin#*:}"
  line="${rest%%:*}"
  from="${rest#*:}"

  # "FROM image:thẻ@sha256:... AS stage" -> "image:thẻ@sha256:..."
  ref=$(echo "$from" | awk '{print $2}')
  repo_tag="${ref%@*}"
  old_digest="${ref#*@}"

  # Mỗi image chỉ tra mạng một lần dù xuất hiện ở nhiều stage.
  cached=$(echo "$SEEN" | grep "^$repo_tag " | head -1)
  if [ -n "$cached" ]; then
    new_digest="${cached#* }"
  else
    dim "tra $repo_tag ..."
    new_digest=$(docker buildx imagetools inspect "$repo_tag" 2>/dev/null \
      | grep -m1 '^Digest:' | awk '{print $2}')
    if [ -z "$new_digest" ]; then
      red "  không tra được digest của $repo_tag — bỏ qua"
      continue
    fi
    SEEN="$SEEN
$repo_tag $new_digest"
  fi

  if [ "$old_digest" = "$new_digest" ]; then
    green "✓ $repo_tag đã mới nhất  ($file:$line)"
    continue
  fi

  CHANGED=$((CHANGED + 1))
  echo
  printf '\033[33m↑ %s\033[0m  (%s:%s)\n' "$repo_tag" "$file" "$line"
  echo "    cũ:  $old_digest"
  echo "    mới: $new_digest"

  if [ "$WRITE" -eq 1 ]; then
    # Thay TOÀN BỘ chuỗi digest cũ trong tệp: cùng một image có thể xuất
    # hiện ở nhiều stage và phải đổi hết, nếu không hai stage dùng hai bản
    # khác nhau mà không ai để ý.
    sed -i "s|$old_digest|$new_digest|g" "$file"
  fi
done

echo
if [ "$CHANGED" -eq 0 ]; then
  green "Mọi base image đã ở digest mới nhất."
  exit 0
fi

if [ "$WRITE" -eq 1 ]; then
  green "Đã cập nhật $CHANGED image."
  echo
  echo "Việc tiếp theo, KHÔNG được bỏ:"
  echo "  1. docker compose build --no-cache api worker frontend"
  echo "  2. bash scripts/smoke-auth.sh   (và các bộ smoke còn lại)"
  echo "  3. Đọc kỹ diff rồi mới commit"
  echo
  echo "Base image mới có thể đổi phiên bản thư viện hệ thống; có những thứ"
  echo "chỉ hỏng lúc chạy chứ không hỏng lúc build."
else
  echo "Đang ở chế độ chỉ xem. Chạy lại với --write để ghi."
fi
