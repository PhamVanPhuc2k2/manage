# syntax=docker/dockerfile:1.7

# GHIM BASE IMAGE THEO DIGEST
#
# Thẻ `:1.27-alpine` là con trỏ di động: cùng một Dockerfile dựng hôm nay và
# tháng sau cho ra hai image khác nhau. Khi một bản dựng hỏng, không có cách
# nào dựng lại đúng thứ đã chạy để so sánh — và đó cũng là đường để một
# image bị thay đổi ở thượng nguồn đi thẳng vào production.
#
# Giữ cả thẻ lẫn digest: Docker dùng digest, còn thẻ để người đọc biết
# digest đó ứng với phiên bản nào mà không phải tra.
#
# CẬP NHẬT ĐỊNH KỲ LÀ BẮT BUỘC. Ghim rồi bỏ quên thì image đóng băng
# luôn cả những lỗ hổng đã được vá ở thượng nguồn — tệ hơn hẳn thẻ trôi nổi.
# Chạy `bash scripts/update-base-digests.sh` để lấy digest mới nhất; quyết
# định có cập nhật hay không dựa vào kết quả quét Trivy hàng tuần
# (.github/workflows/security.yml).

FROM node:22-alpine@sha256:b6f26b36c8ff49624cfdac716b8ea1138d606df02586a77d364bb5536a634f85 AS base
RUN corepack enable && corepack prepare pnpm@9.15.0 --activate
WORKDIR /app
ENV PNPM_HOME=/pnpm
ENV PATH=$PNPM_HOME:$PATH

# =============================================================================
# deps — chỉ cài dependency. Tầng này chỉ rebuild khi lockfile đổi.
# =============================================================================
FROM base AS deps
COPY package.json pnpm-lock.yaml ./
RUN --mount=type=cache,id=pnpm,target=/pnpm/store \
    pnpm install --frozen-lockfile

# =============================================================================
# dev — Next.js dev server, source bind mount từ ngoài vào.
# =============================================================================
FROM deps AS dev

# Bind mount trên Docker Desktop không phát sự kiện file tin cậy.
# Không có hai biến này thì sửa code xong trang không tự tải lại.
ENV WATCHPACK_POLLING=true
ENV CHOKIDAR_USEPOLLING=true
ENV NEXT_TELEMETRY_DISABLED=1

EXPOSE 3000
CMD ["pnpm", "dev"]

# =============================================================================
# builder — build production. Cần next.config.js có output: "standalone".
# =============================================================================
FROM deps AS builder
COPY . .
ENV NEXT_TELEMETRY_DISABLED=1
RUN pnpm build

# =============================================================================
# prod — chỉ chứa server standalone và asset tĩnh.
#
# Kế thừa thẳng từ node:22-alpine chứ KHÔNG từ `base`: stage base có cài
# corepack/pnpm, mà image chạy production không cần trình quản lý gói —
# nó chỉ chạy `node server.js`.
# =============================================================================
FROM node:22-alpine@sha256:b6f26b36c8ff49624cfdac716b8ea1138d606df02586a77d364bb5536a634f85 AS prod

WORKDIR /app

ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1
ENV PORT=3000
ENV HOSTNAME=0.0.0.0

RUN addgroup -g 1001 -S nodejs && \
    adduser -u 1001 -S nextjs -G nodejs

# Thư mục standalone đã chứa sẵn node_modules tối thiểu mà server cần.
COPY --from=builder --chown=nextjs:nodejs /app/.next/standalone ./
COPY --from=builder --chown=nextjs:nodejs /app/.next/static ./.next/static
COPY --from=builder --chown=nextjs:nodejs /app/public ./public

USER nextjs
EXPOSE 3000

CMD ["node", "server.js"]
