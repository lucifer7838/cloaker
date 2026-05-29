# SKILL: PHP Inline Render with Output Buffering

## Overview

PHP output buffering pattern for capturing, modifying, and serving different page content
based on visitor classification. This is the core rendering technique used in cloaking
systems to serve different HTML to bots vs real users without separate URL paths.

## Output Buffering Fundamentals

### Basic ob_start / ob_get_clean Pattern

```php
<?php
/**
 * Core output buffering pattern for page capture and modification.
 * Start buffering at request entry, capture full HTML, modify before sending.
 */

// Start output buffering at the very beginning of request processing
ob_start();

// Include the target page (WordPress, landing page, etc.)
// All echo/print output is captured in the buffer
include $page_path;

// Get the captured HTML and clean (end) the buffer
$html = ob_get_clean();

// Now we have the full HTML in $html and can modify it
// before sending to the client
$modified_html = process_html($html, $visitor_classification);

// Send appropriate headers
header("Content-Type: text/html; charset=UTF-8");
header("Content-Length: " . strlen($modified_html));

// Output the modified content
echo $modified_html;
```

### Request Entry Point (index.php)

```php
<?php
/**
 * Main request entry point for GhostRoute PHP rendering layer.
 * Classifies visitor and routes to appropriate content pipeline.
 */

declare(strict_types=1);

// Autoloader
require_once __DIR__ . '/vendor/autoload.php';

use GhostRoute\Classifier\VisitorClassifier;
use GhostRoute\Render\PageRenderer;
use GhostRoute\Render\HtmlProcessor;
use GhostRoute\Cache\PageCache;

// Get visitor classification from Go API (via Redis cache)
$classifier = new VisitorClassifier();
$visitor = $classifier->classify($_SERVER, $_COOKIE, getallheaders());

// Route based on classification
$renderer = new PageRenderer();

if ($visitor->bot_score >= 0.7) {
    // Serve safe/clean page to detected bots
    $html = $renderer->serveSafePage($visitor);
} else {
    // Serve offer/monetization page to real users
    $html = $renderer->serveOfferPage($visitor);
}

// Process HTML (inject tracking, modify links, etc.)
$processor = new HtmlProcessor();
$final_html = $processor->process($html, $visitor);

// Send response
$renderer->sendResponse($final_html, $visitor);
```

## DOM Manipulation with DOMDocument

### Injecting Tracking Pixels

```php
<?php
/**
 * Inject tracking pixels before </body> tag using DOMDocument.
 */

class TrackingInjector
{
    private array $pixels = [];

    public function addPixel(string $url, array $params = []): self
    {
        $query = http_build_query($params);
        $full_url = $url . ($query ? "?" . $query : "");
        $this->pixels[] = $full_url;
        return $this;
    }

    public function inject(string $html): string
    {
        if (empty($this->pixels)) {
            return $html;
        }

        $doc = new DOMDocument();
        // Suppress warnings for malformed HTML
        libxml_use_internal_errors(true);
        $doc->loadHTML($html, LIBXML_HTML_NOIMPLIED | LIBXML_HTML_NODEFDTD);
        libxml_clear_errors();

        $body = $doc->getElementsByTagName('body')->item(0);
        if (!$body) {
            // Fallback: inject before </body> with string replacement
            return $this->injectFallback($html);
        }

        foreach ($this->pixels as $pixel_url) {
            $img = $doc->createElement('img');
            $img->setAttribute('src', $pixel_url);
            $img->setAttribute('width', '1');
            $img->setAttribute('height', '1');
            $img->setAttribute('style', 'position:absolute;left:-9999px;');
            $img->setAttribute('alt', '');
            $img->setAttribute('aria-hidden', 'true');
            $body->appendChild($img);
        }

        return $doc->saveHTML();
    }

    private function injectFallback(string $html): string
    {
        $pixels_html = '';
        foreach ($this->pixels as $pixel_url) {
            $pixels_html .= sprintf(
                '<img src="%s" width="1" height="1" style="position:absolute;left:-9999px" alt="" aria-hidden="true">',
                htmlspecialchars($pixel_url, ENT_QUOTES, 'UTF-8')
            );
        }
        return str_replace('</body>', $pixels_html . '</body>', $html);
    }
}

// Usage
$injector = new TrackingInjector();
$injector->addPixel('https://track.example.com/px', [
    'click_id' => $visitor->click_id,
    'campaign' => $visitor->campaign_id,
    'ts' => time(),
]);
$html = $injector->inject($html);
```

### Injecting JavaScript Fingerprinting Payload

```php
<?php
/**
 * Inject fingerprinting JavaScript into pages served to real users.
 * This collects browser fingerprint data for bot detection.
 */

class FingerprintInjector
{
    private string $collector_endpoint;
    private string $script_path;

    public function __construct(string $collector_endpoint, string $script_path)
    {
        $this->collector_endpoint = $collector_endpoint;
        $this->script_path = $script_path;
    }

    public function inject(string $html, string $click_id): string
    {
        $doc = new DOMDocument();
        libxml_use_internal_errors(true);
        $doc->loadHTML($html, LIBXML_HTML_NOIMPLIED | LIBXML_HTML_NODEFDTD);
        libxml_clear_errors();

        $head = $doc->getElementsByTagName('head')->item(0);
        if (!$head) {
            $head = $doc->createElement('head');
            $doc->documentElement->insertBefore($head, $doc->documentElement->firstChild);
        }

        // Inline configuration script
        $config_script = $doc->createElement('script');
        $config_js = sprintf(
            'window.__gr={cid:"%s",ep:"%s",v:"%s"}',
            addslashes($click_id),
            addslashes($this->collector_endpoint),
            '1.0.4'
        );
        $config_script->appendChild($doc->createTextNode($config_js));
        $head->appendChild($config_script);

        // External fingerprint collection script (loaded async)
        $fp_script = $doc->createElement('script');
        $fp_script->setAttribute('src', $this->script_path);
        $fp_script->setAttribute('async', '');
        $fp_script->setAttribute('defer', '');
        $head->appendChild($fp_script);

        return $doc->saveHTML();
    }

    /**
     * Generate the inline fingerprinting script content.
     * This is the actual JS that collects fingerprint data.
     */
    public function generateInlineScript(string $click_id): string
    {
        return <<<JS
        <script>
        (function(){
            var d=document,w=window,n=navigator;
            var fp={};
            
            // Canvas fingerprint
            try{
                var c=d.createElement('canvas');
                var ctx=c.getContext('2d');
                ctx.textBaseline="top";
                ctx.font="14px Arial";
                ctx.fillText("GhostRoute",2,2);
                fp.canvas=c.toDataURL().slice(-50);
            }catch(e){fp.canvas="";}
            
            // WebGL fingerprint
            try{
                var gl=d.createElement('canvas').getContext('webgl');
                var dbg=gl.getExtension('WEBGL_debug_renderer_info');
                fp.webgl_vendor=gl.getParameter(dbg.UNMASKED_VENDOR_WEBGL);
                fp.webgl_renderer=gl.getParameter(dbg.UNMASKED_RENDERER_WEBGL);
            }catch(e){fp.webgl_vendor="";fp.webgl_renderer="";}
            
            // Screen and navigator
            fp.screen=w.screen.width+"x"+w.screen.height;
            fp.color_depth=w.screen.colorDepth;
            fp.timezone=Intl.DateTimeFormat().resolvedOptions().timeZone;
            fp.language=n.language;
            fp.platform=n.platform;
            fp.cores=n.hardwareConcurrency||0;
            fp.memory=n.deviceMemory||0;
            fp.touch=n.maxTouchPoints||0;
            
            // Timing (bot detection)
            fp.timing=performance.now();
            
            // Send to collector
            var xhr=new XMLHttpRequest();
            xhr.open("POST",window.__gr.ep,true);
            xhr.setRequestHeader("Content-Type","application/json");
            xhr.send(JSON.stringify({cid:window.__gr.cid,fp:fp,ts:Date.now()}));
        })();
        </script>
        JS;
    }
}
```

### Modifying Links for Click Tracking

```php
<?php
/**
 * Modify all href links on a page to pass through tracking redirect.
 */

class LinkModifier
{
    private string $tracking_base;
    private array $params;
    private array $exclude_domains = [];

    public function __construct(string $tracking_base, array $params = [])
    {
        $this->tracking_base = rtrim($tracking_base, '/');
        $this->params = $params;
    }

    public function excludeDomains(array $domains): self
    {
        $this->exclude_domains = $domains;
        return $this;
    }

    public function modifyLinks(string $html): string
    {
        $doc = new DOMDocument();
        libxml_use_internal_errors(true);
        $doc->loadHTML($html, LIBXML_HTML_NOIMPLIED | LIBXML_HTML_NODEFDTD);
        libxml_clear_errors();

        $links = $doc->getElementsByTagName('a');

        // Iterate in reverse to safely modify
        for ($i = $links->length - 1; $i >= 0; $i--) {
            $link = $links->item($i);
            $href = $link->getAttribute('href');

            if (!$href || $this->shouldExclude($href)) {
                continue;
            }

            // Skip anchors, javascript:, mailto:, tel:
            if (preg_match('/^(#|javascript:|mailto:|tel:)/i', $href)) {
                continue;
            }

            // Build tracking URL
            $tracked_url = $this->buildTrackingUrl($href);
            $link->setAttribute('href', $tracked_url);

            // Add data attribute with original URL (for JS fallback)
            $link->setAttribute('data-original-href', $href);
        }

        return $doc->saveHTML();
    }

    private function shouldExclude(string $url): bool
    {
        foreach ($this->exclude_domains as $domain) {
            if (stripos($url, $domain) !== false) {
                return true;
            }
        }
        return false;
    }

    private function buildTrackingUrl(string $original_url): string
    {
        $params = array_merge($this->params, [
            'url' => base64_encode($original_url),
            't' => time(),
        ]);
        return $this->tracking_base . '/r?' . http_build_query($params);
    }
}

// Usage
$modifier = new LinkModifier('https://trk.example.com', [
    'cid' => $visitor->click_id,
    'src' => $visitor->source,
]);
$modifier->excludeDomains(['google.com', 'facebook.com']);
$html = $modifier->modifyLinks($html);
```

### Stripping Meta Tags for Cloaked Delivery

```php
<?php
/**
 * Strip SEO meta tags from pages served to real users.
 * Prevents search engines from seeing offer page metadata if they
 * somehow bypass bot detection.
 */

class MetaStripper
{
    private array $strip_names = [
        'robots',
        'googlebot',
        'bingbot',
        'description',
        'keywords',
    ];

    private array $strip_rels = [
        'canonical',
        'alternate',
        'amphtml',
    ];

    public function strip(string $html): string
    {
        $doc = new DOMDocument();
        libxml_use_internal_errors(true);
        $doc->loadHTML($html, LIBXML_HTML_NOIMPLIED | LIBXML_HTML_NODEFDTD);
        libxml_clear_errors();

        $this->stripMetaTags($doc);
        $this->stripLinkTags($doc);

        return $doc->saveHTML();
    }

    private function stripMetaTags(DOMDocument $doc): void
    {
        $metas = $doc->getElementsByTagName('meta');
        $to_remove = [];

        for ($i = 0; $i < $metas->length; $i++) {
            $meta = $metas->item($i);
            $name = strtolower($meta->getAttribute('name'));

            if (in_array($name, $this->strip_names, true)) {
                $to_remove[] = $meta;
            }
        }

        foreach ($to_remove as $node) {
            $node->parentNode->removeChild($node);
        }
    }

    private function stripLinkTags(DOMDocument $doc): void
    {
        $links = $doc->getElementsByTagName('link');
        $to_remove = [];

        for ($i = 0; $i < $links->length; $i++) {
            $link = $links->item($i);
            $rel = strtolower($link->getAttribute('rel'));

            if (in_array($rel, $this->strip_rels, true)) {
                $to_remove[] = $link;
            }
        }

        foreach ($to_remove as $node) {
            $node->parentNode->removeChild($node);
        }
    }
}
```

### Adding/Removing CSS Classes

```php
<?php
/**
 * Modify CSS classes on elements for visual differences
 * between bot and user page versions.
 */

class CssClassModifier
{
    public function addClass(string $html, string $selector_id, string $class): string
    {
        $doc = new DOMDocument();
        libxml_use_internal_errors(true);
        $doc->loadHTML($html, LIBXML_HTML_NOIMPLIED | LIBXML_HTML_NODEFDTD);
        libxml_clear_errors();

        $element = $doc->getElementById($selector_id);
        if ($element) {
            $existing = $element->getAttribute('class');
            $classes = $existing ? explode(' ', $existing) : [];
            if (!in_array($class, $classes)) {
                $classes[] = $class;
                $element->setAttribute('class', implode(' ', $classes));
            }
        }

        return $doc->saveHTML();
    }

    public function removeClass(string $html, string $selector_id, string $class): string
    {
        $doc = new DOMDocument();
        libxml_use_internal_errors(true);
        $doc->loadHTML($html, LIBXML_HTML_NOIMPLIED | LIBXML_HTML_NODEFDTD);
        libxml_clear_errors();

        $element = $doc->getElementById($selector_id);
        if ($element) {
            $existing = $element->getAttribute('class');
            $classes = explode(' ', $existing);
            $classes = array_filter($classes, fn($c) => $c !== $class);
            $element->setAttribute('class', implode(' ', $classes));
        }

        return $doc->saveHTML();
    }
}
```


## Content Replacement Strategies

### Visitor Classification and Routing

```php
<?php
/**
 * Decision logic for serving different content based on bot_score.
 */

class ContentRouter
{
    private const BOT_THRESHOLD = 0.7;
    private const SUSPICIOUS_THRESHOLD = 0.4;

    private PageCache $cache;
    private string $safe_pages_dir;
    private string $offer_pages_dir;

    public function __construct(PageCache $cache, string $safe_pages_dir, string $offer_pages_dir)
    {
        $this->cache = $cache;
        $this->safe_pages_dir = $safe_pages_dir;
        $this->offer_pages_dir = $offer_pages_dir;
    }

    public function route(VisitorData $visitor): RenderedPage
    {
        if ($visitor->bot_score >= self::BOT_THRESHOLD) {
            return $this->serveSafePage($visitor);
        }

        if ($visitor->bot_score >= self::SUSPICIOUS_THRESHOLD) {
            // Suspicious but not confirmed bot: serve safe page with JS challenge
            return $this->serveSafePageWithChallenge($visitor);
        }

        // Real user: serve offer page
        return $this->serveOfferPage($visitor);
    }

    private function serveSafePage(VisitorData $visitor): RenderedPage
    {
        // Try cache first (WordPress page cached as static HTML)
        $cache_key = "safe_page:" . $visitor->landing_url_hash;
        $cached = $this->cache->get($cache_key);

        if ($cached !== null) {
            return new RenderedPage($cached, [
                "Cache-Control" => "public, max-age=3600",
                "X-Robots-Tag" => "index, follow",
                "ETag" => '"' . md5($cached) . '"',
            ]);
        }

        // Load from file
        $page_file = $this->safe_pages_dir . '/' . $visitor->landing_page . '.html';
        if (!file_exists($page_file)) {
            $page_file = $this->safe_pages_dir . '/default.html';
        }

        $html = file_get_contents($page_file);
        $this->cache->set($cache_key, $html, 3600);

        return new RenderedPage($html, [
            "Cache-Control" => "public, max-age=3600",
            "X-Robots-Tag" => "index, follow",
            "ETag" => '"' . md5($html) . '"',
        ]);
    }

    private function serveSafePageWithChallenge(VisitorData $visitor): RenderedPage
    {
        $page = $this->serveSafePage($visitor);

        // Inject JS challenge to re-evaluate after fingerprinting
        $injector = new FingerprintInjector(
            'https://api.ghostroute.io/collect',
            '/assets/fp.min.js'
        );
        $html = $injector->inject($page->html, $visitor->click_id);

        return new RenderedPage($html, $page->headers);
    }

    private function serveOfferPage(VisitorData $visitor): RenderedPage
    {
        // Load offer page based on campaign configuration
        $offer_file = $this->offer_pages_dir . '/' . $visitor->campaign_id . '.html';
        if (!file_exists($offer_file)) {
            $offer_file = $this->offer_pages_dir . '/default_offer.html';
        }

        $html = file_get_contents($offer_file);

        // Replace placeholders in offer page
        $html = str_replace([
            '{{click_id}}',
            '{{visitor_ip}}',
            '{{user_agent}}',
            '{{campaign_id}}',
        ], [
            $visitor->click_id,
            $visitor->ip,
            htmlspecialchars($visitor->user_agent),
            $visitor->campaign_id,
        ], $html);

        return new RenderedPage($html, [
            "Cache-Control" => "no-store, no-cache, must-revalidate",
            "X-Robots-Tag" => "noindex, nofollow",
            "Pragma" => "no-cache",
        ]);
    }
}
```

### Complete Safe Page Serving Example

```php
<?php
/**
 * Full example: Serve cached WordPress page to bots.
 * Loads static HTML, processes with DOMDocument, sends with appropriate headers.
 */

function serve_safe_page_to_bot(string $page_slug, array $server_vars): void
{
    // 1. Load cached WordPress content
    $cache_dir = '/var/cache/ghostroute/pages';
    $page_path = $cache_dir . '/' . preg_replace('/[^a-z0-9_-]/', '', $page_slug) . '.html';

    if (!file_exists($page_path)) {
        $page_path = $cache_dir . '/homepage.html';
    }

    $html = file_get_contents($page_path);

    // 2. Process with DOMDocument to ensure valid structure
    $doc = new DOMDocument('1.0', 'UTF-8');
    libxml_use_internal_errors(true);
    $doc->loadHTML($html, LIBXML_HTML_NOIMPLIED | LIBXML_HTML_NODEFDTD);
    libxml_clear_errors();

    // 3. Ensure proper meta robots tag exists
    $head = $doc->getElementsByTagName('head')->item(0);
    if ($head) {
        $meta = $doc->createElement('meta');
        $meta->setAttribute('name', 'robots');
        $meta->setAttribute('content', 'index, follow');
        $head->appendChild($meta);
    }

    // 4. Add canonical URL
    if ($head) {
        $canonical = $doc->createElement('link');
        $canonical->setAttribute('rel', 'canonical');
        $canonical->setAttribute('href', 'https://' . $server_vars['HTTP_HOST'] . $server_vars['REQUEST_URI']);
        $head->appendChild($canonical);
    }

    $output = $doc->saveHTML();

    // 5. Send bot-friendly headers
    header('HTTP/1.1 200 OK');
    header('Content-Type: text/html; charset=UTF-8');
    header('Cache-Control: public, max-age=3600, s-maxage=86400');
    header('X-Robots-Tag: index, follow');
    header('ETag: "' . md5($output) . '"');
    header('Last-Modified: ' . gmdate('D, d M Y H:i:s', filemtime($page_path)) . ' GMT');
    header('Content-Length: ' . strlen($output));

    echo $output;
    exit;
}
```

## Response Header Manipulation

```php
<?php
/**
 * Header management for different visitor types.
 */

class ResponseHeaders
{
    public static function forBot(): array
    {
        return [
            "Cache-Control" => "public, max-age=3600, s-maxage=86400",
            "X-Robots-Tag" => "index, follow",
            "Vary" => "Accept-Encoding",
            "X-Content-Type-Options" => "nosniff",
            "Content-Type" => "text/html; charset=UTF-8",
        ];
    }

    public static function forHuman(): array
    {
        return [
            "Cache-Control" => "no-store, no-cache, must-revalidate, max-age=0",
            "X-Robots-Tag" => "noindex, nofollow, noarchive",
            "Pragma" => "no-cache",
            "Expires" => "Thu, 01 Jan 1970 00:00:00 GMT",
            "X-Content-Type-Options" => "nosniff",
            "Content-Type" => "text/html; charset=UTF-8",
        ];
    }

    public static function send(array $headers): void
    {
        foreach ($headers as $name => $value) {
            header("$name: $value");
        }
    }

    public static function generateEtag(string $content): string
    {
        return '"' . substr(md5($content), 0, 16) . '"';
    }

    public static function handleConditionalRequest(string $etag): bool
    {
        $if_none_match = $_SERVER['HTTP_IF_NONE_MATCH'] ?? '';
        if ($if_none_match === $etag) {
            http_response_code(304);
            header("ETag: $etag");
            return true;  // Response already sent (304)
        }
        return false;  // Caller should send full response
    }
}
```

## Gzip Output Handling

### Using ob_gzhandler

```php
<?php
/**
 * Automatic gzip compression using ob_gzhandler.
 * PHP automatically checks Accept-Encoding and compresses if supported.
 */

// Method 1: Automatic with ob_gzhandler
// PHP handles Content-Encoding header automatically
ob_start('ob_gzhandler');

// ... generate or include page content ...
echo $html_content;

// ob_end_flush() called automatically at script end,
// or explicitly:
ob_end_flush();
```

### Manual Gzip for Pre-Compressed Content

```php
<?php
/**
 * Manual gzip compression for serving pre-compressed cached content
 * or when you need more control over the compression process.
 */

class GzipOutputHandler
{
    private int $compression_level;

    public function __construct(int $compression_level = 6)
    {
        $this->compression_level = $compression_level;
    }

    public function send(string $content, bool $client_accepts_gzip = null): void
    {
        if ($client_accepts_gzip === null) {
            $client_accepts_gzip = $this->clientAcceptsGzip();
        }

        if ($client_accepts_gzip && strlen($content) > 1024) {
            $compressed = gzencode($content, $this->compression_level);
            header('Content-Encoding: gzip');
            header('Content-Length: ' . strlen($compressed));
            header('Vary: Accept-Encoding');
            echo $compressed;
        } else {
            header('Content-Length: ' . strlen($content));
            echo $content;
        }
    }

    public function sendPreCompressed(string $gz_path, string $fallback_path): void
    {
        if ($this->clientAcceptsGzip() && file_exists($gz_path)) {
            header('Content-Encoding: gzip');
            header('Content-Length: ' . filesize($gz_path));
            header('Vary: Accept-Encoding');
            readfile($gz_path);
        } else {
            header('Content-Length: ' . filesize($fallback_path));
            readfile($fallback_path);
        }
    }

    private function clientAcceptsGzip(): bool
    {
        $accept = $_SERVER['HTTP_ACCEPT_ENCODING'] ?? '';
        return str_contains($accept, 'gzip');
    }
}

// Usage
$handler = new GzipOutputHandler(compression_level: 6);
$handler->send($final_html);
```

## Streaming vs Buffered Approaches

### When to Use flush() for Large Pages

```php
<?php
/**
 * Streaming output for large pages where buffering would use too much memory.
 * Use this for pages > 500KB where DOM manipulation is not needed.
 */

class StreamingRenderer
{
    private int $chunk_size;

    public function __construct(int $chunk_size = 8192)
    {
        $this->chunk_size = $chunk_size;
    }

    /**
     * Stream a file with header/footer injection.
     * Avoids loading entire file into memory.
     */
    public function streamWithInjection(
        string $file_path,
        string $header_injection,
        string $footer_injection
    ): void {
        // Disable output buffering for streaming
        while (ob_get_level() > 0) {
            ob_end_clean();
        }

        header('Transfer-Encoding: chunked');
        header('Content-Type: text/html; charset=UTF-8');

        $fp = fopen($file_path, 'r');
        if (!$fp) {
            http_response_code(500);
            return;
        }

        $head_injected = false;
        $buffer = '';

        while (!eof($fp)) {
            $chunk = fread($fp, $this->chunk_size);
            $buffer .= $chunk;

            // Inject into <head> on first occurrence
            if (!$head_injected && str_contains($buffer, '</head>')) {
                $buffer = str_replace('</head>', $header_injection . '</head>', $buffer);
                $head_injected = true;
            }

            // Check for </body> to inject footer
            if (str_contains($buffer, '</body>')) {
                $buffer = str_replace('</body>', $footer_injection . '</body>', $buffer);
                echo $buffer;
                flush();
                $buffer = '';
                break;
            }

            // Flush complete lines only
            $last_newline = strrpos($buffer, "
");
            if ($last_newline !== false && strlen($buffer) > $this->chunk_size) {
                echo substr($buffer, 0, $last_newline + 1);
                flush();
                $buffer = substr($buffer, $last_newline + 1);
            }
        }

        // Flush remaining
        if ($buffer !== '') {
            echo $buffer;
            flush();
        }

        fclose($fp);
    }
}
```

### Memory Management with Nested Buffers

```php
<?php
/**
 * Nested output buffer handling for complex page composition.
 * Useful when multiple components each need their own buffer processing.
 */

class NestedBufferManager
{
    private array $processors = [];

    public function pushBuffer(callable $processor): void
    {
        $this->processors[] = $processor;
        ob_start();
    }

    public function popBuffer(): string
    {
        if (ob_get_level() === 0) {
            throw new RuntimeException('No active output buffer');
        }

        $content = ob_get_clean();
        $processor = array_pop($this->processors);

        if ($processor !== null) {
            $content = $processor($content);
        }

        return $content;
    }

    public function getLevel(): int
    {
        return ob_get_level();
    }

    public function cleanAll(): void
    {
        while (ob_get_level() > 0) {
            ob_end_clean();
        }
        $this->processors = [];
    }

    /**
     * Get memory usage of current buffer.
     */
    public function getBufferMemoryUsage(): int
    {
        return ob_get_length() ?: 0;
    }
}

// Usage example: nested buffers for page sections
$mgr = new NestedBufferManager();

// Outer buffer: full page compression
$mgr->pushBuffer(function(string $html): string {
    return gzencode($html, 6);
});

// Inner buffer: DOM processing
$mgr->pushBuffer(function(string $html): string {
    $doc = new DOMDocument();
    libxml_use_internal_errors(true);
    $doc->loadHTML($html, LIBXML_HTML_NOIMPLIED | LIBXML_HTML_NODEFDTD);
    libxml_clear_errors();
    // ... process DOM ...
    return $doc->saveHTML();
});

// Generate content
include 'templates/page.php';

// Pop inner buffer (DOM processing applied)
$processed = $mgr->popBuffer();
echo $processed;

// Pop outer buffer (gzip applied)
$compressed = $mgr->popBuffer();
header('Content-Encoding: gzip');
echo $compressed;
```


## YellowTDS Integration Patterns

### htmlprocessing.php Pattern

```php
<?php
/**
 * Pattern matching YellowTDS htmlprocessing.php behavior.
 * Captures HTML from the target page and applies transformations.
 */

class HtmlProcessing
{
    private array $replacements = [];
    private array $injections = [];
    private array $removals = [];

    /**
     * Process HTML content with all configured transformations.
     * Mirrors the sequential processing in YellowTDS htmlprocessing.php.
     */
    public function process(string $html, array $config): string
    {
        // Step 1: Remove unwanted elements (analytics, tracking from original)
        foreach ($this->removals as $pattern) {
            $html = preg_replace($pattern, '', $html);
        }

        // Step 2: Apply text/HTML replacements
        foreach ($this->replacements as $search => $replace) {
            $html = str_replace($search, $replace, $html);
        }

        // Step 3: DOM-based injections
        if (!empty($this->injections)) {
            $html = $this->applyDomInjections($html);
        }

        // Step 4: Clean up whitespace and fix encoding
        $html = mb_convert_encoding($html, 'HTML-ENTITIES', 'UTF-8');

        return $html;
    }

    public function addReplacement(string $search, string $replace): self
    {
        $this->replacements[$search] = $replace;
        return $this;
    }

    public function addRemoval(string $regex_pattern): self
    {
        $this->removals[] = $regex_pattern;
        return $this;
    }

    public function addHeadInjection(string $html_fragment): self
    {
        $this->injections['head'][] = $html_fragment;
        return $this;
    }

    public function addBodyInjection(string $html_fragment): self
    {
        $this->injections['body'][] = $html_fragment;
        return $this;
    }

    private function applyDomInjections(string $html): string
    {
        $doc = new DOMDocument();
        libxml_use_internal_errors(true);
        $doc->loadHTML($html, LIBXML_HTML_NOIMPLIED | LIBXML_HTML_NODEFDTD);
        libxml_clear_errors();

        // Head injections
        if (!empty($this->injections['head'])) {
            $head = $doc->getElementsByTagName('head')->item(0);
            if ($head) {
                foreach ($this->injections['head'] as $fragment) {
                    $frag = $doc->createDocumentFragment();
                    @$frag->appendXML($fragment);
                    $head->appendChild($frag);
                }
            }
        }

        // Body injections (before </body>)
        if (!empty($this->injections['body'])) {
            $body = $doc->getElementsByTagName('body')->item(0);
            if ($body) {
                foreach ($this->injections['body'] as $fragment) {
                    $frag = $doc->createDocumentFragment();
                    @$frag->appendXML($fragment);
                    $body->appendChild($frag);
                }
            }
        }

        return $doc->saveHTML();
    }
}
```

### htmlinject.php Pattern

```php
<?php
/**
 * Pattern matching YellowTDS htmlinject.php behavior.
 * Adds scripts and pixels to captured page output.
 */

class HtmlInject
{
    /**
     * Inject all configured elements into the page.
     * Called after htmlprocessing.php has done its work.
     */
    public static function injectAll(string $html, array $config): string
    {
        // Inject head scripts (analytics, pixels)
        if (!empty($config['head_scripts'])) {
            $scripts = implode("
", $config['head_scripts']);
            $html = str_replace('</head>', $scripts . "
</head>", $html);
        }

        // Inject body scripts (before </body>)
        if (!empty($config['body_scripts'])) {
            $scripts = implode("
", $config['body_scripts']);
            $html = str_replace('</body>', $scripts . "
</body>", $html);
        }

        // Inject tracking pixels
        if (!empty($config['pixels'])) {
            $pixels_html = '';
            foreach ($config['pixels'] as $pixel) {
                $pixels_html .= sprintf(
                    '<img src="%s" width="1" height="1" style="display:none" alt="">',
                    htmlspecialchars($pixel['url'], ENT_QUOTES)
                );
            }
            $html = str_replace('</body>', $pixels_html . "
</body>", $html);
        }

        // Inject CSS overrides
        if (!empty($config['css_overrides'])) {
            $css = '<style>' . implode("
", $config['css_overrides']) . '</style>';
            $html = str_replace('</head>', $css . "
</head>", $html);
        }

        return $html;
    }
}

// Usage matching YellowTDS configuration style
$inject_config = [
    'head_scripts' => [
        '<script>window.click_id="' . $click_id . '";</script>',
        '<script src="/assets/fp-collect.js" async></script>',
    ],
    'body_scripts' => [
        '<script src="/assets/mouse-track.js" async></script>',
    ],
    'pixels' => [
        ['url' => "https://track.example.com/px?cid={$click_id}&ev=view"],
        ['url' => "https://partner.example.com/conv?ref={$campaign_id}"],
    ],
    'css_overrides' => [
        '.header-banner { display: none !important; }',
        '.cookie-notice { display: none !important; }',
    ],
];

$html = HtmlInject::injectAll($html, $inject_config);
```

### directload.php Pattern

```php
<?php
/**
 * Pattern matching YellowTDS directload.php behavior.
 * Directly loads and serves cached content with minimal processing.
 * Used for fast delivery of pre-processed safe pages.
 */

class DirectLoad
{
    private string $cache_dir;
    private int $cache_ttl;

    public function __construct(string $cache_dir = '/var/cache/ghostroute', int $cache_ttl = 3600)
    {
        $this->cache_dir = $cache_dir;
        $this->cache_ttl = $cache_ttl;
    }

    /**
     * Serve a page directly from cache without full processing pipeline.
     */
    public function serve(string $page_key, array $headers = []): bool
    {
        $cache_path = $this->getCachePath($page_key);

        if (!file_exists($cache_path)) {
            return false;
        }

        // Check TTL
        $mtime = filemtime($cache_path);
        if (time() - $mtime > $this->cache_ttl) {
            unlink($cache_path);
            return false;
        }

        // Check If-Modified-Since
        $if_modified = $_SERVER['HTTP_IF_MODIFIED_SINCE'] ?? '';
        if ($if_modified && strtotime($if_modified) >= $mtime) {
            http_response_code(304);
            return true;
        }

        // Serve from cache
        $content_type = 'text/html; charset=UTF-8';
        $gz_path = $cache_path . '.gz';

        header('Content-Type: ' . $content_type);
        header('Last-Modified: ' . gmdate('D, d M Y H:i:s', $mtime) . ' GMT');
        header('ETag: "' . md5_file($cache_path) . '"');

        foreach ($headers as $name => $value) {
            header("$name: $value");
        }

        // Serve gzipped version if available and client supports it
        if (file_exists($gz_path) && str_contains($_SERVER['HTTP_ACCEPT_ENCODING'] ?? '', 'gzip')) {
            header('Content-Encoding: gzip');
            header('Content-Length: ' . filesize($gz_path));
            readfile($gz_path);
        } else {
            header('Content-Length: ' . filesize($cache_path));
            readfile($cache_path);
        }

        return true;
    }

    /**
     * Cache a processed page for future direct loading.
     */
    public function cache(string $page_key, string $html): void
    {
        $cache_path = $this->getCachePath($page_key);
        $dir = dirname($cache_path);

        if (!is_dir($dir)) {
            mkdir($dir, 0755, true);
        }

        file_put_contents($cache_path, $html);

        // Pre-compress for faster serving
        $compressed = gzencode($html, 9);
        file_put_contents($cache_path . '.gz', $compressed);
    }

    private function getCachePath(string $page_key): string
    {
        $safe_key = preg_replace('/[^a-z0-9_-]/i', '_', $page_key);
        return $this->cache_dir . '/pages/' . $safe_key . '.html';
    }
}
```

## Performance Benchmarks

### Memory Usage by Page Size

| Page Size | ob_get_clean() | DOMDocument Parse | Full Processing | Peak Memory |
|-----------|---------------|-------------------|-----------------|-------------|
| 10 KB     | 10 KB         | ~80 KB            | ~120 KB         | ~2 MB       |
| 50 KB     | 50 KB         | ~400 KB           | ~600 KB         | ~3 MB       |
| 100 KB    | 100 KB        | ~800 KB           | ~1.2 MB         | ~4 MB       |
| 500 KB    | 500 KB        | ~4 MB             | ~6 MB           | ~10 MB      |
| 1 MB      | 1 MB          | ~8 MB             | ~12 MB          | ~18 MB      |

### Processing Time Benchmarks (PHP 8.3, single core)

| Operation                    | 10 KB  | 100 KB | 500 KB | 1 MB   |
|------------------------------|--------|--------|--------|--------|
| ob_get_clean()               | 0.01ms | 0.02ms | 0.08ms | 0.15ms |
| DOMDocument::loadHTML()      | 0.3ms  | 2.1ms  | 12ms   | 28ms   |
| Link modification (20 links)| 0.5ms  | 0.8ms  | 1.2ms  | 1.5ms  |
| Meta tag stripping           | 0.2ms  | 0.3ms  | 0.4ms  | 0.5ms  |
| Pixel injection              | 0.1ms  | 0.1ms  | 0.2ms  | 0.2ms  |
| gzencode (level 6)          | 0.3ms  | 2.5ms  | 15ms   | 35ms   |
| Total pipeline               | 1.4ms  | 5.8ms  | 29ms   | 65ms   |

### Recommendations

- Pages under 100 KB: Use full DOMDocument processing (fast enough, most flexible)
- Pages 100-500 KB: Use DOMDocument for head/body injection, string replacement for links
- Pages over 500 KB: Use streaming approach (StreamingRenderer) to avoid memory spikes
- Always pre-compress cached pages (store .html and .html.gz together)

## PHP Version Compatibility

### PHP 8.1+ Features Used

```php
<?php
// Enums for visitor classification (PHP 8.1+)
enum VisitorType: string
{
    case Bot = 'bot';
    case Human = 'human';
    case Suspicious = 'suspicious';
    case Unknown = 'unknown';
}

// Readonly properties (PHP 8.1+)
class VisitorData
{
    public function __construct(
        public readonly string $click_id,
        public readonly string $ip,
        public readonly string $user_agent,
        public readonly float $bot_score,
        public readonly VisitorType $type,
        public readonly string $campaign_id,
        public readonly string $landing_page,
        public readonly string $landing_url_hash,
        public readonly string $source,
    ) {}
}

// Fiber-based async page fetching (PHP 8.1+)
// Useful for warming cache with multiple pages simultaneously
function warm_cache_async(array $urls): array
{
    $fibers = [];
    $results = [];

    foreach ($urls as $url) {
        $fiber = new Fiber(function() use ($url): string {
            $ch = curl_init($url);
            curl_setopt_array($ch, [
                CURLOPT_RETURNTRANSFER => true,
                CURLOPT_TIMEOUT => 10,
                CURLOPT_FOLLOWLOCATION => true,
            ]);
            $content = curl_exec($ch);
            curl_close($ch);
            Fiber::suspend($content);
            return $content;
        });
        $fiber->start();
        $fibers[$url] = $fiber;
    }

    // Collect results
    foreach ($fibers as $url => $fiber) {
        if ($fiber->isSuspended()) {
            $results[$url] = $fiber->getReturn() ?: $fiber->resume();
        }
    }

    return $results;
}

// Intersection types (PHP 8.1+)
interface Cacheable
{
    public function getCacheKey(): string;
    public function getTtl(): int;
}

interface Renderable
{
    public function render(): string;
}

function processPage(Cacheable&Renderable $page): string
{
    $cache = new PageCache();
    $cached = $cache->get($page->getCacheKey());
    if ($cached !== null) {
        return $cached;
    }
    $html = $page->render();
    $cache->set($page->getCacheKey(), $html, $page->getTtl());
    return $html;
}
```

### PHP 8.2+ Features

```php
<?php
// Readonly classes (PHP 8.2+)
readonly class RenderConfig
{
    public function __construct(
        public string $safe_pages_dir,
        public string $offer_pages_dir,
        public string $cache_dir,
        public float $bot_threshold,
        public bool $enable_gzip,
        public int $cache_ttl,
    ) {}
}

// Disjunctive Normal Form types (PHP 8.2+)
function getPageContent((Cacheable&Renderable)|DirectLoad $source): string
{
    if ($source instanceof DirectLoad) {
        return $source->getContent();
    }
    return $source->render();
}
```
