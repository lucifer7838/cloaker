(function() {
  'use strict';

  var collectors = [];
  var results = {};

  function randomDelay(min, max) {
    return new Promise(function(resolve) {
      setTimeout(resolve, Math.floor(Math.random() * (max - min + 1)) + min);
    });
  }

  function shuffle(array) {
    for (var i = array.length - 1; i > 0; i--) {
      var j = Math.floor(Math.random() * (i + 1));
      var tmp = array[i];
      array[i] = array[j];
      array[j] = tmp;
    }
    return array;
  }

  function safeGet(fn) {
    try { return fn(); } catch (e) { return undefined; }
  }

  // --- Anti-Detection: Proxy/Getter Trap Detection ---
  function detectProxyTraps() {
    var flags = {};
    try {
      var desc = Object.getOwnPropertyDescriptor(navigator, 'webdriver');
      flags.webdriverDescriptor = desc !== undefined;
    } catch (e) {
      flags.webdriverDescriptorThrows = true;
    }

    var nativeToString = Function.prototype.toString;
    try {
      var chromeGetter = Object.getOwnPropertyDescriptor(
        Object.getPrototypeOf(navigator), 'userAgent'
      );
      if (chromeGetter && chromeGetter.get) {
        var fnStr = nativeToString.call(chromeGetter.get);
        flags.userAgentGetterNative = fnStr.indexOf('[native code]') !== -1;
      }
    } catch (e) {
      flags.userAgentGetterError = true;
    }

    flags.webdriver = navigator.webdriver === true;
    flags.selenium = !!window.__selenium;
    flags.phantom = !!window._phantom;
    flags.webdriverEvaluate = !!document.__webdriver_evaluate;
    flags.domAutomation = !!window.domAutomation;
    flags.domAutomationController = !!window.domAutomationController;

    return flags;
  }

  // --- Anti-Detection: iframe Sandbox Escape Detection ---
  function detectIframeSandbox() {
    var checks = {};
    checks.isIframe = window !== window.top;
    if (checks.isIframe) {
      try {
        checks.canAccessParent = !!window.parent.document;
      } catch (e) {
        checks.canAccessParent = false;
      }
      checks.sameOrigin = (function() {
        try { return !!window.top.location.href; }
        catch (e) { return false; }
      })();
    }
    return checks;
  }

  // --- Canvas Fingerprinting with Noise Injection Detection ---
  collectors.push({
    name: 'canvas',
    collect: function() {
      var canvas = document.createElement('canvas');
      canvas.width = 256;
      canvas.height = 128;
      var ctx = canvas.getContext('2d');

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

      var dataUrl = canvas.toDataURL('image/png');

      // Noise detection: render same content twice and compare
      var canvas2 = document.createElement('canvas');
      canvas2.width = 256;
      canvas2.height = 128;
      var ctx2 = canvas2.getContext('2d');
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

      var dataUrl2 = canvas2.toDataURL('image/png');
      var noisePoisoned = dataUrl !== dataUrl2;

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
      var canvas = document.createElement('canvas');
      var gl = canvas.getContext('webgl') || canvas.getContext('experimental-webgl');
      if (!gl) return { supported: false };

      var debugInfo = gl.getExtension('WEBGL_debug_renderer_info');
      var vendor = debugInfo ? gl.getParameter(debugInfo.UNMASKED_VENDOR_WEBGL) : 'unknown';
      var renderer = debugInfo ? gl.getParameter(debugInfo.UNMASKED_RENDERER_WEBGL) : 'unknown';

      var extensions = gl.getSupportedExtensions() || [];
      var extHash = extensions.sort().join(',');

      return {
        supported: true,
        vendor: vendor,
        renderer: renderer,
        version: gl.getParameter(gl.VERSION),
        shadingLanguageVersion: gl.getParameter(gl.SHADING_LANGUAGE_VERSION),
        maxTextureSize: gl.getParameter(gl.MAX_TEXTURE_SIZE),
        maxViewportDims: Array.from(gl.getParameter(gl.MAX_VIEWPORT_DIMS)),
        extensionsHash: extHash.length > 0 ? extHash.slice(0, 64) : ''
      };
    }
  });

  // --- AudioContext Fingerprinting ---
  collectors.push({
    name: 'audio',
    collect: function() {
      return new Promise(function(resolve) {
        try {
          var AudioCtx = window.AudioContext || window.webkitAudioContext;
          if (!AudioCtx) { resolve({ supported: false }); return; }

          var context = new AudioCtx();
          var oscillator = context.createOscillator();
          var compressor = context.createDynamicsCompressor();
          var analyser = context.createAnalyser();

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
            var freqData = new Float32Array(analyser.frequencyBinCount);
            analyser.getFloatFrequencyData(freqData);

            var hash = 0;
            for (var i = 0; i < freqData.length; i++) {
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
      var baseFonts = ['monospace', 'sans-serif', 'serif'];
      var testFonts = [
        'Arial', 'Verdana', 'Times New Roman', 'Courier New', 'Georgia',
        'Palatino', 'Garamond', 'Comic Sans MS', 'Trebuchet MS', 'Arial Black',
        'Impact', 'Lucida Console', 'Tahoma', 'Lucida Sans', 'Century Gothic',
        'Bookman Old Style', 'Candara', 'Calibri', 'Cambria', 'Consolas',
        'Franklin Gothic', 'Futura', 'Geneva', 'Helvetica', 'Monaco',
        'Optima', 'Segoe UI', 'Roboto', 'Ubuntu', 'Droid Sans'
      ];
      var testString = 'mmmmmmmmmmlli';
      var testSize = '72px';
      var canvas = document.createElement('canvas');
      var ctx = canvas.getContext('2d');

      function getTextWidth(fontFamily) {
        ctx.font = testSize + ' ' + fontFamily;
        return ctx.measureText(testString).width;
      }

      var baseWidths = {};
      baseFonts.forEach(function(font) {
        baseWidths[font] = getTextWidth(font);
      });

      var detected = [];
      testFonts.forEach(function(font) {
        for (var i = 0; i < baseFonts.length; i++) {
          var width = getTextWidth(font + ',' + baseFonts[i]);
          if (width !== baseWidths[baseFonts[i]]) {
            detected.push(font);
            break;
          }
        }
      });

      return { detected: detected, count: detected.length };
    }
  });

  // --- Screen and Window Properties ---
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

  // --- Timezone and Locale ---
  collectors.push({
    name: 'timezone',
    collect: function() {
      var dateTimeFormat = Intl.DateTimeFormat();
      var resolved = dateTimeFormat.resolvedOptions();
      var offset = new Date().getTimezoneOffset();

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
        var ips = [];
        try {
          var RTCPeerConnection = window.RTCPeerConnection ||
            window.mozRTCPeerConnection || window.webkitRTCPeerConnection;
          if (!RTCPeerConnection) { resolve({ supported: false }); return; }

          var pc = new RTCPeerConnection({
            iceServers: [{ urls: 'stun:stun.l.google.com:19302' }]
          });
          pc.createDataChannel('');

          pc.createOffer().then(function(sdp) {
            pc.setLocalDescription(sdp);
          });

          pc.onicecandidate = function(event) {
            if (!event || !event.candidate) {
              pc.close();
              resolve({ supported: true, localIPs: ips });
              return;
            }
            var candidate = event.candidate.candidate;
            var ipRegex = /([0-9]{1,3}(\.[0-9]{1,3}){3}|[a-f0-9]{1,4}(:[a-f0-9]{1,4}){7})/;
            var match = ipRegex.exec(candidate);
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

  // --- Battery API ---
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
    var shuffled = shuffle(collectors.slice());
    for (var i = 0; i < shuffled.length; i++) {
      await randomDelay(10, 50);
      var collector = shuffled[i];
      try {
        var result = collector.collect();
        if (result && typeof result.then === 'function') {
          results[collector.name] = await result;
        } else {
          results[collector.name] = result;
        }
      } catch (e) {
        results[collector.name] = { error: e.message };
      }
    }

    // Add anti-detection signals
    results._proxyTraps = detectProxyTraps();
    results._iframeSandbox = detectIframeSandbox();

    return results;
  }

  // --- Exfiltration via POST /fingerprint with fallback chain ---
  function encodePayload(data) {
    return JSON.stringify(data);
  }

  function sendBeacon(data) {
    if (!navigator.sendBeacon) return false;
    var blob = new Blob([encodePayload(data)], { type: 'application/json' });
    return navigator.sendBeacon('/fingerprint', blob);
  }

  function sendFetch(data) {
    return fetch('/fingerprint', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: encodePayload(data),
      keepalive: true,
      credentials: 'same-origin'
    }).then(function(resp) {
      return resp.ok;
    });
  }

  function sendXHR(data) {
    return new Promise(function(resolve) {
      var xhr = new XMLHttpRequest();
      xhr.open('POST', '/fingerprint', true);
      xhr.setRequestHeader('Content-Type', 'application/json');
      xhr.onload = function() { resolve(xhr.status === 200); };
      xhr.onerror = function() { resolve(false); };
      xhr.send(encodePayload(data));
    });
  }

  async function exfiltrate(data) {
    if (sendBeacon(data)) return { method: 'beacon', success: true };

    try {
      var fetchResult = await sendFetch(data);
      if (fetchResult) return { method: 'fetch', success: true };
    } catch (e) {}

    try {
      var xhrResult = await sendXHR(data);
      if (xhrResult) return { method: 'xhr', success: true };
    } catch (e) {}

    return { method: 'none', success: false };
  }

  // Auto-run: collect and send
  async function init() {
    var fp = await runCollectors();
    await exfiltrate(fp);
  }

  if (document.readyState === 'complete' || document.readyState === 'interactive') {
    setTimeout(init, Math.floor(Math.random() * 200) + 50);
  } else {
    document.addEventListener('DOMContentLoaded', function() {
      setTimeout(init, Math.floor(Math.random() * 200) + 50);
    });
  }

  window.__fp_collect = runCollectors;
})();
