export interface ConfigCardHandle {
  /** 当前模块是否存在未保存更改 */
  isDirty: () => boolean;
  /** 校验当前草稿，不通过时自行提示并返回 false */
  validate: () => boolean;
  /** 保存当前模块更改，返回是否保存成功 */
  save: () => Promise<boolean>;
  /** 放弃更改并恢复为已保存数据 */
  discard: () => void;
}
