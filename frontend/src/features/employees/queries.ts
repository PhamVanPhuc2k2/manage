import {
  useMutation,
  useQuery,
  useQueryClient,
  keepPreviousData,
} from "@tanstack/react-query";

import {
  createEmployee,
  deactivateEmployee,
  getEmployee,
  listEmployees,
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
