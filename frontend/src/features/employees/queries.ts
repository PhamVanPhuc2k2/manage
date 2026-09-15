import {
  useMutation,
  useQuery,
  useQueryClient,
  keepPreviousData,
} from "@tanstack/react-query";

import {
  confirmAvatar,
  createAccount,
  createEmployee,
  deactivateEmployee,
  getEmployee,
  getEmployeeRoles,
  listEmployees,
  removeAvatar,
  requestAvatarUpload,
  setAccountActive,
  setEmployeeRoles,
  updateEmployee,
} from "./api";
import type { EmployeeFilters } from "./types";

/**
 * Khoá cache, dựng theo tầng.
 *
 * Nhờ cấu trúc phân tầng này, `invalidateQueries({ queryKey: keys.all })` sau
 * khi thêm hoặc sửa sẽ làm mới TẤT CẢ danh sách và chi tiết cùng lúc — không
 * phải nhớ từng khoá một.
 *
 * Bộ lọc nằm TRONG khoá: đổi trang hay đổi từ khoá tìm kiếm là một khoá khác,
 * nên mỗi kết quả được cache riêng và quay lại trang cũ có ngay.
 */
export const employeeKeys = {
  all: ["employees"] as const,
  lists: () => [...employeeKeys.all, "list"] as const,
  list: (f: EmployeeFilters) => [...employeeKeys.lists(), f] as const,
  details: () => [...employeeKeys.all, "detail"] as const,
  detail: (id: string) => [...employeeKeys.details(), id] as const,
};

export function useEmployees(filters: EmployeeFilters) {
  return useQuery({
    queryKey: employeeKeys.list(filters),
    queryFn: () => listEmployees(filters),

    // Giữ dữ liệu trang cũ trong lúc tải trang mới.
    //
    // Không có nó, mỗi lần đổi trang hay gõ thêm một ký tự tìm kiếm là bảng
    // nháy về trạng thái rỗng rồi mới hiện lại — nhìn rất giật.
    placeholderData: keepPreviousData,
  });
}

export function useEmployee(id: string | undefined) {
  return useQuery({
    queryKey: employeeKeys.detail(id ?? ""),
    queryFn: () => getEmployee(id!),
    // Chưa có id (ví dụ đang ở form tạo mới) thì đừng gọi API.
    enabled: Boolean(id),
  });
}

export function useCreateEmployee() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: createEmployee,
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: employeeKeys.lists() });
    },
  });
}

export function useUpdateEmployee(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: unknown) => updateEmployee(id, body),
    onSuccess: (updated) => {
      // Ghi thẳng bản mới vào cache chi tiết: màn hình chi tiết cập nhật
      // ngay, không phải chờ thêm một vòng request.
      qc.setQueryData(employeeKeys.detail(id), updated);
      void qc.invalidateQueries({ queryKey: employeeKeys.lists() });
    },
  });
}

export function useDeactivateEmployee() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: deactivateEmployee,
    onSuccess: (_, id) => {
      qc.removeQueries({ queryKey: employeeKeys.detail(id) });
      void qc.invalidateQueries({ queryKey: employeeKeys.lists() });
    },
  });
}

// ------------------------------------------- tài khoản, vai trò, ảnh đại diện

export function useEmployeeRoles(employeeId: string, enabled = true) {
  return useQuery({
    queryKey: [...employeeKeys.detail(employeeId), "roles"],
    queryFn: () => getEmployeeRoles(employeeId),
    enabled,
    // Nhân viên chưa có tài khoản thì endpoint trả 404 — đó là câu trả lời
    // hợp lệ, không phải lỗi tạm thời, nên đừng thử lại.
    retry: false,
  });
}

export function useCreateAccount(employeeId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => createAccount(employeeId),
    onSuccess: () => {
      // has_account đổi nên cả chi tiết lẫn danh sách đều cũ.
      void qc.invalidateQueries({ queryKey: employeeKeys.all });
    },
  });
}

export function useSetAccountActive(employeeId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (active: boolean) => setAccountActive(employeeId, active),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: employeeKeys.detail(employeeId) });
    },
  });
}

export function useSetEmployeeRoles(employeeId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (roles: string[]) => setEmployeeRoles(employeeId, roles),
    onSuccess: (data) => {
      qc.setQueryData([...employeeKeys.detail(employeeId), "roles"], data);
    },
  });
}

/**
 * Tải ảnh đại diện — ba bước trong một mutation.
 *
 * Gộp lại để nơi gọi chỉ cần truyền File, không phải tự điều phối ba lời gọi
 * và tự xử lý trường hợp bước giữa lỗi.
 */
export function useUploadAvatar(employeeId: string) {
  const qc = useQueryClient();

  return useMutation({
    mutationFn: async (file: File) => {
      // 1. Xin URL có chữ ký
      const ticket = await requestAvatarUpload(employeeId, file.type);

      // 2. PUT thẳng lên Cloudflare R2.
      //
      // Dùng fetch trần chứ KHÔNG dùng api-client: đây là request tới R2,
      // không phải tới backend của mình. Gắn Authorization header của mình
      // vào sẽ làm chữ ký của R2 sai.
      const res = await fetch(ticket.upload_url, {
        method: "PUT",
        body: file,
        headers: { "Content-Type": file.type },
      });
      if (!res.ok) {
        throw new Error(`Tải ảnh lên thất bại (${res.status})`);
      }

      // 3. Xác nhận — server kiểm tra rồi mới ghi vào database
      return confirmAvatar(employeeId, ticket.key);
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: employeeKeys.all });
    },
  });
}

export function useRemoveAvatar(employeeId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => removeAvatar(employeeId),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: employeeKeys.all });
    },
  });
}
