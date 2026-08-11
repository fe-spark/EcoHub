/** 管理端图片上传统一白名单（后缀 + MIME，与后端 allowedImageExt 对齐） */
export const IMAGE_UPLOAD_ACCEPT =
  ".jpg,.jpeg,.png,.webp,.ico,image/jpeg,image/png,image/webp,image/x-icon,image/vnd.microsoft.icon";

const ALLOWED_EXT = new Set(["jpg", "jpeg", "png", "webp", "ico"]);

export function isAllowedImageFile(file: { name?: string; type?: string }): boolean {
  const name = (file.name || "").toLowerCase();
  const ext = name.includes(".") ? name.split(".").pop() || "" : "";
  if (ext && ALLOWED_EXT.has(ext)) {
    return true;
  }
  const type = (file.type || "").toLowerCase();
  return (
    type === "image/jpeg" ||
    type === "image/png" ||
    type === "image/webp" ||
    type === "image/x-icon" ||
    type === "image/vnd.microsoft.icon"
  );
}
