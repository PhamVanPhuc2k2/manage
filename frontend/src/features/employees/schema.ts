import { z } from "zod";

/**
 * Schema kiểm tra dữ liệu form nhân viên.
 *
 * Đây CHỈ là kiểm tra ở client để phản hồi nhanh cho người dùng. Backend vẫn
 * kiểm tra đầy đủ và độc lập — ai cũng gọi được API bằng curl, bỏ qua toàn
 * bộ phần này.
 */
export const employeeSchema = z
  .object({
    employee_code: z
      .string()
      .trim()
      .min(1, "Mã nhân viên không được để trống")
      .max(50, "Mã nhân viên tối đa 50 ký tự"),

    full_name: z
      .string()
      .trim()
      .min(1, "Họ tên không được để trống")
      .max(255, "Họ tên tối đa 255 ký tự"),

    email: z.string().trim().email("Email không hợp lệ"),

    // Chuỗi rỗng là hợp lệ cho các trường không bắt buộc. Dùng .or(literal(""))
    // thay vì .optional() vì input HTML luôn trả về chuỗi, không bao giờ undefined.
    phone: z
      .string()
      .trim()
      .regex(/^0\d{9,10}$/, "Số điện thoại phải bắt đầu bằng 0 và có 10–11 chữ số")
      .or(z.literal("")),

    date_of_birth: z.string(),
    gender: z.string(),
    address: z.string(),

    department_id: z.string(),
    position_id: z.string(),
    manager_id: z.string(),

    work_mode: z.enum(["onsite", "remote", "hybrid"]),
    status: z.enum(["probation", "official", "resigned"]),

    joined_at: z.string().min(1, "Ngày vào làm không được để trống"),
    resigned_at: z.string(),
  })
  .refine(
    (v) => !v.resigned_at || !v.joined_at || v.resigned_at >= v.joined_at,
    {
      message: "Ngày nghỉ việc phải sau ngày vào làm",
      path: ["resigned_at"],
    },
  )
  .refine((v) => v.status !== "resigned" || v.resigned_at !== "", {
    message: "Nhân viên đã nghỉ việc thì phải có ngày nghỉ",
    path: ["resigned_at"],
  });

export type EmployeeFormValues = z.infer<typeof employeeSchema>;

export const emptyEmployeeForm: EmployeeFormValues = {
  employee_code: "",
  full_name: "",
  email: "",
  phone: "",
  date_of_birth: "",
  gender: "",
  address: "",
  department_id: "",
  position_id: "",
  manager_id: "",
  work_mode: "onsite",
  status: "probation",
  joined_at: new Date().toISOString().slice(0, 10),
  resigned_at: "",
};

/**
 * Đổi giá trị form thành payload gửi lên API.
 *
 * Backend phân biệt rõ "không có giá trị" (null) với "chuỗi rỗng", nên phải
 * đổi chuỗi rỗng thành null ở đây. Gửi department_id="" sẽ bị từ chối với
 * lỗi "department_id không hợp lệ".
 */
export function toEmployeePayload(v: EmployeeFormValues) {
  const orNull = (s: string) => (s.trim() === "" ? null : s.trim());

  return {
    employee_code: v.employee_code.trim(),
    full_name: v.full_name.trim(),
    email: v.email.trim(),
    phone: v.phone.trim(),
    date_of_birth: orNull(v.date_of_birth),
    gender: v.gender,
    address: v.address,
    department_id: orNull(v.department_id),
    position_id: orNull(v.position_id),
    manager_id: orNull(v.manager_id),
    work_mode: v.work_mode,
    status: v.status,
    joined_at: v.joined_at,
    resigned_at: orNull(v.resigned_at),
  };
}
