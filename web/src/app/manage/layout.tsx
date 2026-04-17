import ManageLayoutView from "./layout-view";

export default function ManageLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return <ManageLayoutView>{children}</ManageLayoutView>;
}
