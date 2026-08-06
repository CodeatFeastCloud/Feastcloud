#!/usr/bin/env node

import { spawn } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { basename, join, resolve } from 'node:path';

const targetUrl = process.argv[2] ?? 'http://127.0.0.1:5173/?view=overview';
const artifactDirectory = resolve(process.argv[3] ?? 'artifacts/visual-smoke');
const expectedSelector = process.argv[4] ?? '.executive-dashboard';
const forbiddenText = process.argv[5] ?? '';
const chromeCandidates = [
  process.env.CHROME_PATH,
  '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
  '/Applications/Chromium.app/Contents/MacOS/Chromium',
  'google-chrome',
  'chromium',
].filter(Boolean);
const chromePath = chromeCandidates.find((candidate) => candidate.includes('/') ? existsSync(candidate) : true);

if (!chromePath) {
  throw new Error('Chrome or Chromium was not found. Set CHROME_PATH to its executable.');
}

mkdirSync(artifactDirectory, { recursive: true });

const sleep = (milliseconds) => new Promise((resolvePromise) => setTimeout(resolvePromise, milliseconds));

async function waitForDevToolsPort(profileDirectory, chrome, stderr) {
  const portFile = join(profileDirectory, 'DevToolsActivePort');
  const deadline = Date.now() + 15_000;
  while (Date.now() < deadline) {
    if (chrome.exitCode !== null) {
      throw new Error(`Chrome exited before DevTools became available.\n${stderr.value}`);
    }
    if (existsSync(portFile)) {
      const [portText] = readFileSync(portFile, 'utf8').trim().split(/\r?\n/);
      const port = Number(portText);
      if (Number.isInteger(port) && port > 0) return port;
    }
    await sleep(50);
  }
  throw new Error(`Timed out waiting for Chrome DevTools.\n${stderr.value}`);
}

class CdpClient {
  constructor(url) {
    this.url = url;
    this.nextId = 1;
    this.pending = new Map();
    this.listeners = new Map();
  }

  async connect() {
    this.socket = new WebSocket(this.url);
    await new Promise((resolvePromise, reject) => {
      this.socket.addEventListener('open', resolvePromise, { once: true });
      this.socket.addEventListener('error', () => reject(new Error(`Could not connect to ${this.url}`)), { once: true });
    });
    this.socket.addEventListener('message', (event) => {
      const message = JSON.parse(String(event.data));
      if (message.id) {
        const pending = this.pending.get(message.id);
        if (!pending) return;
        this.pending.delete(message.id);
        if (message.error) pending.reject(new Error(`${pending.method}: ${message.error.message}`));
        else pending.resolve(message.result ?? {});
        return;
      }
      for (const listener of this.listeners.get(message.method) ?? []) listener(message.params ?? {});
    });
  }

  send(method, params = {}) {
    const id = this.nextId++;
    return new Promise((resolvePromise, reject) => {
      this.pending.set(id, { method, resolve: resolvePromise, reject });
      this.socket.send(JSON.stringify({ id, method, params }));
    });
  }

  on(method, listener) {
    this.listeners.set(method, [...(this.listeners.get(method) ?? []), listener]);
  }

  close() {
    this.socket?.close();
  }
}

function consoleText(args = []) {
  return args.map((argument) => {
    if (argument.value !== undefined) return String(argument.value);
    if (argument.description) return argument.description;
    return argument.type ?? 'unknown';
  }).join(' ');
}

async function createTarget(port) {
  const response = await fetch(`http://127.0.0.1:${port}/json/new?${encodeURIComponent('about:blank')}`, {
    method: 'PUT',
  });
  if (!response.ok) throw new Error(`Chrome target creation failed with HTTP ${response.status}`);
  return response.json();
}

async function waitForHydration(client, label) {
  const deadline = Date.now() + 25_000;
  let state;
  while (Date.now() < deadline) {
    const evaluation = await client.send('Runtime.evaluate', {
      expression: `(() => ({
        readyState: document.readyState,
        loadingElementPresent: Boolean(document.querySelector('.loading-state')),
        restoringTextPresent: Boolean(document.body?.innerText.includes('Restoring this device')),
        targetPresent: Boolean(document.querySelector(${JSON.stringify(expectedSelector)})),
        forbiddenTextPresent: ${JSON.stringify(Boolean(forbiddenText))}
          && Boolean(document.body?.innerText.includes(${JSON.stringify(forbiddenText)})),
        pairingPresent: Boolean(document.querySelector('.pairing-shell')),
        title: document.title,
        bodyText: document.body?.innerText.slice(0, 500) ?? ''
      }))()`,
      returnByValue: true,
    });
    state = evaluation.result?.value;
    if (
      state?.readyState === 'complete' &&
      !state.loadingElementPresent &&
      !state.restoringTextPresent &&
      state.targetPresent
    ) return state;
    if (state?.pairingPresent) throw new Error(`${label}: device pairing blocked the dashboard smoke test`);
    await sleep(100);
  }
  throw new Error(`${label}: expected view did not hydrate in 25 seconds. Last state: ${JSON.stringify(state)}`);
}

async function captureScenario(port, scenario) {
  const target = await createTarget(port);
  const client = new CdpClient(target.webSocketDebuggerUrl);
  await client.connect();

  const consoleErrors = [];
  const consoleWarnings = [];
  const runtimeExceptions = [];
  const logErrors = [];

  client.on('Runtime.consoleAPICalled', (event) => {
    const item = { type: event.type, text: consoleText(event.args), timestamp: event.timestamp };
    if (event.type === 'error' || event.type === 'assert') consoleErrors.push(item);
    else if (event.type === 'warning') consoleWarnings.push(item);
  });
  client.on('Runtime.exceptionThrown', (event) => {
    runtimeExceptions.push({
      text: event.exceptionDetails?.text ?? 'Uncaught exception',
      description: event.exceptionDetails?.exception?.description,
      url: event.exceptionDetails?.url,
      lineNumber: event.exceptionDetails?.lineNumber,
      columnNumber: event.exceptionDetails?.columnNumber,
    });
  });
  client.on('Log.entryAdded', ({ entry }) => {
    if (entry?.level === 'error') logErrors.push({ text: entry.text, source: entry.source, url: entry.url });
  });

  await Promise.all([
    client.send('Page.enable'),
    client.send('Runtime.enable'),
    client.send('Log.enable'),
  ]);
  await client.send('Emulation.setDeviceMetricsOverride', {
    width: scenario.width,
    height: scenario.height,
    deviceScaleFactor: scenario.deviceScaleFactor,
    mobile: scenario.mobile,
    screenWidth: scenario.width,
    screenHeight: scenario.height,
  });
  await client.send('Emulation.setTouchEmulationEnabled', {
    enabled: scenario.mobile,
    maxTouchPoints: scenario.mobile ? 5 : 1,
  });
  await client.send('Page.navigate', { url: targetUrl });
  const hydration = await waitForHydration(client, scenario.name);
  await client.send('Runtime.evaluate', {
    expression: 'document.fonts?.ready',
    awaitPromise: true,
    returnByValue: true,
  });
  await sleep(300);

  const finalEvaluation = await client.send('Runtime.evaluate', {
    expression: `(() => ({
      targetPresent: Boolean(document.querySelector(${JSON.stringify(expectedSelector)})),
      forbiddenTextPresent: ${JSON.stringify(Boolean(forbiddenText))}
        && Boolean(document.body?.innerText.includes(${JSON.stringify(forbiddenText)})),
      bodyText: document.body?.innerText.slice(0, 2_000) ?? ''
    }))()`,
    returnByValue: true,
  });
  const finalState = finalEvaluation.result?.value ?? {};

  const layout = await client.send('Page.getLayoutMetrics');
  const screenshot = await client.send('Page.captureScreenshot', {
    format: 'png',
    fromSurface: true,
    captureBeyondViewport: true,
  });
  const screenshotPath = join(artifactDirectory, `${scenario.name}.png`);
  writeFileSync(screenshotPath, Buffer.from(screenshot.data, 'base64'));

  await fetch(`http://127.0.0.1:${port}/json/close/${target.id}`, { method: 'PUT' }).catch(() => {});
  client.close();

  return {
    name: scenario.name,
    viewport: {
      width: scenario.width,
      height: scenario.height,
      deviceScaleFactor: scenario.deviceScaleFactor,
      mobile: scenario.mobile,
    },
    screenshot: screenshotPath,
    contentSize: layout.contentSize,
    hydration: {
      readyState: hydration.readyState,
      loadingElementPresent: hydration.loadingElementPresent,
      restoringTextPresent: hydration.restoringTextPresent,
      expectedSelector,
      targetPresent: finalState.targetPresent,
      forbiddenText,
      forbiddenTextPresent: finalState.forbiddenTextPresent,
      title: hydration.title,
    },
    consoleErrors,
    consoleWarnings,
    runtimeExceptions,
    logErrors,
  };
}

const profileDirectory = mkdtempSync(join(tmpdir(), 'feastcloud-cdp-'));
const stderr = { value: '' };
const chrome = spawn(chromePath, [
  '--headless=new',
  '--disable-gpu',
  '--hide-scrollbars',
  '--no-first-run',
  '--no-default-browser-check',
  '--remote-debugging-port=0',
  `--user-data-dir=${profileDirectory}`,
  '--window-size=1440,1100',
  'about:blank',
], { stdio: ['ignore', 'ignore', 'pipe'] });
chrome.stderr.setEncoding('utf8');
chrome.stderr.on('data', (chunk) => { stderr.value += chunk; });

let report;
try {
  const port = await waitForDevToolsPort(profileDirectory, chrome, stderr);
  const scenarios = [
    { name: 'desktop', width: 1440, height: 1100, deviceScaleFactor: 1, mobile: false },
    { name: 'mobile', width: 390, height: 844, deviceScaleFactor: 2, mobile: true },
  ];
  const results = [];
  for (const scenario of scenarios) results.push(await captureScenario(port, scenario));
  const errorCount = results.reduce(
    (count, result) => count + result.consoleErrors.length + result.runtimeExceptions.length + result.logErrors.length,
    0,
  );
  report = {
    generatedAt: new Date().toISOString(),
    targetUrl,
    browser: basename(chromePath),
    passed: errorCount === 0 && results.every(
      (result) => result.hydration.targetPresent && !result.hydration.forbiddenTextPresent,
    ),
    errorCount,
    scenarios: results,
  };
} catch (error) {
  report = {
    generatedAt: new Date().toISOString(),
    targetUrl,
    browser: basename(chromePath),
    passed: false,
    fatalError: error instanceof Error ? error.stack ?? error.message : String(error),
    chromeStderr: stderr.value.slice(-4_000),
  };
} finally {
  chrome.kill('SIGTERM');
  await Promise.race([
    new Promise((resolvePromise) => {
      if (chrome.exitCode !== null) resolvePromise();
      else chrome.once('exit', resolvePromise);
    }),
    sleep(1_500),
  ]);
  try {
    rmSync(profileDirectory, { recursive: true, force: true, maxRetries: 5, retryDelay: 100 });
  } catch (error) {
    report.cleanupWarning = error instanceof Error ? error.message : String(error);
  }
}

const reportPath = join(artifactDirectory, 'report.json');
writeFileSync(reportPath, `${JSON.stringify(report, null, 2)}\n`);
process.stdout.write(`${JSON.stringify({ ...report, reportPath }, null, 2)}\n`);
if (!report.passed) process.exitCode = 1;
