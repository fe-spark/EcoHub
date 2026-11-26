"use client";

import React from "react";
import { Alert, Form, Input, InputNumber, Modal, Radio, Select, Switch } from "antd";
import type { FormInstance } from "antd";
import {
  ChartOption,
  RecommendAdminGroup,
  RecommendCategoryOption,
} from "./recommend-types";

interface RecommendGroupModalProps {
  open: boolean;
  editingGroup: RecommendAdminGroup | null;
  saving: boolean;
  form: FormInstance;
  chartOptions: ChartOption[];
  categoryOptions: RecommendCategoryOption[];
  onCancel: () => void;
  onOk: () => void;
}

export default function RecommendGroupModal({
  open,
  editingGroup,
  saving,
  form,
  chartOptions,
  categoryOptions,
  onCancel,
  onOk,
}: RecommendGroupModalProps) {
  return (
    <Modal
      title={editingGroup ? "编辑推荐分组" : "新建推荐分组"}
      open={open}
      onCancel={onCancel}
      onOk={onOk}
      confirmLoading={saving}
      destroyOnHidden
    >
      <Form form={form} layout="vertical">
        <Form.Item name="sourceMode" label="分组内容来源">
          <Radio.Group
            optionType="button"
            buttonStyle="solid"
            options={[
              { label: "榜单同步", value: "chart" },
              { label: "站点分类", value: "category" },
              { label: "自选专题", value: "manual" },
            ]}
          />
        </Form.Item>
        <Form.Item
          name="title"
          label="分组名称"
          rules={[{ required: true, message: "请输入分组名称" }]}
        >
          <Input placeholder="例如：院线热映、电影、站长精选" maxLength={32} />
        </Form.Item>
        <Form.Item name="enabled" label="首页显示" valuePropName="checked">
          <Switch checkedChildren="显示" unCheckedChildren="隐藏" />
        </Form.Item>
        <Form.Item noStyle shouldUpdate={(prev, cur) => prev.sourceMode !== cur.sourceMode}>
          {({ getFieldValue, setFieldValue }) => {
            const mode = getFieldValue("sourceMode");
            if (mode === "chart") {
              return (
                <>
                  <Form.Item
                    name="syncChartKey"
                    label="绑定热门榜单"
                    rules={[{ required: true, message: "请选择要绑定的热门榜单" }]}
                  >
                    <Select
                      options={chartOptions.map((o) => ({ value: o.key, label: o.title }))}
                    />
                  </Form.Item>
                  <Form.Item
                    name="autoSync"
                    label="自动定时刷新（每 6 小时自动跟随榜单更新）"
                    valuePropName="checked"
                  >
                    <Switch checkedChildren="自动" unCheckedChildren="仅手动" />
                  </Form.Item>
                  <Form.Item name="syncLimit" label="匹配影片上限（1-24）">
                    <InputNumber min={1} max={24} style={{ width: "100%" }} />
                  </Form.Item>
                </>
              );
            }
            if (mode === "category") {
              return (
                <>
                  <Form.Item
                    name="categoryId"
                    label="绑定当前站一级分类"
                    rules={[{ required: true, message: "请选择要绑定的站点分类" }]}
                  >
                    <Select
                      placeholder={
                        categoryOptions.length > 0
                          ? "选择当前站在分类管理中开启的一级分类"
                          : "当前站暂无可见的一级分类"
                      }
                      options={categoryOptions.map((c) => ({
                        value: c.id,
                        label: c.filmCount ? `${c.name}（${c.filmCount} 部）` : c.name,
                      }))}
                      onChange={(catId: number) => {
                        const chosen = categoryOptions.find((c) => c.id === catId);
                        const curTitle = String(getFieldValue("title") || "").trim();
                        if (chosen && !curTitle) {
                          setFieldValue("title", chosen.name);
                        }
                      }}
                    />
                  </Form.Item>
                  <Alert
                    type="info"
                    showIcon
                    title="首页将以分类双栏楼层展示，并联动「分类管理」显隐：分类管理隐藏则首页同步隐藏；在此处关闭仅影响首页，不影响分类页与导航。"
                  />
                </>
              );
            }
            return (
              <Alert
                type="info"
                showIcon
                title="自选专题不绑定外部榜单或分类楼层，可通过「添加影片」或「一键排片」维护片单，不会被定时同步覆盖。"
              />
            );
          }}
        </Form.Item>
      </Form>
    </Modal>
  );
}
