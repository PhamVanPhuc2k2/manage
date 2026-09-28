import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api } from "@/lib/api-client";

import { employeeKeys } from "./queries";

/**
 * Nhập nhân viên hàng loạt từ CSV/Excel.
 *
 * Tệp được đọc ngay ở máy chủ (lỗi của cả tệp trả về lúc tải lên), còn nhân
 * viên được tạo ở worker. Trang kết quả hỏi lại định kỳ cho tới khi xong —
 * và người nhập cũng nhận một thông báo, nên đóng trang đi cũng không sao.
 */

export type ImportStatus = "queued" | "processing" | "done" | "failed";

export type ImportRowResult = {
  line: number;
  employee_code: string;
  full_name: string;
  employee_id?: string;
  error?: string;
  warning?: string;
  account_created: boolean;
};

export type EmployeeImport = {
  id: string;
  file_name: string;
  status: ImportStatus;
  create_accounts: boolean;
  total_rows: number;
  processed: number;
  succeeded: number;
  failed: number;
  creator_name?: string;
  error?: string;
  created_at: string;
  started_at?: string;
  finished_at?: string;
  results?: ImportRowResult[];
};

export type ImportTemplate = {
  columns: string[];
  required_columns: string[];
  max_rows: number;
};

export const IMPORT_STATUS_LABEL: Record<ImportStatus, string> = {
  queued: "Đang chờ",
  processing: "Đang nhập",
  done: "Xong",
  failed: "Hỏng",
};

export const importKeys = {
  all: [...employeeKeys.all, "imports"] as const,
  list: () => [...importKeys.all, "list"] as const,
  detail: (id: string) => [...importKeys.all, "detail", id] as const,
};

export function useImports() {
  return useQuery({
    queryKey: importKeys.list(),
    queryFn: async () => {
      const { data, meta } =
        await api.get<EmployeeImport[]>("/employees/imports");
      return {
        items: data ?? [],
        // meta của endpoint này là mô tả tệp mẫu, không phải phân trang.
        template: meta as unknown as ImportTemplate,
      };
    },
  });
}

export function useImport(id: string | undefined) {
  return useQuery({
    queryKey: importKeys.detail(id ?? ""),
    queryFn: async () =>
      (await api.get<EmployeeImport>(`/employees/imports/${id}`)).data,
    enabled: Boolean(id),

    // Hỏi lại mỗi giây tới khi worker xong. Không dùng WebSocket cho việc
    // này: vài trăm dòng xong trong vài giây, và thông báo realtime đã báo
    // cho người nhập khi xong rồi.
    refetchInterval: (q) => {
      const s = q.state.data?.status;
      return s === "done" || s === "failed" ? false : 1000;
    },
  });
}

export function useStartImport() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (v: { file: File; createAccounts: boolean }) => {
      const form = new FormData();
      form.append("file", v.file);
      form.append("create_accounts", String(v.createAccounts));
      return (await api.post<EmployeeImport>("/employees/imports", form)).data;
    },
    onSuccess: () => {
      // Làm mới cả danh sách nhân viên: người mới sẽ xuất hiện ở đó.
      void qc.invalidateQueries({ queryKey: employeeKeys.all });
    },
  });
}

/** Tiêu đề hiển thị của từng cột trong tệp mẫu. */
const COLUMN_TITLE: Record<string, string> = {
  ma_nhan_vien: "Mã nhân viên",
  ho_ten: "Họ tên",
  email: "Email",
  so_dien_thoai: "Số điện thoại",
  ngay_sinh: "Ngày sinh",
  gioi_tinh: "Giới tính",
  dia_chi: "Địa chỉ",
  phong_ban: "Phòng ban",
  chuc_vu: "Chức vụ",
  ma_cap_tren: "Mã cấp trên",
  hinh_thuc_lam_viec: "Hình thức làm việc",
  trang_thai: "Trạng thái",
  ngay_vao_lam: "Ngày vào làm",
};

const EXAMPLE: Record<string, string> = {
  ma_nhan_vien: "NV001",
  ho_ten: "Nguyễn Văn An",
  email: "an.nguyen@congty.vn",
  so_dien_thoai: "0901234567",
  ngay_sinh: "15/03/1995",
  gioi_tinh: "Nam",
  dia_chi: "Hà Nội",
  phong_ban: "Kế toán",
  chuc_vu: "Nhân viên",
  ma_cap_tren: "",
  hinh_thuc_lam_viec: "Tại văn phòng",
  trang_thai: "Thử việc",
  ngay_vao_lam: "01/10/2026",
};

export function columnTitle(key: string): string {
  return COLUMN_TITLE[key] ?? key;
}

/**
 * Sinh tệp mẫu CSV từ danh sách cột mà MÁY CHỦ trả về — tệp mẫu không
 * bao giờ lệch với bộ đọc.
 *
 * Tiêu đề viết tiếng Việt có dấu (máy chủ tự quy về tên chuẩn), và tệp bắt
 * đầu bằng BOM: thiếu BOM thì Excel mở tệp UTF-8 thành chữ rác.
 */
export function downloadTemplate(t: ImportTemplate) {
  const esc = (s: string) =>
    /[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
  const header = t.columns.map((c) => esc(columnTitle(c))).join(",");
  const example = t.columns.map((c) => esc(EXAMPLE[c] ?? "")).join(",");
  const blob = new Blob(["\ufeff" + header + "\n" + example + "\n"], {
    type: "text/csv;charset=utf-8",
  });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = "mau-nhap-nhan-vien.csv";
  a.click();
  URL.revokeObjectURL(url);
}
