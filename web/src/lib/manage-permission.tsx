"use client";

import { createContext, useContext } from "react";

interface ManagePermissionValue {
  canWrite: boolean;
  isAdmin: boolean;
}

const ManagePermissionContext = createContext<ManagePermissionValue>({
  canWrite: true,
  isAdmin: false,
});

export function ManagePermissionProvider({
  canWrite,
  isAdmin = false,
  children,
}: {
  canWrite: boolean;
  isAdmin?: boolean;
  children: React.ReactNode;
}) {
  return (
    <ManagePermissionContext.Provider value={{ canWrite, isAdmin }}>
      {children}
    </ManagePermissionContext.Provider>
  );
}

export function useManagePermission() {
  return useContext(ManagePermissionContext);
}
