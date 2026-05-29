(function() {
  'use strict';

  var EVALUATE_ENDPOINT = '/evaluate';

  function safeGet(fn) {
    try { return fn(); } catch (e) { return undefined; }
  }

  // Lightweight fingerprint subset for pixel integration
  function collectLightFingerprint() {
    var fp = {};
    fp.screen_width = screen.width;
    fp.screen_height = screen.height;
    fp.color_depth = screen.colorDepth;
    fp.pixel_ratio = window.devicePixelRatio;
    fp.timezone = safeGet(function() { return Intl.DateTimeFormat().resolvedOptions().timeZone; });
    fp.languages = navigator.languages ? Array.from(navigator.languages) : [navigator.language];
    fp.platform = navigator.platform;
    fp.hardware_concurrency = navigator.hardwareConcurrency;
    fp.device_memory = navigator.deviceMemory;
    fp.user_agent = navigator.userAgent;
    fp.touch_points = navigator.maxTouchPoints;
    return fp;
  }

  // Evaluate the current visitor and optionally rewrite DOM with money page content
  async function evaluate(campaignId, options) {
    options = options || {};
    var fp = collectLightFingerprint();

    var payload = {
      ip: '',
      user_agent: navigator.userAgent,
      headers: {},
      campaign_id: campaignId || '',
      fingerprint: fp
    };

    try {
      var resp = await fetch(EVALUATE_ENDPOINT, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
        credentials: 'same-origin'
      });

      if (!resp.ok) return null;

      var data = await resp.json();

      // If decision is "allow" and money page content is provided, rewrite DOM
      if (data.decision === 'allow' && data.content && options.rewrite !== false) {
        document.open();
        document.write(data.content);
        document.close();
      }

      return data;
    } catch (e) {
      return null;
    }
  }

  // Auto-evaluate on load if data attributes are present on script tag
  var currentScript = document.currentScript;
  if (currentScript) {
    var campaignId = currentScript.getAttribute('data-campaign');
    if (campaignId) {
      if (document.readyState === 'complete' || document.readyState === 'interactive') {
        evaluate(campaignId);
      } else {
        document.addEventListener('DOMContentLoaded', function() {
          evaluate(campaignId);
        });
      }
    }
  }

  window.__gr_evaluate = evaluate;
})();
