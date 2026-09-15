# syntax=docker/dockerfile:1.7

FROM node:22-alpine AS base
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
FROM node:22-alpine AS prod

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
