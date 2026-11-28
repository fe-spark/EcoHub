"use client";

import { useEffect, useRef } from "react";
import * as echarts from "echarts/core";
import { LineChart, BarChart, PieChart } from "echarts/charts";
import {
  GridComponent,
  TooltipComponent,
  LegendComponent,
} from "echarts/components";
import { CanvasRenderer } from "echarts/renderers";
import type { EChartsCoreOption } from "echarts/core";
import { useThemeMode } from "@/components/theme/GlobalThemeProvider";

echarts.use([
  LineChart,
  BarChart,
  PieChart,
  GridComponent,
  TooltipComponent,
  LegendComponent,
  CanvasRenderer,
]);

interface EChartBoxProps {
  option: EChartsCoreOption;
  height?: number;
  /** 撑满父级 flex 容器剩余高度，避免图表自身画布反撑导致容器无法收缩 */
  fill?: boolean;
  minHeight?: number;
}

export default function EChartBox({
  option,
  height = 260,
  fill = false,
  minHeight,
}: EChartBoxProps) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const chartRef = useRef<echarts.ECharts | null>(null);
  const { effective } = useThemeMode();

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;

    const instance = echarts.init(el, effective === "dark" ? "dark" : undefined, {
      renderer: "canvas",
    });
    chartRef.current = instance;

    const ro = new ResizeObserver(() => {
      instance.resize();
    });
    ro.observe(el);

    return () => {
      ro.disconnect();
      instance.dispose();
      chartRef.current = null;
    };
  }, [effective]);

  useEffect(() => {
    const instance = chartRef.current;
    if (!instance) return;
    instance.setOption(
      {
        backgroundColor: "transparent",
        ...option,
      },
      { notMerge: true, lazyUpdate: true },
    );
  }, [option, effective]);

  if (fill) {
    return (
      <div
        style={{
          position: "relative",
          width: "100%",
          flex: 1,
          minHeight: minHeight ?? height,
        }}
      >
        <div ref={containerRef} style={{ position: "absolute", inset: 0 }} />
      </div>
    );
  }

  return <div ref={containerRef} style={{ width: "100%", height }} />;
}
