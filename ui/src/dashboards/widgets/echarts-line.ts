import { graphic, init, use, type ECharts } from 'echarts/core';
import { LineChart } from 'echarts/charts';
import {
  AxisPointerComponent,
  DataZoomSliderComponent,
  GridComponent,
  LegendComponent,
  TooltipComponent,
} from 'echarts/components';
import { CanvasRenderer } from 'echarts/renderers';
import type { EChartsOption, SeriesOption, XAXisComponentOption, YAXisComponentOption } from 'echarts';

use([
  AxisPointerComponent,
  CanvasRenderer,
  DataZoomSliderComponent,
  GridComponent,
  LegendComponent,
  LineChart,
  TooltipComponent,
]);

export { graphic, init };
export type { ECharts, EChartsOption, SeriesOption, XAXisComponentOption, YAXisComponentOption };
