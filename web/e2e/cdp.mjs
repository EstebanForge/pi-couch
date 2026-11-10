import { spawn, spawnSync } from "node:child_process";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

function chromeBinary() {
	if (process.env.CHROME_BIN) return process.env.CHROME_BIN;
	for (const candidate of [
		"google-chrome",
		"google-chrome-stable",
		"chromium",
		"chromium-browser",
	]) {
		const probe = spawnSync(candidate, ["--version"], { stdio: "ignore" });
		if (!probe.error) return candidate;
	}
	throw new Error("Chrome/Chromium not found; set CHROME_BIN to its executable");
}

async function connect(url) {
	const ws = new WebSocket(url);
	await new Promise((resolve, reject) => {
		ws.addEventListener("open", resolve, { once: true });
		ws.addEventListener("error", () => reject(new Error("CDP WebSocket failed")), {
			once: true,
		});
	});
	let nextID = 0;
	const pending = new Map();
	const exceptions = [];
	ws.addEventListener("message", (event) => {
		const message = JSON.parse(String(event.data));
		if (message.method === "Runtime.exceptionThrown") {
			exceptions.push(message.params.exceptionDetails);
		}
		if (!message.id) return;
		const waiter = pending.get(message.id);
		if (!waiter) return;
		pending.delete(message.id);
		clearTimeout(waiter.timer);
		if (message.error) waiter.reject(new Error(JSON.stringify(message.error)));
		else waiter.resolve(message.result);
	});

	const send = (method, params = {}) =>
		new Promise((resolve, reject) => {
			const id = ++nextID;
			const timer = setTimeout(() => {
				pending.delete(id);
				reject(new Error(`CDP timeout: ${method}`));
			}, 15_000);
			pending.set(id, { resolve, reject, timer });
			ws.send(JSON.stringify({ id, method, params }));
		});

	const evaluate = async (expression) => {
		const result = await send("Runtime.evaluate", {
			expression,
			awaitPromise: true,
			returnByValue: true,
			userGesture: true,
		});
		if (result.exceptionDetails) {
			throw new Error(
				result.exceptionDetails.exception?.description ??
					result.exceptionDetails.text ??
					"page evaluation failed",
			);
		}
		return result.result.value;
	};

	const waitFor = async (expression, label = expression, timeoutMs = 12_000) => {
		const deadline = Date.now() + timeoutMs;
		while (Date.now() < deadline) {
			if (await evaluate(`Boolean(${expression})`)) return;
			await sleep(100);
		}
		const text = await evaluate("document.body?.innerText?.slice(0, 2000) ?? ''");
		throw new Error(`timed out waiting for ${label}\npage text:\n${text}`);
	};

	const navigate = async (url) => {
		exceptions.length = 0;
		await send("Page.navigate", { url });
		await waitFor("document.readyState === 'complete'", `load ${url}`);
	};

	const pointFor = async (selector, text, exact) => {
		const expression = `(async () => {
			const matches = [...document.querySelectorAll(${JSON.stringify(selector)})].filter((node) => {
				if (${text === undefined}) return true;
				const value = (node.textContent || "").replace(/\\s+/g, " ").trim();
				return ${exact} ? value === ${JSON.stringify(text)} : value.includes(${JSON.stringify(text)});
			});
			const point = (element) => {
				const rect = element.getBoundingClientRect();
				return { x: rect.left + rect.width / 2, y: rect.top + rect.height / 2 };
			};
			const tappable = (element) => {
				if (!element.checkVisibility({ checkOpacity: true, checkVisibilityCSS: true })) return false;
				const p = point(element);
				if (p.x < 0 || p.y < 0 || p.x >= innerWidth || p.y >= innerHeight) return false;
				const hit = document.elementFromPoint(p.x, p.y);
				return hit && (element.contains(hit) || hit.contains(element));
			};
			let element = matches.find(tappable);
			if (!element) {
				for (const candidate of matches) {
					if (candidate.closest('[aria-hidden="true"], .pointer-events-none')) continue;
					candidate.scrollIntoView({ block: "center", inline: "center" });
					await new Promise((resolve) => setTimeout(resolve, 300));
					if (tappable(candidate)) {
						element = candidate;
						break;
					}
				}
			}
			if (!element) return { error: ${JSON.stringify(`no tappable element: ${selector} ${text ?? ""}`)} };
			if (element.disabled) return { error: "element is disabled: " + (element.textContent || selector) };
			return point(element);
		})()`;
		const point = await evaluate(expression);
		if (point.error) throw new Error(point.error);
		return point;
	};

	const tapPoint = async ({ x, y }) => {
		const touchPoints = [{ x: Math.round(x), y: Math.round(y), radiusX: 8, radiusY: 8, force: 1 }];
		await send("Input.dispatchTouchEvent", { type: "touchStart", touchPoints });
		await sleep(60);
		await send("Input.dispatchTouchEvent", { type: "touchEnd", touchPoints: [] });
		await sleep(80);
	};

	// Hold past the app's LONG_PRESS_MS (500) so the long-press fires
	// instead of the tap.
	const holdPoint = async ({ x, y }) => {
		const touchPoints = [{ x: Math.round(x), y: Math.round(y), radiusX: 8, radiusY: 8, force: 1 }];
		await send("Input.dispatchTouchEvent", { type: "touchStart", touchPoints });
		await sleep(750);
		await send("Input.dispatchTouchEvent", { type: "touchEnd", touchPoints: [] });
		await sleep(80);
	};

	const tap = async (selector) => tapPoint(await pointFor(selector, undefined, true));
	const tapText = async (text, selector = "button", exact = true) =>
		tapPoint(await pointFor(selector, text, exact));

	const fill = async (selector, text) => {
		await tap(selector);
		await send("Input.dispatchKeyEvent", {
			type: "keyDown",
			key: "a",
			code: "KeyA",
			modifiers: 2,
			windowsVirtualKeyCode: 65,
		});
		await send("Input.dispatchKeyEvent", {
			type: "keyUp",
			key: "a",
			code: "KeyA",
			modifiers: 2,
			windowsVirtualKeyCode: 65,
		});
		await send("Input.insertText", { text });
	};

	const pressEnter = async () => {
		await send("Input.dispatchKeyEvent", {
			type: "keyDown",
			key: "Enter",
			code: "Enter",
			text: "\r",
			windowsVirtualKeyCode: 13,
		});
		await send("Input.dispatchKeyEvent", {
			type: "keyUp",
			key: "Enter",
			code: "Enter",
			windowsVirtualKeyCode: 13,
		});
	};

	const setFiles = async (expression, files) => {
		const result = await send("Runtime.evaluate", { expression, returnByValue: false });
		if (!result.result.objectId) throw new Error("file input expression returned no element");
		await send("DOM.enable");
		await send("DOM.getDocument", { depth: 0 });
		const { nodeId } = await send("DOM.requestNode", { objectId: result.result.objectId });
		await send("DOM.setFileInputFiles", { nodeId, files });
		await send("Runtime.releaseObject", { objectId: result.result.objectId });
	};

	const screenshot = async (artifacts, name) => {
		const { data } = await send("Page.captureScreenshot", {
			format: "png",
			fromSurface: true,
			captureBeyondViewport: false,
		});
		const path = join(artifacts, `${name}.png`);
		await writeFile(path, Buffer.from(data, "base64"));
		return path;
	};

	const close = () => {
		for (const waiter of pending.values()) {
			clearTimeout(waiter.timer);
			waiter.reject(new Error("CDP closed"));
		}
		pending.clear();
		ws.close();
	};

	return {
		send,
		evaluate,
		waitFor,
		navigate,
		tap,
		tapText,
		holdText: async (text, selector = "div", exact = true) =>
			holdPoint(await pointFor(selector, text, exact)),
		holdSel: async (selector) => holdPoint(await pointFor(selector)),
		fill,
		pressEnter,
		setFiles,
		screenshot,
		close,
		exceptions,
	};
}

export async function launchBrowser(artifacts) {
	const profile = await mkdtemp(join(tmpdir(), "couch-pi-chrome-"));
	const browser = spawn(
		chromeBinary(),
		[
			"--headless=new",
			"--remote-debugging-port=0",
			`--user-data-dir=${profile}`,
			"--no-first-run",
			"--no-sandbox",
			"--disable-dev-shm-usage",
			"--disable-background-networking",
			"--window-size=393,873",
			"about:blank",
		],
		{
			stdio: ["ignore", "ignore", "pipe"],
			// Headless chromium wedges every http navigation on the
			// session bus (Fedora's portal stack answers lazily); the
			// bus var's absence makes chrome skip dbus entirely.
			env: Object.fromEntries(
				Object.entries(process.env).filter(([k]) => k !== "DBUS_SESSION_BUS_ADDRESS"),
			),
		},
	);
	let stderr = "";
	let debuggingURL;
	browser.stderr.on("data", (chunk) => {
		stderr += String(chunk);
		const match = stderr.match(/DevTools listening on (ws:\/\/[^\s]+)/);
		if (match) debuggingURL = match[1];
	});
	const deadline = Date.now() + 15_000;
	while (!debuggingURL && Date.now() < deadline) {
		if (browser.exitCode !== null) throw new Error(`Chrome exited: ${stderr.slice(-2000)}`);
		await sleep(100);
	}
	if (!debuggingURL) {
		browser.kill("SIGKILL");
		throw new Error(`Chrome did not expose CDP: ${stderr.slice(-2000)}`);
	}
	const port = new URL(debuggingURL).port;
	let target;
	while (!target && Date.now() < deadline) {
		try {
			const targets = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json();
			target = targets.find((item) => item.type === "page");
		} catch {
			// Chrome opened the browser socket before the initial page target.
		}
		if (!target) await sleep(100);
	}
	if (!target) {
		browser.kill("SIGKILL");
		throw new Error("Chrome did not expose a page target");
	}

	const page = await connect(target.webSocketDebuggerUrl);
	await page.send("Page.enable");
	await page.send("Runtime.enable");
	await page.send("Emulation.setDeviceMetricsOverride", {
		width: 393,
		height: 873,
		deviceScaleFactor: 2,
		mobile: true,
		screenWidth: 393,
		screenHeight: 873,
	});
	await page.send("Emulation.setTouchEmulationEnabled", { enabled: true, maxTouchPoints: 5 });
	await page.send("Emulation.setEmitTouchEventsForMouse", {
		enabled: true,
		configuration: "mobile",
	});
	await page.send("Emulation.setUserAgentOverride", {
		userAgent:
			"Mozilla/5.0 (Linux; Android 15; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/156.0.0.0 Mobile Safari/537.36",
		platform: "Linux armv8l",
	});
	await mkdir(artifacts, { recursive: true });
	await writeFile(join(artifacts, "chrome.log"), stderr);

	return {
		page,
		close: async () => {
			page.close();
			const exited = new Promise((resolve) => browser.once("exit", resolve));
			browser.kill("SIGTERM");
			await Promise.race([exited, sleep(3000)]);
			if (browser.exitCode === null && browser.signalCode === null) {
				browser.kill("SIGKILL");
				await exited;
			}
			await rm(profile, { recursive: true, force: true, maxRetries: 5, retryDelay: 100 });
		},
	};
}
