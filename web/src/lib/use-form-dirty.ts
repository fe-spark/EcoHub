"use client";

import { useState, useCallback, useEffect, useRef } from "react";
import type { FormInstance } from "antd";

/**
 * 递归归一化数据：统一空字符串、null、undefined 为等价空值并去除字符串两端空白
 */
export function normalizeFormValue(val: any): any {
  if (val === undefined || val === null) return "";
  if (typeof val === "string") return val.trim();
  if (typeof val === "boolean" || typeof val === "number") return val;
  if (Array.isArray(val)) return val.map(normalizeFormValue);
  if (typeof val === "object") {
    const res: Record<string, any> = {};
    const sortedKeys = Object.keys(val).sort();
    for (const k of sortedKeys) {
      res[k] = normalizeFormValue(val[k]);
    }
    return res;
  }
  return val;
}

export function isFormValuesEqual(a: any, b: any): boolean {
  if (a === b) return true;
  if (!a && !b) return true;
  return JSON.stringify(normalizeFormValue(a)) === JSON.stringify(normalizeFormValue(b));
}

/**
 * Ant Design 表单脏状态追踪 Hook
 * 核心保证：
 * 1. 页面加载与数据初始化期间（用户未触碰）isDirty 绝对为 false，杜绝误弹；
 * 2. 仅在用户实际交互触发 onValuesChange 后，比对当前值与基准数据；
 * 3. 若改回初始值，isDirty 自动复原为 false；
 * 4. 放弃更改或保存成功后，重置基准并隐藏操作条。
 */
export function useFormDirtyTracker<T extends Record<string, any>>(
  form: FormInstance<T>,
  initialValues: T | null | undefined
) {
  const [isDirty, setIsDirty] = useState(false);
  const baselineRef = useRef<T | null>(null);

  // 初始化或服务端数据更新时，同步基准并重置脏状态
  const setBaseline = useCallback((values: T) => {
    baselineRef.current = values;
    setIsDirty(false);
  }, []);

  useEffect(() => {
    if (initialValues) {
      baselineRef.current = initialValues;
      setIsDirty(false);
    }
  }, [initialValues]);

  // 用户修改表单字段时触发
  const onValuesChange = useCallback((_changedValues: any, allValues: T) => {
    if (!baselineRef.current) {
      setIsDirty(false);
      return;
    }
    const equal = isFormValuesEqual(allValues, baselineRef.current);
    setIsDirty(!equal);
  }, []);

  // 放弃更改并还原为初始基准
  const resetToBaseline = useCallback(() => {
    if (baselineRef.current) {
      form.setFieldsValue(baselineRef.current);
    }
    setIsDirty(false);
  }, [form]);

  return {
    isDirty,
    setIsDirty,
    onValuesChange,
    setBaseline,
    resetToBaseline,
  };
}
