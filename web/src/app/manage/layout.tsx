import { cookies } from "next/headers";
import { redirectToLogin, serverGet } from "@/lib/server-api";
import ManageLayoutView from "./layout-view";

async function verifyManageUser() {
  const cookieStore = await cookies();
  const authCookie = cookieStore.get("ecohub_auth_token");
  if (!authCookie?.value) {
    await redirectToLogin();
  }

  try {
    const response = await serverGet("/manage/user/info", undefined, {
      Cookie: cookieStore.toString(),
    });
    if (response.code !== 0) {
      await redirectToLogin();
    }
  } catch {
    await redirectToLogin();
  }
}

export default async function ManageLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  await verifyManageUser();

  return <ManageLayoutView>{children}</ManageLayoutView>;
}
