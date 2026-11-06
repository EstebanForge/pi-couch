import { renderToString } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { SystemMetricsSample } from "../api";
import { cpuGraph, formatBytes, SystemMetricsView } from "./SystemMetrics";

describe("formatBytes", () => {
	it("uses readable binary units", () => {
		expect(formatBytes(0)).toBe("0 B");
		expect(formatBytes(1024)).toBe("1.0 KB");
		expect(formatBytes(6 * 1024 ** 3)).toBe("6.0 GB");
		expect(formatBytes(100 * 1024 ** 3)).toBe("100 GB");
	});
});

describe("cpuGraph", () => {
	it("scales samples across the chart", () => {
		const graph = cpuGraph([0, 50, 100]);
		expect(graph.line).toBe("M0.00,64.00 L150.00,32.00 L300.00,0.00");
		expect(graph.area).toBe("M0.00,64.00 L150.00,32.00 L300.00,0.00 L300.00,64 L0.00,64 Z");
		expect(graph.last).toEqual({ x: 300, y: 0 });
	});
});

describe("SystemMetricsView", () => {
	it("renders the current RAM meter and CPU history", () => {
		const sample = (cpu: number, usedGB: number): SystemMetricsSample => ({
			cpu_percent: cpu,
			memory_used_bytes: usedGB * 1024 ** 3,
			memory_total_bytes: 16 * 1024 ** 3,
			sampled_at: "2026-11-06T12:00:00Z",
		});
		const html = renderToString(
			<SystemMetricsView history={[sample(22.4, 6), sample(68.9, 8)]} error="" />,
		);
		expect(html).toContain('aria-label="CPU history, current 68.9%"');
		expect(html).toContain('aria-valuenow="50"');
		expect(html).toContain("8.0 GB");
		expect(html).toContain("16.0 GB");
	});
});
