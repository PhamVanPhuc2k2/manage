"use client";

import { useState } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ReactQueryDevtools } from "@tanstack/react-query-devtools";

import { ApiError } from "./api-client";

function makeQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: {
        // Dữ liệu được coi là còn "tươi" trong 30 giây.
        //
        // Không có staleTime, mỗi lần component mount lại là một request mới —
        // chuyển tab qua lại vài lần là bắn hàng chục request vô ích.
        staleTime: 30_000,

        // Giữ trong cache 5 phút sau khi không còn component nào dùng.
        // Nhờ vậy quay lại trang vừa rời khỏi thì có dữ liệu ngay, rồi mới
        // âm thầm làm mới ở nền.
        gcTime: 5 * 60_000,

        retry: (failureCount, error) => {
          // KHÔNG thử lại với lỗi 4xx.
          //
          // 401, 403, 404, 400 là lỗi của chính request — gửi lại y hệt thì
          // vẫn lỗi y hệt, chỉ tổ chậm và làm nhiễu log. Riêng 401 còn nguy
          // hiểm hơn: api-client đã tự refresh token một lần rồi, thử lại
          // nữa có thể kích hoạt cơ chế chống đánh cắp refresh token.
          if (error instanceof ApiError && error.status < 500) return false;
          return failureCount < 2;
        },

        // Làm mới khi người dùng quay lại tab. Với hệ thống nhiều người cùng
        // sửa dữ liệu (HR thêm nhân viên trong khi bạn đang xem danh sách),
        // đây là hành vi đúng. staleTime ở trên đã chặn việc bắn request liên tục.
        refetchOnWindowFocus: true,
        refetchOnReconnect: true,
      },
      mutations: {
        // Thao tác ghi thì KHÔNG bao giờ tự thử lại: gửi lại một lệnh tạo
        // nhân viên có thể tạo ra hai bản ghi.
        retry: false,
      },
    },
  });
}

export function QueryProvider({ children }: { children: React.ReactNode }) {
  // Tạo client trong useState chứ không phải ở phạm vi module.
  //
  // Client ở phạm vi module sẽ được dùng chung giữa các request khi render
  // phía máy chủ — dữ liệu của người dùng này lọt sang người dùng khác.
  const [queryClient] = useState(makeQueryClient);

  return (
    <QueryClientProvider client={queryClient}>
      {children}
      {process.env.NODE_ENV === "development" && (
        <ReactQueryDevtools initialIsOpen={false} buttonPosition="bottom-left" />
      )}
    </QueryClientProvider>
  );
}
