# SKILL: CreepJS Fingerprint Obfuscation

## Overview

This document describes a browser fingerprint collection payload inspired by CreepJS
techniques, including anti-detection measures, payload obfuscation, exfiltration
strategies, and server-side validation. The system collects stable browser fingerprint
signals while evading common anti-fingerprinting defenses.

**Version Pins:**
- javascript-obfuscator: 4.1.1
- Node.js: 22 (build environment)

---

## 1. Fingerprint Collection Payload

### 1.1 Core Architecture

The collector uses a modular architecture where each fingerprint signal is gathered
by an independent collector function. Collectors are shuffled and executed with random
timing jitter to avoid detection by behavioral analysis tools.

```javascript
// fingerprint.js - Core fingerprint collection payload
(function() {
  'use strict';

  const collectors = [];
  const results = {};

  // Utility: generate random delay between min and max ms
  function randomDelay(min, max) {
    return new Promise(resolve => {
      setTimeout(resolve, Math.floor(Math.random() * (max - min + 1)) + min);
    });
  }

  // Utility: shuffle array using Fisher-Yates
  function shuffle(array) {
    for (let i = array.length - 1; i > 0; i--) {
      const j = Math.floor(Math.random() * (i + 1));
      [array[i], array[j]] = [array[j], array[i]];
    }
    return array;
  }

  // Utility: safe property access with error boundary
  function safeGet(fn) {
    try { return fn(); } catch (e) { return undefined; }
  }

  // --- Canvas Fingerprinting with Noise Injection Detection ---
  collectors.push({
    name: 'canvas',
    collect: function() {
      const canvas = document.createElement('canvas');
      canvas.width = 256;
      canvas.height = 128;
      const ctx = canvas.getContext('2d');

      ctx.fillStyle = '#f60';
      ctx.fillRect(10, 10, 100, 50);
      ctx.fillStyle = '#069';
      ctx.font = '15px Arial';
      ctx.fillText('GhostRoute FP v1.0', 2, 90);
      ctx.fillStyle = 'rgba(102, 204, 0, 0.7)';
      ctx.arc(75, 75, 50, 0, Math.PI * 2, true);
      ctx.fill();
      ctx.strokeStyle = '#3a7bd5';
      ctx.beginPath();
      ctx.moveTo(0, 0);
      ctx.lineTo(256, 128);
      ctx.stroke();

      const dataUrl = canvas.toDataURL('image/png');

      // Noise injection detection: draw same content twice, compare
      const canvas2 = document.createElement('canvas');
      canvas2.width = 256;
      canvas2.height = 128;
      const ctx2 = canvas2.getContext('2d');
      ctx2.fillStyle = '#f60';
      ctx2.fillRect(10, 10, 100, 50);
      ctx2.fillStyle = '#069';
      ctx2.font = '15px Arial';
      ctx2.fillText('GhostRoute FP v1.0', 2, 90);
      ctx2.fillStyle = 'rgba(102, 204, 0, 0.7)';
      ctx2.arc(75, 75, 50, 0, Math.PI * 2, true);
      ctx2.fill();
      ctx2.strokeStyle = '#3a7bd5';
      ctx2.beginPath();
      ctx2.moveTo(0, 0);
      ctx2.lineTo(256, 128);
      ctx2.stroke();

      const dataUrl2 = canvas2.toDataURL('image/png');
      const noisePoisoned = dataUrl !== dataUrl2;

      return {
        hash: dataUrl.slice(-50),
        noisePoisoned: noisePoisoned,
        length: dataUrl.length
      };
    }
  });

  // --- WebGL Renderer/Vendor Extraction ---
  collectors.push({
    name: 'webgl',
    collect: function() {
      const canvas = document.createElement('canvas');
      const gl = canvas.getContext('webgl') || canvas.getContext('experimental-webgl');
      if (!gl) return { supported: false };

      const debugInfo = gl.getExtension('WEBGL_debug_renderer_info');
      const vendor = debugInfo ? gl.getParameter(debugInfo.UNMASKED_VENDOR_WEBGL) : 'unknown';
      const renderer = debugInfo ? gl.getParameter(debugInfo.UNMASKED_RENDERER_WEBGL) : 'unknown';

      return {
        supported: true,
        vendor: vendor,
        renderer: renderer,
        version: gl.getParameter(gl.VERSION),
        shadingLanguageVersion: gl.getParameter(gl.SHADING_LANGUAGE_VERSION),
        maxTextureSize: gl.getParameter(gl.MAX_TEXTURE_SIZE),
        maxViewportDims: gl.getParameter(gl.MAX_VIEWPORT_DIMS)
      };
    }
  });

  // --- AudioContext Fingerprinting ---
  collectors.push({
    name: 'audio',
    collect: function() {
      return new Promise(function(resolve) {
        try {
          const AudioContext = window.AudioContext || window.webkitAudioContext;
          if (!AudioContext) { resolve({ supported: false }); return; }

          const context = new AudioContext();
          const oscillator = context.createOscillator();
          const compressor = context.createDynamicsCompressor();
          const analyser = context.createAnalyser();

          oscillator.type = 'triangle';
          oscillator.frequency.setValueAtTime(10000, context.currentTime);

          compressor.threshold.setValueAtTime(-50, context.currentTime);
          compressor.knee.setValueAtTime(40, context.currentTime);
          compressor.ratio.setValueAtTime(12, context.currentTime);
          compressor.attack.setValueAtTime(0, context.currentTime);
          compressor.release.setValueAtTime(0.25, context.currentTime);

          oscillator.connect(compressor);
          compressor.connect(analyser);
          analyser.connect(context.destination);

          oscillator.start(0);

          setTimeout(function() {
            const freqData = new Float32Array(analyser.frequencyBinCount);
            analyser.getFloatFrequencyData(freqData);

            let hash = 0;
            for (let i = 0; i < freqData.length; i++) {
              if (freqData[i] !== -Infinity) {
                hash += Math.abs(freqData[i]);
              }
            }

            oscillator.stop();
            context.close();

            resolve({
              supported: true,
              hash: hash.toFixed(6),
              sampleRate: context.sampleRate,
              channelCount: context.destination.maxChannelCount
            });
          }, 100);
        } catch (e) {
          resolve({ supported: false, error: e.message });
        }
      });
    }
  });

  // --- Font Enumeration via Canvas Measurement ---
  collectors.push({
    name: 'fonts',
    collect: function() {
      const baseFonts = ['monospace', 'sans-serif', 'serif'];
      const testFonts = [
        'Arial', 'Verdana', 'Times New Roman', 'Courier New', 'Georgia',
        'Palatino', 'Garamond', 'Comic Sans MS', 'Trebuchet MS', 'Arial Black',
        'Impact', 'Lucida Console', 'Tahoma', 'Lucida Sans', 'Century Gothic',
        'Bookman Old Style', 'Candara', 'Calibri', 'Cambria', 'Consolas',
        'Franklin Gothic', 'Futura', 'Geneva', 'Helvetica', 'Monaco',
        'Optima', 'Segoe UI', 'Roboto', 'Ubuntu', 'Droid Sans'
      ];
      const testString = 'mmmmmmmmmmlli';
      const testSize = '72px';
      const canvas = document.createElement('canvas');
      const ctx = canvas.getContext('2d');

      function getTextWidth(fontFamily) {
        ctx.font = testSize + ' ' + fontFamily;
        return ctx.measureText(testString).width;
      }

      const baseWidths = {};
      baseFonts.forEach(function(font) {
        baseWidths[font] = getTextWidth(font);
      });

      const detected = [];
      testFonts.forEach(function(font) {
        for (let i = 0; i < baseFonts.length; i++) {
          const width = getTextWidth(font + ',' + baseFonts[i]);
          if (width !== baseWidths[baseFonts[i]]) {
            detected.push(font);
            break;
          }
        }
      });

      return { detected: detected, count: detected.length };
    }
  });

  // --- Screen and Window Property Collection ---
  collectors.push({
    name: 'screen',
    collect: function() {
      return {
        width: screen.width,
        height: screen.height,
        availWidth: screen.availWidth,
        availHeight: screen.availHeight,
        colorDepth: screen.colorDepth,
        pixelDepth: screen.pixelDepth,
        pixelRatio: window.devicePixelRatio,
        outerWidth: window.outerWidth,
        outerHeight: window.outerHeight,
        innerWidth: window.innerWidth,
        innerHeight: window.innerHeight,
        screenX: window.screenX,
        screenY: window.screenY,
        widthDiscrepancy: window.outerWidth - window.innerWidth,
        heightDiscrepancy: window.outerHeight - window.innerHeight
      };
    }
  });

  // --- Navigator Properties ---
  collectors.push({
    name: 'navigator',
    collect: function() {
      return {
        hardwareConcurrency: navigator.hardwareConcurrency,
        deviceMemory: navigator.deviceMemory,
        platform: navigator.platform,
        maxTouchPoints: navigator.maxTouchPoints,
        languages: navigator.languages ? Array.from(navigator.languages) : [],
        language: navigator.language,
        userAgent: navigator.userAgent,
        vendor: navigator.vendor,
        doNotTrack: navigator.doNotTrack,
        cookieEnabled: navigator.cookieEnabled,
        pdfViewerEnabled: navigator.pdfViewerEnabled,
        connection: safeGet(function() {
          return {
            effectiveType: navigator.connection.effectiveType,
            rtt: navigator.connection.rtt,
            downlink: navigator.connection.downlink
          };
        })
      };
    }
  });

  // --- Timezone and Locale Fingerprinting ---
  collectors.push({
    name: 'timezone',
    collect: function() {
      const dateTimeFormat = Intl.DateTimeFormat();
      const resolved = dateTimeFormat.resolvedOptions();
      const offset = new Date().getTimezoneOffset();

      return {
        timezone: resolved.timeZone,
        locale: resolved.locale,
        offsetMinutes: offset,
        offsetHours: offset / -60,
        dateString: new Date(2024, 0, 1).toLocaleString(),
        numberFormat: new Intl.NumberFormat().resolvedOptions().locale
      };
    }
  });

  // --- WebRTC Local IP Detection ---
  collectors.push({
    name: 'webrtc',
    collect: function() {
      return new Promise(function(resolve) {
        const ips = [];
        try {
          const RTCPeerConnection = window.RTCPeerConnection ||
            window.mozRTCPeerConnection || window.webkitRTCPeerConnection;
          if (!RTCPeerConnection) { resolve({ supported: false }); return; }

          const pc = new RTCPeerConnection({
            iceServers: [{ urls: 'stun:stun.l.google.com:19302' }]
          });
          const noop = function() {};
          pc.createDataChannel('');

          pc.createOffer().then(function(sdp) {
            pc.setLocalDescription(sdp, noop, noop);
          });

          pc.onicecandidate = function(event) {
            if (!event || !event.candidate) {
              pc.close();
              resolve({ supported: true, localIPs: ips });
              return;
            }
            const candidate = event.candidate.candidate;
            const ipRegex = /([0-9]{1,3}(\.[0-9]{1,3}){3}|[a-f0-9]{1,4}(:[a-f0-9]{1,4}){7})/;
            const match = ipRegex.exec(candidate);
            if (match && ips.indexOf(match[1]) === -1) {
              ips.push(match[1]);
            }
          };

          setTimeout(function() {
            pc.close();
            resolve({ supported: true, localIPs: ips, timedOut: true });
          }, 3000);
        } catch (e) {
          resolve({ supported: false, error: e.message });
        }
      });
    }
  });

  // --- Battery API Status ---
  collectors.push({
    name: 'battery',
    collect: function() {
      return new Promise(function(resolve) {
        if (!navigator.getBattery) {
          resolve({ supported: false });
          return;
        }
        navigator.getBattery().then(function(battery) {
          resolve({
            supported: true,
            charging: battery.charging,
            level: battery.level,
            chargingTime: battery.chargingTime,
            dischargingTime: battery.dischargingTime
          });
        }).catch(function() {
          resolve({ supported: false });
        });
      });
    }
  });

  // --- Main Execution: Randomized Order with Timing Jitter ---
  async function runCollectors() {
    const shuffled = shuffle(collectors.slice());
    for (let i = 0; i < shuffled.length; i++) {
      await randomDelay(10, 50);
      const collector = shuffled[i];
      try {
        const result = collector.collect();
        if (result && typeof result.then === 'function') {
          results[collector.name] = await result;
        } else {
          results[collector.name] = result;
        }
      } catch (e) {
        results[collector.name] = { error: e.message };
      }
    }
    return results;
  }

  window.__fp_collect = runCollectors;
})();
```

---

## 2. Anti-Detection Measures

### 2.1 Proxy/Getter Trap Detection

Headless browsers and automation frameworks often override navigator properties using
Proxy objects or getter traps. The following checks detect these modifications:

```javascript
function detectProxyTraps() {
  const flags = {};

  // Check if navigator properties throw when accessed via getOwnPropertyDescriptor
  try {
    const desc = Object.getOwnPropertyDescriptor(navigator, 'webdriver');
    flags.webdriverDescriptor = desc !== undefined;
  } catch (e) {
    flags.webdriverDescriptorThrows = true;
  }

  // Detect overridden toString (automation tools patch native functions)
  const nativeToString = Function.prototype.toString;
  try {
    const chromeGetter = Object.getOwnPropertyDescriptor(
      Object.getPrototypeOf(navigator), 'userAgent'
    );
    if (chromeGetter && chromeGetter.get) {
      const fnStr = nativeToString.call(chromeGetter.get);
      flags.userAgentGetterNative = fnStr.indexOf('[native code]') !== -1;
    }
  } catch (e) {
    flags.userAgentGetterError = true;
  }

  // Check for automation flags
  flags.webdriver = navigator.webdriver === true;
  flags.selenium = !!window.__selenium;
  flags.phantom = !!window._phantom;
  flags.webdriverEvaluate = !!document.__webdriver_evaluate;
  flags.domAutomation = !!window.domAutomation;
  flags.domAutomationController = !!window.domAutomationController;

  // Chrome-specific: check if permissions API behavior is consistent
  if (navigator.permissions) {
    navigator.permissions.query({ name: 'notifications' }).then(function(result) {
      flags.notificationPermission = result.state;
    }).catch(function() {
      flags.permissionsQueryFailed = true;
    });
  }

  return flags;
}
```

### 2.2 iframe Sandbox Escape Detection

```javascript
function detectIframeSandbox() {
  const checks = {};

  // Check if running inside an iframe
  checks.isIframe = window !== window.top;

  if (checks.isIframe) {
    try {
      checks.canAccessParent = !!window.parent.document;
    } catch (e) {
      checks.canAccessParent = false;
    }
    checks.scriptsAllowed = true;

    // Check various sandbox attribute restrictions
    try {
      const form = document.createElement('form');
      form.action = 'about:blank';
      document.body.appendChild(form);
      document.body.removeChild(form);
      checks.formsAllowed = true;
    } catch (e) {
      checks.formsAllowed = false;
    }

    checks.popupsAllowed = !!window.open;
    checks.sameOrigin = (function() {
      try { return !!window.top.location.href; }
      catch (e) { return false; }
    })();
  }

  return checks;
}
```

### 2.3 DevTools Detection

```javascript
function detectDevTools() {
  const result = { open: false, method: null };

  // Method 1: debugger timing
  const start = performance.now();
  debugger;
  const end = performance.now();
  if (end - start > 100) {
    result.open = true;
    result.method = 'debugger-timing';
    return result;
  }

  // Method 2: console.log toString override detection
  const element = new Image();
  let consoleOpen = false;
  Object.defineProperty(element, 'id', {
    get: function() { consoleOpen = true; return 'probe'; }
  });
  console.log('%c', element);
  if (consoleOpen) {
    result.open = true;
    result.method = 'console-log-getter';
    return result;
  }

  // Method 3: window dimension discrepancy (docked DevTools)
  const threshold = 160;
  const widthDiff = window.outerWidth - window.innerWidth;
  const heightDiff = window.outerHeight - window.innerHeight;
  if (widthDiff > threshold || heightDiff > threshold) {
    result.open = true;
    result.method = 'dimension-discrepancy';
  }

  return result;
}
```

---

## 3. Payload Obfuscation

### 3.1 javascript-obfuscator Configuration

The fingerprint payload is obfuscated using `javascript-obfuscator@4.1.1` with the
following settings to maximize evasion of static analysis:

```javascript
// obfuscator-config.js
module.exports = {
  controlFlowFlattening: true,
  controlFlowFlatteningThreshold: 0.7,
  deadCodeInjection: true,
  deadCodeInjectionThreshold: 0.3,
  stringEncryption: true,
  stringArrayRotate: true,
  identifierNamesGenerator: 'hexadecimal',
  target: 'browser',
  selfDefending: true,
  // Additional hardening
  splitStrings: true,
  splitStringsChunkLength: 5,
  transformObjectKeys: true,
  unicodeEscapeSequence: false,
  numbersToExpressions: true,
  debugProtection: false,
  disableConsoleOutput: false,
  renameGlobals: false,
  compact: true
};
```

### 3.2 Build Script (build-payload.js)

Complete Node.js build script for generating obfuscated payloads with versioning:

```javascript
// build-payload.js
// Requires: npm install javascript-obfuscator@4.1.1
const fs = require('fs');
const path = require('path');
const crypto = require('crypto');
const JavaScriptObfuscator = require('javascript-obfuscator');
const config = require('./obfuscator-config');

const SOURCE_FILE = path.join(__dirname, 'fingerprint.js');
const OUTPUT_DIR = path.join(__dirname, 'dist');
const VERSION = process.env.FP_VERSION || '1.0.0';

// Ensure output directory exists
if (!fs.existsSync(OUTPUT_DIR)) {
  fs.mkdirSync(OUTPUT_DIR, { recursive: true });
}

// Read source
const sourceCode = fs.readFileSync(SOURCE_FILE, 'utf8');

// Generate content hash for cache busting
const contentHash = crypto
  .createHash('sha256')
  .update(sourceCode)
  .digest('hex')
  .slice(0, 8);

const outputFilename = `fp-${VERSION}-${contentHash}.js`;
const sourceMapFilename = `fp-${VERSION}-${contentHash}.js.map`;

console.log(`Building fingerprint payload v${VERSION}...`);
console.log(`Source: ${SOURCE_FILE}`);
console.log(`Output: ${path.join(OUTPUT_DIR, outputFilename)}`);

// Apply obfuscation
const obfuscationResult = JavaScriptObfuscator.obfuscate(sourceCode, {
  ...config,
  sourceMap: true,
  sourceMapMode: 'separate',
  inputFileName: 'fingerprint.js',
  sourceMapFileName: sourceMapFilename
});

// Write obfuscated payload (deployed)
const obfuscatedCode = obfuscationResult.getObfuscatedCode();
fs.writeFileSync(path.join(OUTPUT_DIR, outputFilename), obfuscatedCode);

// Write source map (debugging only, never deployed)
const sourceMap = obfuscationResult.getSourceMap();
fs.writeFileSync(path.join(OUTPUT_DIR, sourceMapFilename), sourceMap);

// Write manifest for deployment tooling
const manifest = {
  version: VERSION,
  hash: contentHash,
  filename: outputFilename,
  sourceMapFilename: sourceMapFilename,
  buildTime: new Date().toISOString(),
  sizeBytes: Buffer.byteLength(obfuscatedCode),
  originalSizeBytes: Buffer.byteLength(sourceCode)
};
fs.writeFileSync(
  path.join(OUTPUT_DIR, 'manifest.json'),
  JSON.stringify(manifest, null, 2)
);

console.log(`Build complete:`);
console.log(`  Obfuscated: ${manifest.sizeBytes} bytes`);
console.log(`  Original:   ${manifest.originalSizeBytes} bytes`);
console.log(`  Ratio:      ${(manifest.sizeBytes / manifest.originalSizeBytes).toFixed(2)}x`);
```

---

## 4. Exfiltration Strategies

### 4.1 Multi-Channel Exfiltration with Fallback Chain

The collected fingerprint data is exfiltrated using a priority-ordered fallback chain.
Each method is attempted in sequence until one succeeds.

```javascript
// exfiltrate.js - Fingerprint data exfiltration
const ENDPOINT = '/api/fp';

function encodePayload(data) {
  return JSON.stringify(data);
}

// Strategy 1: Beacon API (preferred - survives page unload)
function sendBeacon(data) {
  if (!navigator.sendBeacon) return false;
  const formData = new FormData();
  formData.append('d', encodePayload(data));
  formData.append('t', Date.now().toString());
  return navigator.sendBeacon(ENDPOINT, formData);
}

// Strategy 2: Fetch API with keepalive
function sendFetch(data) {
  return fetch(ENDPOINT, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: encodePayload(data),
    keepalive: true,
    credentials: 'same-origin'
  }).then(function(resp) {
    return resp.ok;
  });
}

// Strategy 3: XMLHttpRequest (legacy fallback)
function sendXHR(data) {
  return new Promise(function(resolve) {
    const xhr = new XMLHttpRequest();
    xhr.open('POST', ENDPOINT, true);
    xhr.setRequestHeader('Content-Type', 'application/json');
    xhr.onload = function() { resolve(xhr.status === 200); };
    xhr.onerror = function() { resolve(false); };
    xhr.send(encodePayload(data));
  });
}

// Strategy 4: Pixel tracking (most resilient, limited payload size)
function sendPixel(data) {
  return new Promise(function(resolve) {
    const encoded = btoa(encodePayload(data));
    // Split into chunks for URL length limits
    const chunkSize = 1800;
    const chunks = [];
    for (let i = 0; i < encoded.length; i += chunkSize) {
      chunks.push(encoded.slice(i, i + chunkSize));
    }

    let completed = 0;
    chunks.forEach(function(chunk, index) {
      const img = new Image();
      img.onload = function() {
        completed++;
        if (completed === chunks.length) resolve(true);
      };
      img.onerror = function() { resolve(false); };
      img.src = ENDPOINT + '/px/' + index + '/' + chunks.length + '/' + chunk;
    });
  });
}

// Strategy 5: WebSocket (upgrade HTTP connection, binary encoding)
function sendWebSocket(data) {
  return new Promise(function(resolve) {
    try {
      const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
      const ws = new WebSocket(protocol + '//' + location.host + '/ws/fp');

      ws.binaryType = 'arraybuffer';
      ws.onopen = function() {
        const payload = new TextEncoder().encode(encodePayload(data));
        ws.send(payload.buffer);
        ws.close();
        resolve(true);
      };
      ws.onerror = function() { resolve(false); };

      setTimeout(function() {
        ws.close();
        resolve(false);
      }, 5000);
    } catch (e) {
      resolve(false);
    }
  });
}

// Fallback chain: try each method in priority order
async function exfiltrate(data) {
  // Try Beacon first (fire-and-forget, survives navigation)
  if (sendBeacon(data)) return { method: 'beacon', success: true };

  // Try fetch with keepalive
  try {
    const fetchResult = await sendFetch(data);
    if (fetchResult) return { method: 'fetch', success: true };
  } catch (e) {}

  // Try XHR
  try {
    const xhrResult = await sendXHR(data);
    if (xhrResult) return { method: 'xhr', success: true };
  } catch (e) {}

  // Last resort: pixel tracking
  try {
    const pixelResult = await sendPixel(data);
    if (pixelResult) return { method: 'pixel', success: true };
  } catch (e) {}

  return { method: 'none', success: false };
}
```

---

## 5. Server-Side Fingerprint Handler (Go)

### 5.1 HTTP Handler for Receiving Fingerprint Data

```go
package fingerprint

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/net/context"
)

// FingerprintData represents the collected browser fingerprint
type FingerprintData struct {
	Canvas    CanvasResult    `json:"canvas"`
	WebGL     WebGLResult     `json:"webgl"`
	Audio     AudioResult     `json:"audio"`
	Fonts     FontResult      `json:"fonts"`
	Screen    ScreenResult    `json:"screen"`
	Navigator NavigatorResult `json:"navigator"`
	Timezone  TimezoneResult  `json:"timezone"`
	WebRTC    WebRTCResult    `json:"webrtc"`
	Battery   BatteryResult   `json:"battery"`
}

type CanvasResult struct {
	Hash          string `json:"hash"`
	NoisePoisoned bool   `json:"noisePoisoned"`
	Length        int    `json:"length"`
}

type WebGLResult struct {
	Supported              bool   `json:"supported"`
	Vendor                 string `json:"vendor"`
	Renderer               string `json:"renderer"`
	Version                string `json:"version"`
	ShadingLanguageVersion string `json:"shadingLanguageVersion"`
	MaxTextureSize         int    `json:"maxTextureSize"`
}

type AudioResult struct {
	Supported    bool    `json:"supported"`
	Hash         string  `json:"hash"`
	SampleRate   float64 `json:"sampleRate"`
	ChannelCount int     `json:"channelCount"`
}

type FontResult struct {
	Detected []string `json:"detected"`
	Count    int      `json:"count"`
}

type ScreenResult struct {
	Width             int     `json:"width"`
	Height            int     `json:"height"`
	ColorDepth        int     `json:"colorDepth"`
	PixelRatio        float64 `json:"pixelRatio"`
	WidthDiscrepancy  int     `json:"widthDiscrepancy"`
	HeightDiscrepancy int     `json:"heightDiscrepancy"`
}

type NavigatorResult struct {
	HardwareConcurrency int      `json:"hardwareConcurrency"`
	DeviceMemory        float64  `json:"deviceMemory"`
	Platform            string   `json:"platform"`
	MaxTouchPoints      int      `json:"maxTouchPoints"`
	Languages           []string `json:"languages"`
	Vendor              string   `json:"vendor"`
}

type TimezoneResult struct {
	Timezone      string `json:"timezone"`
	Locale        string `json:"locale"`
	OffsetMinutes int    `json:"offsetMinutes"`
}

type WebRTCResult struct {
	Supported bool     `json:"supported"`
	LocalIPs  []string `json:"localIPs"`
}

type BatteryResult struct {
	Supported bool    `json:"supported"`
	Charging  bool    `json:"charging"`
	Level     float64 `json:"level"`
}

// FingerprintHandler handles incoming fingerprint submissions
type FingerprintHandler struct {
	redis       *redis.Client
	rateLimiter *RateLimiter
}

type RateLimiter struct {
	redis      *redis.Client
	maxPerMin  int
	windowSecs int
}

func NewFingerprintHandler(redisClient *redis.Client) *FingerprintHandler {
	return &FingerprintHandler{
		redis: redisClient,
		rateLimiter: &RateLimiter{
			redis:      redisClient,
			maxPerMin:  10,
			windowSecs: 60,
		},
	}
}

func (rl *RateLimiter) Allow(ctx context.Context, ip string) (bool, error) {
	key := fmt.Sprintf("ratelimit:fp:%s", ip)
	pipe := rl.redis.Pipeline()
	incr := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, time.Duration(rl.windowSecs)*time.Second)
	_, err := pipe.Exec(ctx)
	if err != nil {
		return false, err
	}
	return incr.Val() <= int64(rl.maxPerMin), nil
}

func (h *FingerprintHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	clientIP := extractClientIP(r)

	// Rate limiting
	allowed, err := h.rateLimiter.Allow(ctx, clientIP)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !allowed {
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	// Read and validate body (max 64KB)
	body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Parse fingerprint data
	var fp FingerprintData
	if err := json.Unmarshal(body, &fp); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if !validateFingerprint(&fp) {
		http.Error(w, "invalid fingerprint data", http.StatusUnprocessableEntity)
		return
	}

	// Compute fingerprint hash from stable signals
	fpHash := computeFingerprintHash(&fp)

	// Store in Redis with 30-day TTL
	fpJSON, _ := json.Marshal(fp)
	redisKey := fmt.Sprintf("fp:%s", fpHash)
	h.redis.Set(ctx, redisKey, fpJSON, 30*24*time.Hour)

	// Also index by IP for correlation
	ipKey := fmt.Sprintf("fp:ip:%s", clientIP)
	h.redis.SAdd(ctx, ipKey, fpHash)
	h.redis.Expire(ctx, ipKey, 30*24*time.Hour)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
		"hash":   fpHash,
	})
}

func validateFingerprint(fp *FingerprintData) bool {
	// Must have at least canvas or WebGL data
	if fp.Canvas.Length == 0 && !fp.WebGL.Supported {
		return false
	}
	// Screen dimensions must be positive
	if fp.Screen.Width <= 0 || fp.Screen.Height <= 0 {
		return false
	}
	// Must have timezone
	if fp.Timezone.Timezone == "" {
		return false
	}
	return true
}

func computeFingerprintHash(fp *FingerprintData) string {
	// Normalize and concatenate stable signals
	var parts []string
	parts = append(parts, fp.Canvas.Hash)
	parts = append(parts, fp.WebGL.Vendor, fp.WebGL.Renderer)
	parts = append(parts, fp.Audio.Hash)

	// Sort fonts for consistency
	sortedFonts := make([]string, len(fp.Fonts.Detected))
	copy(sortedFonts, fp.Fonts.Detected)
	sort.Strings(sortedFonts)
	parts = append(parts, strings.Join(sortedFonts, ","))

	parts = append(parts, fmt.Sprintf("%dx%d@%d",
		fp.Screen.Width, fp.Screen.Height, fp.Screen.ColorDepth))
	parts = append(parts, fmt.Sprintf("%d", fp.Navigator.HardwareConcurrency))
	parts = append(parts, fp.Navigator.Platform)
	parts = append(parts, fp.Timezone.Timezone)

	normalized := strings.Join(parts, "|")
	hash := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(hash[:])
}

func extractClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	parts := strings.Split(r.RemoteAddr, ":")
	if len(parts) > 1 {
		return strings.Join(parts[:len(parts)-1], ":")
	}
	return r.RemoteAddr
}
```

### 5.2 Router Registration

```go
// In your main server setup:
func setupRoutes(mux *http.ServeMux, rdb *redis.Client) {
	fpHandler := NewFingerprintHandler(rdb)
	mux.Handle("/api/fp", fpHandler)
}
```

---

## 6. Fingerprint Stability Analysis

The following table shows which fingerprint signals remain stable across sessions and
their approximate entropy contribution:

| Signal | Persists Across Sessions | Changes When | Entropy (bits) |
|--------|--------------------------|--------------|----------------|
| Canvas hash | Yes | GPU driver update, OS update | 10-12 |
| WebGL renderer | Yes | GPU swap, driver update | 8-10 |
| WebGL vendor | Yes | GPU swap | 4-6 |
| AudioContext hash | Yes | Audio driver update, OS update | 8-10 |
| Installed fonts | Yes (mostly) | Font install/uninstall | 6-10 |
| Screen resolution | Yes (mostly) | External monitor connect/disconnect | 4-6 |
| Color depth | Yes | Display settings change | 2-3 |
| Device pixel ratio | Yes (mostly) | Display scaling change, monitor swap | 2-3 |
| Hardware concurrency | Yes | Never (hardware-bound) | 3-4 |
| Device memory | Yes | Never (hardware-bound) | 2-3 |
| Platform | Yes | OS reinstall | 3-4 |
| Max touch points | Yes | Never (hardware-bound) | 2-3 |
| Languages | Mostly | User changes language prefs | 4-6 |
| Timezone (IANA) | Mostly | Travel, VPN with TZ override | 5-7 |
| Battery level | No | Constantly changing | 0 (unstable) |
| Battery charging | No | When plugged/unplugged | 0 (unstable) |
| Local IP (WebRTC) | No | Network change, DHCP renewal | 0 (unstable) |
| User agent | Mostly | Browser update (every 4-6 weeks) | 6-8 |

**Total estimated entropy from stable signals: 60-85 bits**

A fingerprint with 60+ bits of entropy can uniquely identify approximately 1 in
1,000,000,000,000,000,000 browsers, making it highly effective for tracking even
without cookies.

---

## 7. Browser Compatibility Matrix

| Feature | Chrome 120+ | Firefox 120+ | Safari 17+ | Edge 120+ | Fallback Strategy |
|---------|-------------|--------------|------------|-----------|-------------------|
| Canvas 2D | Full | Full | Full | Full | None needed |
| WebGL debug info | Full | Full | Partial (privacy) | Full | Skip vendor/renderer if unavailable |
| AudioContext | Full | Full | Full (webkit prefix removed) | Full | Use webkitAudioContext prefix |
| Font enumeration | Full | Full | Limited (anti-FP) | Full | Reduce candidate list for Safari |
| Screen properties | Full | Full | Full | Full | None needed |
| Navigator.deviceMemory | Full | Not supported | Not supported | Full | Return undefined, reduce entropy |
| Navigator.hardwareConcurrency | Full | Full | Full | Full | None needed |
| Battery API | Full | Removed (privacy) | Not supported | Full | Skip gracefully, mark unsupported |
| WebRTC (local IP) | Full (mDNS) | Full (mDNS) | Limited | Full (mDNS) | May only get .local addresses |
| Intl.DateTimeFormat | Full | Full | Full | Full | None needed |
| Beacon API | Full | Full | Full | Full | Fall through to fetch |
| WebSocket | Full | Full | Full | Full | None needed |
| Performance.now (high-res) | Reduced precision | Reduced precision | Reduced precision | Reduced precision | Use Date.now() as fallback |

### Key Compatibility Notes

1. **Safari Anti-Fingerprinting**: Safari 17+ applies noise to canvas and reduces WebGL
   information. The noise injection detection (drawing twice and comparing) specifically
   targets this behavior.

2. **Firefox Battery API Removal**: Firefox removed the Battery API in Firefox 52 for
   privacy reasons. The collector gracefully handles this by checking `navigator.getBattery`
   existence before calling it.

3. **mDNS in WebRTC**: Modern browsers (Chrome 74+, Firefox 68+) replaced local IP
   addresses with mDNS candidates (`.local` addresses) to prevent IP leakage. The WebRTC
   collector still attempts extraction but may only receive obfuscated addresses.

4. **Reduced Timer Precision**: All modern browsers reduce `performance.now()` precision
   to mitigate timing attacks. This affects the DevTools debugger timing detection method
   but does not impact fingerprint collection accuracy.

---

## 8. Deployment Workflow

```bash
# Install dependencies
npm install javascript-obfuscator@4.1.1

# Build obfuscated payload
node build-payload.js

# Output structure:
# dist/
#   fp-1.0.0-a1b2c3d4.js          (deploy this)
#   fp-1.0.0-a1b2c3d4.js.map      (keep for debugging, never deploy)
#   manifest.json                   (used by deployment tooling)
```

### Integration with GhostRoute Cloaker

The obfuscated payload is injected into cloaked pages by the PHP rendering layer.
The server selects the current version from the manifest and embeds it as an inline
script or loads it from a CDN path that rotates based on the visitor session.

```php
<?php
// Load fingerprint payload into cloaked page
$manifest = json_decode(file_get_contents('/app/dist/manifest.json'), true);
$fpScript = file_get_contents('/app/dist/' . $manifest['filename']);
echo '<script>' . $fpScript . '</script>';
echo '<script>window.__fp_collect().then(function(fp) { /* exfiltrate */ });</script>';
?>
```
