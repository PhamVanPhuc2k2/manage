import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import {
  createDepartment,
  deleteDepartment,
  getDepartmentTree,
  listDepartments,
  updateDepartment,
} from "./api";

export const departmentKeys = {
  all: ["departments"] as const,
  list: () => [...departmentKeys.all, "list"] as const,
  tree: () => [...departmentKeys.all, "tree"] as const,
};

export function useDepartments() {
  return useQuery({
    queryKey: departmentKeys.list(),
    queryFn: listDepartments,
    // Phòng ban gần như không đổi trong một phiên làm việc. Để 5 phút thay
    // vì 30 giây mặc định, tránh gọi lại mỗi lần mở dropdown chọn phòng ban.
    staleTime: 5 * 60_000,
  });
}

export function useDepartmentTree() {
  return useQuery({
    queryKey: departmentKeys.tree(),
    queryFn: getDepartmentTree,
    staleTime: 5 * 60_000,
  });
}

/**
 * invalidate cả `all` chứ không riêng `list` hay `tree`.
 *
 * Danh sách phẳng và cây là hai cách nhìn của CÙNG một dữ liệu — sửa một
 * phòng ban là cả hai đều cũ. Chỉ làm mới một cái sẽ để lại dữ liệu mâu
 * thuẫn giữa hai màn hình.
 */
function useDepartmentMutation<TArgs, TResult>(
  fn: (args: TArgs) => Promise<TResult>,
) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: departmentKeys.all });
    },
  });
}

export function useCreateDepartment() {
  return useDepartmentMutation(createDepartment);
}

export function useUpdateDepartment(id: string) {
  return useDepartmentMutation((body: unknown) => updateDepartment(id, body));
}

export function useDeleteDepartment() {
  return useDepartmentMutation(deleteDepartment);
}
