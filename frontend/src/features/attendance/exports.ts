import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api, downloadFile } from "@/lib/api-client";

/**
 * Xuất báo cáo chấm công ra Excel.
 *
 * Máy chủ chỉ ghi nhận yêu cầu, worker dựng tệp rồi gửi thông báo. Trang
 * danh sách hỏi lại mỗi giây khi còn lượt đang chạy, để người đang đứng chờ
 * thấy nút "Tải về" hiện ra mà không phải tải lại trang.
 */

export type ExportStatus = "queued" | "processing" | "done" | "failed";

export type AttendanceExport = {
  id: string;
  year: number;
  month: number;
  department_id?: string;
  department_name?: string;
  status: ExportStatus;
  requester_name?: string;
  file_name?: string;
  has_file: boolean;
  employee_count: number;
  error?: string;
  created_at: string;
  finished_at?: string;
};

export const EXPORT_STATUS_LABEL: Record<ExportStatus, string> = {
  queued: "Đang chờ",
  processing: "Đang dựng tệp",
  done: "Xong",
  failed: "Hỏng",
};

const exportKeys = {
  all: ["attendance", "exports"] as const,
};

export function useAttendanceExports() {
  return useQuery({
    queryKey: exportKeys.all,
    queryFn: async () =>
      (await api.get<AttendanceExport[]>("/attendance/exports")).data ?? [],
    refetchInterval: (q) =>
      (q.state.data ?? []).some(
        (e) => e.status === "queued" || e.status === "processing",
      )
        ? 1000
        : false,
  });
}

export function useRequestExport() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (v: {
      year: number;
      month: number;
      departmentId?: string;
    }) =>
      (
        await api.post<AttendanceExport>("/attendance/exports", {
          year: v.year,
          month: v.month,
          department_id: v.departmentId || undefined,
        })
      ).data,
    onSuccess: () => qc.invalidateQueries({ queryKey: exportKeys.all }),
  });
}

export function downloadExport(e: AttendanceExport) {
  return downloadFile(
    `/attendance/exports/${e.id}/file`,
    `cham-cong-${e.year}-${String(e.month).padStart(2, "0")}.xlsx`,
  );
}
