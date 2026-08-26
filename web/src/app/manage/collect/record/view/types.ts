export interface FailRecord {
  ID: number;
  originName: string;
  originId: string;
  pageNumber: number;
  hour: number;
  cause: string;
  status: number;
  retryCount: number;
  CreatedAt: string;
  UpdatedAt: string;
}

export const RECOVER_MAX_RETRY_COUNT = 5;

export const FAILURE_RECORD_STATUS = {
  pending: 1,
  success: 0,
  failed: 2,
} as const;
