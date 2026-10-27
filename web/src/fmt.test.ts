import { describe, expect, it } from "vitest";
import { thousand, tildify } from "./fmt";

describe("thousand", () => {
	it("formats integers with Chilean dot separators", () => {
		expect(thousand(1234567)).toBe("1.234.567");
		expect(thousand(662)).toBe("662");
	});
});

describe("tildify", () => {
	it("shortens the server home prefix", () => {
		expect(tildify("/home/maria/Dev/app", "/home/maria")).toBe("~/Dev/app");
		expect(tildify("/home/maria", "/home/maria")).toBe("~");
		expect(tildify("/etc", "/home/maria")).toBe("/etc");
	});

	it("falls back to a generic /home/<user> match", () => {
		expect(tildify("/home/maria/Dev/app")).toBe("~/Dev/app");
		expect(tildify("/home/maria/x")).toBe("~/x");
	});
});
