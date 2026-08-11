"use client";

import { createContext, useContext } from "react";

interface ManagePermissionValue {
  canWrite: boolean;
}

const ManagePermissionContext = createContext<ManagePermissionValue>({
  canWrite: true,
});

export function ManagePermissionProvider({
  canWrite,
  children,
}: {
  canWrite: boolean;
  children: React.ReactNode;
}) {
  return (
    <ManagePermissionContext.Provider value={{ canWrite }}>
      {children}
    </ManagePermissionContext.Provider>
  );
}

export function useManagePermission() {
  return useContext(ManagePermissionContext);
}
