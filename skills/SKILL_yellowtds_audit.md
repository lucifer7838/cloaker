# SKILL: YellowTDS PHP Codebase Audit

## Purpose

Comprehensive audit methodology for analyzing the dvygolov/YellowTDS PHP codebase
to inform the GhostRoute reimplementation in Go. This document provides a
file-by-file analysis checklist, extraction patterns for each subsystem, database
migration strategy, and a migration priority matrix.

---

## 1. Core Router Files

### 1.1 index.php - Entry Point

The main entry point handles initial request routing and campaign selection.

**Key extraction patterns:**

```php
<?php
// index.php pattern - Campaign routing based on URL path
require_once 'core.php';
require_once 'settings.php';

$requestUri = $_SERVER['REQUEST_URI'];
$campaigns = loadCampaigns();

foreach ($campaigns as $camp) {
    if (matchesRoute($camp['route'], $requestUri)) {
        $activeCampaign = $camp;
        break;
    }
}

if (!isset($activeCampaign)) {
    // Default campaign or 404 handling
    include 'directload.php';
    exit;
}

// Initialize TDS processing
require_once 'tds.php';
processTDS($activeCampaign, $_SERVER, $_GET, $_POST);
```

**GhostRoute equivalent:** `internal/router/campaign.go` - Use `chi.Router` with
dynamic route registration per campaign.

### 1.2 core.php - Core Initialization

Handles autoloading, global configuration, error handling, and session setup.

```php
<?php
// core.php pattern - Global initialization
error_reporting(0);
ini_set('display_errors', 0);

define('ROOT_DIR', __DIR__);
define('DB_PATH', ROOT_DIR . '/db/db.sqlite');
define('BASES_DIR', ROOT_DIR . '/bases/');
define('LOGS_DIR', ROOT_DIR . '/logs/');

session_start();

// Load global settings from JSON
$globalSettings = json_decode(
    file_get_contents(ROOT_DIR . '/db/common.json'),
    true
);

// Database connection (SQLite)
function getDB() {
    static $db = null;
    if ($db === null) {
        $db = new SQLite3(DB_PATH);
        $db->busyTimeout(5000);
        $db->exec('PRAGMA journal_mode=WAL');
    }
    return $db;
}
```

**GhostRoute equivalent:** `cmd/ghostroute/main.go` - Use `sync.Once` for DB init,
viper for config, structured logging with zerolog.

### 1.3 main.php - Main Logic Dispatcher

Orchestrates the detection pipeline: bot check, geo check, device check, then route.

```php
<?php
// main.php pattern - Detection pipeline
function processRequest($campaign, $request) {
    $result = [
        'is_bot' => false,
        'country' => '',
        'device' => '',
        'os' => '',
        'browser' => '',
    ];

    // Step 1: Bot detection
    require_once 'actions.php';
    $result['is_bot'] = checkIfBot($request);

    if ($result['is_bot']) {
        return handleBot($campaign, $request);
    }

    // Step 2: Geo detection
    require_once ROOT_DIR . '/bases/ipcountry.php';
    $result['country'] = getCountryByIP($request['ip']);

    // Step 3: Device detection
    require_once ROOT_DIR . '/bases/device/DeviceDetector.php';
    $dd = new DeviceDetector($request['ua']);
    $dd->parse();
    $result['device'] = $dd->getDeviceName();
    $result['os'] = $dd->getOs('name');
    $result['browser'] = $dd->getClient('name');

    // Step 4: Apply campaign filters
    return applyFilters($campaign, $result, $request);
}
```

### 1.4 tds.php - Traffic Distribution System

Core TDS logic implementing weighted distribution, A/B testing, and flow selection.

```php
<?php
// tds.php pattern - Traffic distribution
function processTDS($campaign, $server, $get, $post) {
    $click = createClick($campaign, $server, $get);

    // Determine flow based on filters
    $flow = determineFlow($campaign, $click);

    // Log the click
    logClick($click, $flow);

    // Execute the action for this flow
    executeAction($flow, $click, $server);
}

function determineFlow($campaign, $click) {
    $flows = $campaign['flows'];

    foreach ($flows as $flow) {
        if (matchesFilters($flow['filters'], $click)) {
            // Weighted distribution within matching flow
            if (isset($flow['weight'])) {
                return weightedSelect($flow['landings'], $flow['weight']);
            }
            return $flow;
        }
    }

    // Default flow (safe page)
    return $campaign['default_flow'];
}

function weightedSelect($options, $weights) {
    $total = array_sum($weights);
    $rand = mt_rand(1, $total);
    $cumulative = 0;

    for ($i = 0; $i < count($options); $i++) {
        $cumulative += $weights[$i];
        if ($rand <= $cumulative) {
            return $options[$i];
        }
    }
    return $options[0];
}
```

**GhostRoute equivalent:** `internal/tds/distributor.go` - Use atomic counters for
weighted round-robin, separate goroutine for stats aggregation.

### 1.5 campaign.php - Campaign Configuration

Campaign CRUD operations and settings management.

```php
<?php
// campaign.php pattern - Campaign management
function loadCampaigns() {
    $db = getDB();
    $result = $db->query('SELECT * FROM campaigns WHERE active = 1');
    $campaigns = [];
    while ($row = $result->fetchArray(SQLITE3_ASSOC)) {
        $row['flows'] = json_decode($row['flows_json'], true);
        $row['filters'] = json_decode($row['filters_json'], true);
        $campaigns[] = $row;
    }
    return $campaigns;
}

function getCampaignByID($id) {
    $db = getDB();
    $stmt = $db->prepare('SELECT * FROM campaigns WHERE id = :id');
    $stmt->bindValue(':id', $id, SQLITE3_INTEGER);
    $result = $stmt->execute();
    $row = $result->fetchArray(SQLITE3_ASSOC);
    if ($row) {
        $row['flows'] = json_decode($row['flows_json'], true);
        $row['filters'] = json_decode($row['filters_json'], true);
    }
    return $row;
}

function saveCampaign($data) {
    $db = getDB();
    $data['flows_json'] = json_encode($data['flows']);
    $data['filters_json'] = json_encode($data['filters']);
    // INSERT or UPDATE logic...
}
```

### 1.6 settings.php - Global Settings

```php
<?php
// settings.php pattern - Global configuration
function getSettings() {
    static $settings = null;
    if ($settings === null) {
        $path = ROOT_DIR . '/db/common.json';
        $settings = json_decode(file_get_contents($path), true);
    }
    return $settings;
}

function getSetting($key, $default = null) {
    $settings = getSettings();
    return isset($settings[$key]) ? $settings[$key] : $default;
}

// Settings include:
// - timezone: default timezone for logging
// - admin_password: hashed admin password
// - allowed_ips: whitelist for admin access
// - bot_detection_enabled: toggle bot filtering
// - logging_level: debug/info/error
// - default_action: what to do with unmatched traffic
```

### 1.7 actions.php - Action Handlers

```php
<?php
// actions.php pattern - Traffic action execution
function executeAction($flow, $click, $server) {
    $action = $flow['action'];

    switch ($action['type']) {
        case 'redirect':
            $url = replaceMacros($action['url'], $click);
            header("Location: $url", true, $action['code'] ?? 302);
            exit;

        case 'show_html':
            $html = loadLanding($action['landing_id']);
            $html = injectPixels($html, $click);
            echo $html;
            exit;

        case 'reverse_proxy':
            proxyRequest($action['target_url'], $server);
            exit;

        case 'iframe':
            showIframe($action['url'], $click);
            exit;

        case 'curl':
            curlPage($action['url'], $click);
            exit;

        case 'local_file':
            include $action['path'];
            exit;
    }
}
```

### 1.8 cookies.php - Cookie Management

```php
<?php
// cookies.php pattern - Visitor tracking via cookies
function setTrackingCookie($clickId, $campaignId) {
    $cookieName = 'uid_' . $campaignId;
    $cookieValue = $clickId;
    $expire = time() + (86400 * 30); // 30 days

    setcookie($cookieName, $cookieValue, [
        'expires' => $expire,
        'path' => '/',
        'secure' => true,
        'httponly' => true,
        'samesite' => 'Lax'
    ]);
}

function getReturningVisitor($campaignId) {
    $cookieName = 'uid_' . $campaignId;
    if (isset($_COOKIE[$cookieName])) {
        return $_COOKIE[$cookieName]; // Returns original click ID
    }
    return null;
}

function clearTrackingCookies() {
    foreach ($_COOKIE as $name => $value) {
        if (strpos($name, 'uid_') === 0) {
            setcookie($name, '', time() - 3600, '/');
        }
    }
}
```

### 1.9 debug.php - Debug Mode

```php
<?php
// debug.php pattern - Debug output for testing
function debugOutput($click, $flow, $campaign) {
    if (!getSetting('debug_mode')) return;

    header('Content-Type: text/html');
    echo '<pre>';
    echo "Campaign: {$campaign['name']}\n";
    echo "Flow: {$flow['name']}\n";
    echo "IP: {$click['ip']}\n";
    echo "Country: {$click['country']}\n";
    echo "Device: {$click['device']}\n";
    echo "OS: {$click['os']}\n";
    echo "Browser: {$click['browser']}\n";
    echo "Is Bot: " . ($click['is_bot'] ? 'YES' : 'NO') . "\n";
    echo "UA: {$click['ua']}\n";
    echo "Referer: {$click['referer']}\n";
    echo '</pre>';
}
```

### 1.10 directload.php - Direct Page Loading

```php
<?php
// directload.php pattern - Serve local HTML files directly
function directLoad($path, $click = null) {
    $fullPath = ROOT_DIR . '/landings/' . $path;

    if (!file_exists($fullPath)) {
        http_response_code(404);
        exit;
    }

    $content = file_get_contents($fullPath);

    if ($click) {
        // Replace macros in HTML content
        $content = replaceMacros($content, $click);
        // Inject tracking pixels
        $content = injectPixels($content, $click);
    }

    echo $content;
}
```

### 1.11 htmlprocessing.php - HTML Processing

```php
<?php
// htmlprocessing.php pattern - HTML manipulation
function processHTML($html, $options) {
    // Remove comments if configured
    if ($options['strip_comments']) {
        $html = preg_replace('/<!--.*?-->/s', '', $html);
    }

    // Minify if configured
    if ($options['minify']) {
        $html = preg_replace('/\s+/', ' ', $html);
        $html = str_replace('> <', '><', $html);
    }

    // Add base tag for relative URLs
    if (isset($options['base_url'])) {
        $baseTag = '<base href="' . $options['base_url'] . '">';
        $html = str_replace('<head>', '<head>' . $baseTag, $html);
    }

    return $html;
}
```

### 1.12 htmlinject.php - HTML Injection

```php
<?php
// htmlinject.php pattern - Pixel and script injection
function injectPixels($html, $click) {
    $pixels = getPixelsForCampaign($click['campaign_id']);
    $pixelHtml = '';

    foreach ($pixels as $pixel) {
        switch ($pixel['type']) {
            case 'facebook':
                $pixelHtml .= getFBPixelCode($pixel['id'], $click);
                break;
            case 'google':
                $pixelHtml .= getGACode($pixel['id']);
                break;
            case 'custom':
                $pixelHtml .= $pixel['code'];
                break;
        }
    }

    // Inject before </body>
    $html = str_replace('</body>', $pixelHtml . '</body>', $html);
    return $html;
}

function getFBPixelCode($pixelId, $click) {
    return "<script>
    !function(f,b,e,v,n,t,s){if(f.fbq)return;n=f.fbq=function(){
    n.callMethod?n.callMethod.apply(n,arguments):n.queue.push(arguments)};
    if(!f._fbq)f._fbq=n;n.push=n;n.loaded=!0;n.version='2.0';
    n.queue=[];t=b.createElement(e);t.async=!0;t.src=v;
    s=b.getElementsByTagName(e)[0];s.parentNode.insertBefore(t,s)}
    (window,document,'script','https://connect.facebook.net/en_US/fbevents.js');
    fbq('init', '$pixelId');
    fbq('track', 'PageView');
    </script>";
}
```

### 1.13 logging.php - Click Logging

```php
<?php
// logging.php pattern - Click and event logging to SQLite
function logClick($click, $flow) {
    $db = getDB();
    $stmt = $db->prepare('
        INSERT INTO clicks
        (campaign_id, flow_id, ip, country, city, device, os, browser,
         ua, referer, landing_url, offer_url, timestamp, is_bot, sub_id)
        VALUES
        (:campaign_id, :flow_id, :ip, :country, :city, :device, :os,
         :browser, :ua, :referer, :landing, :offer, :ts, :is_bot, :sub_id)
    ');

    $stmt->bindValue(':campaign_id', $click['campaign_id']);
    $stmt->bindValue(':flow_id', $flow['id']);
    $stmt->bindValue(':ip', $click['ip']);
    $stmt->bindValue(':country', $click['country']);
    $stmt->bindValue(':city', $click['city'] ?? '');
    $stmt->bindValue(':device', $click['device']);
    $stmt->bindValue(':os', $click['os']);
    $stmt->bindValue(':browser', $click['browser']);
    $stmt->bindValue(':ua', $click['ua']);
    $stmt->bindValue(':referer', $click['referer']);
    $stmt->bindValue(':landing', $flow['landing_url'] ?? '');
    $stmt->bindValue(':offer', $flow['offer_url'] ?? '');
    $stmt->bindValue(':ts', time());
    $stmt->bindValue(':is_bot', $click['is_bot'] ? 1 : 0);
    $stmt->bindValue(':sub_id', $click['sub_id'] ?? '');

    $stmt->execute();
    return $db->lastInsertRowID();
}
```

### 1.14 macros.php - URL Macro Replacement

```php
<?php
// macros.php pattern - Dynamic URL parameter substitution
function replaceMacros($template, $click) {
    $macros = [
        '{click_id}'    => $click['id'] ?? '',
        '{campaign_id}' => $click['campaign_id'] ?? '',
        '{ip}'          => $click['ip'] ?? '',
        '{country}'     => $click['country'] ?? '',
        '{city}'        => $click['city'] ?? '',
        '{device}'      => $click['device'] ?? '',
        '{os}'          => $click['os'] ?? '',
        '{browser}'     => $click['browser'] ?? '',
        '{ua}'          => urlencode($click['ua'] ?? ''),
        '{referer}'     => urlencode($click['referer'] ?? ''),
        '{timestamp}'   => time(),
        '{sub_id}'      => $click['sub_id'] ?? '',
        '{sub1}'        => $click['sub1'] ?? '',
        '{sub2}'        => $click['sub2'] ?? '',
        '{sub3}'        => $click['sub3'] ?? '',
        '{sub4}'        => $click['sub4'] ?? '',
        '{sub5}'        => $click['sub5'] ?? '',
    ];

    return str_replace(array_keys($macros), array_values($macros), $template);
}
```

### 1.15 next.php - Sequential Landing Rotation

```php
<?php
// next.php pattern - Round-robin landing page selection
function getNextLanding($campaignId, $landings) {
    $db = getDB();
    $stmt = $db->prepare(
        'SELECT last_index FROM campaign_rotation WHERE campaign_id = :id'
    );
    $stmt->bindValue(':id', $campaignId);
    $result = $stmt->execute();
    $row = $result->fetchArray(SQLITE3_ASSOC);

    $currentIndex = $row ? $row['last_index'] : -1;
    $nextIndex = ($currentIndex + 1) % count($landings);

    // Update rotation counter
    $db->exec("INSERT OR REPLACE INTO campaign_rotation
        (campaign_id, last_index) VALUES ($campaignId, $nextIndex)");

    return $landings[$nextIndex];
}
```

### 1.16 paths.php - Path Resolution

```php
<?php
// paths.php pattern - File and URL path utilities
function getLandingPath($landingId) {
    return ROOT_DIR . '/landings/' . $landingId . '/index.html';
}

function getAssetPath($campaignId, $asset) {
    return ROOT_DIR . '/assets/' . $campaignId . '/' . $asset;
}

function getRelativeUrl($path) {
    $base = getSetting('base_url', '/');
    return rtrim($base, '/') . '/' . ltrim($path, '/');
}

function resolveTemplatePath($template, $campaign) {
    $paths = [
        ROOT_DIR . '/templates/' . $campaign['id'] . '/' . $template,
        ROOT_DIR . '/templates/default/' . $template,
        ROOT_DIR . '/templates/' . $template,
    ];

    foreach ($paths as $path) {
        if (file_exists($path)) return $path;
    }
    return null;
}
```

### 1.17 phpclient.php - PHP-to-PHP Communication

```php
<?php
// phpclient.php pattern - Server-to-server HTTP client
function phpClient($url, $method = 'GET', $data = null, $headers = []) {
    $ch = curl_init();

    curl_setopt_array($ch, [
        CURLOPT_URL => $url,
        CURLOPT_RETURNTRANSFER => true,
        CURLOPT_FOLLOWLOCATION => true,
        CURLOPT_TIMEOUT => 10,
        CURLOPT_SSL_VERIFYPEER => false,
        CURLOPT_HTTPHEADER => $headers,
    ]);

    if ($method === 'POST') {
        curl_setopt($ch, CURLOPT_POST, true);
        curl_setopt($ch, CURLOPT_POSTFIELDS,
            is_array($data) ? http_build_query($data) : $data);
    }

    $response = curl_exec($ch);
    $httpCode = curl_getinfo($ch, CURLINFO_HTTP_CODE);
    curl_close($ch);

    return ['body' => $response, 'code' => $httpCode];
}
```

### 1.18 redirect.php - Redirect Handling

```php
<?php
// redirect.php pattern - Various redirect methods
function doRedirect($url, $method = '302', $click = null) {
    $url = $click ? replaceMacros($url, $click) : $url;

    switch ($method) {
        case '301':
            header("Location: $url", true, 301);
            break;
        case '302':
            header("Location: $url", true, 302);
            break;
        case 'meta':
            echo "<html><head><meta http-equiv='refresh' content='0;url=$url'></head></html>";
            break;
        case 'js':
            echo "<script>window.location.href='$url';</script>";
            break;
        case 'double_meta':
            // Two-stage redirect via intermediate page
            $intermediate = getSetting('base_url') . '/r/' . base64_encode($url);
            echo "<html><head><meta http-equiv='refresh' content='0;url=$intermediate'></head></html>";
            break;
    }
    exit;
}
```

### 1.19 requestfunc.php - Request Parsing

```php
<?php
// requestfunc.php pattern - Extract and normalize request data
function parseRequest($server, $get) {
    return [
        'ip'        => getRealIP($server),
        'ua'        => $server['HTTP_USER_AGENT'] ?? '',
        'referer'   => $server['HTTP_REFERER'] ?? '',
        'lang'      => parseAcceptLanguage($server['HTTP_ACCEPT_LANGUAGE'] ?? ''),
        'method'    => $server['REQUEST_METHOD'],
        'uri'       => $server['REQUEST_URI'],
        'host'      => $server['HTTP_HOST'] ?? '',
        'scheme'    => isHTTPS($server) ? 'https' : 'http',
        'query'     => $get,
        'headers'   => getallheaders(),
        'timestamp' => time(),
    ];
}

function getRealIP($server) {
    $headers = [
        'HTTP_CF_CONNECTING_IP',    // Cloudflare
        'HTTP_X_REAL_IP',           // Nginx proxy
        'HTTP_X_FORWARDED_FOR',     // Standard proxy
        'REMOTE_ADDR',             // Direct connection
    ];

    foreach ($headers as $header) {
        if (!empty($server[$header])) {
            $ip = $server[$header];
            // X-Forwarded-For may contain multiple IPs
            if (strpos($ip, ',') !== false) {
                $ip = trim(explode(',', $ip)[0]);
            }
            if (filter_var($ip, FILTER_VALIDATE_IP)) {
                return $ip;
            }
        }
    }
    return '0.0.0.0';
}
```

### 1.20 send.php - Response Delivery

```php
<?php
// send.php pattern - Response output with proper headers
function sendHTML($content, $statusCode = 200) {
    http_response_code($statusCode);
    header('Content-Type: text/html; charset=utf-8');
    header('Cache-Control: no-store, no-cache, must-revalidate');
    header('X-Content-Type-Options: nosniff');
    echo $content;
    exit;
}

function sendJSON($data, $statusCode = 200) {
    http_response_code($statusCode);
    header('Content-Type: application/json');
    echo json_encode($data);
    exit;
}

function sendFile($path, $mimeType = null) {
    if (!file_exists($path)) {
        http_response_code(404);
        exit;
    }
    $mimeType = $mimeType ?? mime_content_type($path);
    header("Content-Type: $mimeType");
    header('Content-Length: ' . filesize($path));
    readfile($path);
    exit;
}
```

### 1.21 abtest.php - A/B Testing

```php
<?php
// abtest.php pattern - Split testing with persistent assignment
function getABVariant($campaignId, $visitorId, $variants) {
    // Check for existing assignment
    $db = getDB();
    $stmt = $db->prepare(
        'SELECT variant FROM ab_assignments
         WHERE campaign_id = :cid AND visitor_id = :vid'
    );
    $stmt->bindValue(':cid', $campaignId);
    $stmt->bindValue(':vid', $visitorId);
    $result = $stmt->execute();
    $row = $result->fetchArray(SQLITE3_ASSOC);

    if ($row) {
        return $row['variant'];
    }

    // Assign new variant based on weights
    $variant = weightedSelect(
        array_keys($variants),
        array_values($variants)
    );

    // Persist assignment
    $stmt = $db->prepare(
        'INSERT INTO ab_assignments (campaign_id, visitor_id, variant, created_at)
         VALUES (:cid, :vid, :variant, :ts)'
    );
    $stmt->bindValue(':cid', $campaignId);
    $stmt->bindValue(':vid', $visitorId);
    $stmt->bindValue(':variant', $variant);
    $stmt->bindValue(':ts', time());
    $stmt->execute();

    return $variant;
}
```

### 1.22 currency.php - Currency Conversion

```php
<?php
// currency.php pattern - Postback currency conversion
function convertCurrency($amount, $from, $to) {
    if ($from === $to) return $amount;

    $rates = getCachedRates();
    if (!isset($rates[$from]) || !isset($rates[$to])) {
        return $amount; // Cannot convert, return original
    }

    // Convert to USD base, then to target
    $usdAmount = $amount / $rates[$from];
    return round($usdAmount * $rates[$to], 2);
}

function getCachedRates() {
    $cacheFile = ROOT_DIR . '/db/rates_cache.json';
    $maxAge = 3600; // 1 hour cache

    if (file_exists($cacheFile)) {
        $data = json_decode(file_get_contents($cacheFile), true);
        if (time() - $data['timestamp'] < $maxAge) {
            return $data['rates'];
        }
    }

    // Fetch fresh rates (fallback to cached if API fails)
    $fresh = fetchExchangeRates();
    if ($fresh) {
        file_put_contents($cacheFile, json_encode([
            'timestamp' => time(),
            'rates' => $fresh
        ]));
        return $fresh;
    }

    return isset($data['rates']) ? $data['rates'] : ['USD' => 1];
}
```

---

## 2. Database Migration Plan

### 2.1 SQLite Schema (db/db.sql)

```sql
-- Core tables extracted from YellowTDS db/db.sql
CREATE TABLE campaigns (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    route TEXT NOT NULL,
    active INTEGER DEFAULT 1,
    flows_json TEXT,
    filters_json TEXT,
    settings_json TEXT,
    created_at INTEGER,
    updated_at INTEGER
);

CREATE TABLE clicks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    campaign_id INTEGER,
    flow_id INTEGER,
    ip TEXT,
    country TEXT,
    city TEXT,
    device TEXT,
    os TEXT,
    browser TEXT,
    ua TEXT,
    referer TEXT,
    landing_url TEXT,
    offer_url TEXT,
    timestamp INTEGER,
    is_bot INTEGER DEFAULT 0,
    sub_id TEXT,
    cost REAL DEFAULT 0,
    revenue REAL DEFAULT 0,
    FOREIGN KEY (campaign_id) REFERENCES campaigns(id)
);

CREATE TABLE conversions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    click_id INTEGER,
    campaign_id INTEGER,
    status TEXT DEFAULT 'pending',
    payout REAL DEFAULT 0,
    currency TEXT DEFAULT 'USD',
    timestamp INTEGER,
    postback_data TEXT,
    FOREIGN KEY (click_id) REFERENCES clicks(id)
);

CREATE TABLE ab_assignments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    campaign_id INTEGER,
    visitor_id TEXT,
    variant TEXT,
    created_at INTEGER
);

CREATE TABLE campaign_rotation (
    campaign_id INTEGER PRIMARY KEY,
    last_index INTEGER DEFAULT 0
);

CREATE INDEX idx_clicks_campaign ON clicks(campaign_id);
CREATE INDEX idx_clicks_timestamp ON clicks(timestamp);
CREATE INDEX idx_clicks_ip ON clicks(ip);
CREATE INDEX idx_conversions_click ON conversions(click_id);
CREATE INDEX idx_ab_campaign_visitor ON ab_assignments(campaign_id, visitor_id);
```

### 2.2 Migration to PostgreSQL (GhostRoute Target)

```sql
-- GhostRoute PostgreSQL schema
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE campaigns (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) NOT NULL,
    route VARCHAR(512) NOT NULL UNIQUE,
    active BOOLEAN DEFAULT true,
    flows JSONB NOT NULL DEFAULT '[]',
    filters JSONB NOT NULL DEFAULT '{}',
    settings JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Clicks go to ClickHouse for high-volume analytics
-- Only recent/active data in PostgreSQL for real-time lookups
CREATE TABLE active_clicks (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    campaign_id UUID REFERENCES campaigns(id),
    visitor_fingerprint VARCHAR(64),
    ip INET NOT NULL,
    country CHAR(2),
    created_at TIMESTAMPTZ DEFAULT NOW(),
    flow_id VARCHAR(64),
    is_bot BOOLEAN DEFAULT false
);

CREATE INDEX idx_active_clicks_campaign ON active_clicks(campaign_id);
CREATE INDEX idx_active_clicks_fingerprint ON active_clicks(visitor_fingerprint);
CREATE INDEX idx_active_clicks_created ON active_clicks(created_at);

-- Partition by month for automatic cleanup
CREATE TABLE active_clicks_partitioned (
    LIKE active_clicks INCLUDING ALL
) PARTITION BY RANGE (created_at);
```

### 2.3 Data Access Layer (db/db.php - 61KB Analysis)

The `db/db.php` file is the largest in the codebase at 61KB. It contains:

1. **Connection management** - SQLite connection with WAL mode
2. **Campaign CRUD** - All campaign operations (create, read, update, delete, clone)
3. **Click logging** - High-frequency insert operations
4. **Statistics aggregation** - Complex GROUP BY queries for dashboard
5. **Conversion tracking** - Postback processing and attribution
6. **Settings storage** - Key-value settings in JSON files
7. **Data export** - CSV generation for click/conversion data
8. **Cleanup routines** - Old data pruning, database optimization

**GhostRoute migration strategy:**
- Split into separate repository packages: `internal/store/campaign.go`, `internal/store/click.go`, etc.
- Use sqlx for PostgreSQL operations
- Move analytics queries to ClickHouse
- Use Redis for hot caching of campaign data
- Implement write-behind pattern for click logging

### 2.4 Configuration Files

**db/common.json** - Global settings:
```json
{
    "timezone": "UTC",
    "admin_password_hash": "$2y$10$...",
    "allowed_admin_ips": ["127.0.0.1"],
    "bot_detection": true,
    "debug_mode": false,
    "default_action": "show_safe_page",
    "safe_page_url": "https://example.com",
    "logging_enabled": true,
    "log_bots": false,
    "max_click_age_days": 90
}
```

**db/default.json** - Default campaign template:
```json
{
    "name": "New Campaign",
    "flows": [
        {
            "name": "Main",
            "filters": {"countries": [], "devices": [], "os": []},
            "landings": [{"url": "", "weight": 100}],
            "offers": [{"url": "", "weight": 100}],
            "action": {"type": "redirect", "code": 302}
        }
    ],
    "default_flow": {
        "action": {"type": "show_html", "path": "safe.html"}
    }
}
```

---

## 3. Admin Panel Architecture

### 3.1 Directory Structure

```
admin/
  index.php          - Dashboard (statistics overview)
  login.php          - Authentication (password-based)
  campsettings.php   - Campaign editor (CRUD interface)
  clicks.php         - Click log viewer with filtering
  statistics.php     - Detailed statistics with charts
  export.php         - Data export (CSV/JSON)
  settings.php       - Global settings editor
  assets/
    css/
      bootstrap.min.css
      custom.css
    js/
      app.js
      charts.js
      datatables.min.js
```

### 3.2 Authentication Pattern

```php
<?php
// admin/login.php pattern
session_start();

if ($_SERVER['REQUEST_METHOD'] === 'POST') {
    $password = $_POST['password'] ?? '';
    $stored = getSettings()['admin_password_hash'];

    if (password_verify($password, $stored)) {
        $_SESSION['admin_authenticated'] = true;
        $_SESSION['admin_ip'] = $_SERVER['REMOTE_ADDR'];
        $_SESSION['login_time'] = time();
        header('Location: index.php');
        exit;
    }
    $error = 'Invalid password';
}

// IP whitelist check
function checkAdminAccess() {
    $allowed = getSettings()['allowed_admin_ips'] ?? [];
    if (!empty($allowed) && !in_array($_SERVER['REMOTE_ADDR'], $allowed)) {
        http_response_code(403);
        exit('Access denied');
    }

    if (!isset($_SESSION['admin_authenticated']) || !$_SESSION['admin_authenticated']) {
        header('Location: login.php');
        exit;
    }
}
```

**GhostRoute equivalent:** Replace with JWT-based auth, API-first design,
separate React/Vue frontend. Admin API in `internal/api/admin/`.

---

## 4. Bot Detection System

### 4.1 bases/bots.txt (630KB Bot UA List)

The bot detection file contains approximately 15,000 user-agent patterns:

```text
# Sample patterns from bases/bots.txt
Googlebot
Bingbot
YandexBot
Baiduspider
DuckDuckBot
facebookexternalhit
Twitterbot
rogerbot
linkedinbot
embedly
showyoubot
outbrain
pinterest
slackbot
vkShare
W3C_Validator
whatsapp
Applebot
SemrushBot
AhrefsBot
MJ12bot
DotBot
```

### 4.2 Bot Detection Logic

```php
<?php
// Bot detection combining UA matching and behavioral signals
function checkIfBot($request) {
    // 1. Check UA against bot list
    if (matchesBotUA($request['ua'])) {
        return true;
    }

    // 2. Check for empty/suspicious UA
    if (empty($request['ua']) || strlen($request['ua']) < 10) {
        return true;
    }

    // 3. Check for known bot IPs (datacenter ranges)
    if (isDatacenterIP($request['ip'])) {
        return true;
    }

    // 4. Check for suspicious headers
    if (hasSuspiciousHeaders($request['headers'])) {
        return true;
    }

    return false;
}

function matchesBotUA($ua) {
    static $patterns = null;
    if ($patterns === null) {
        $patterns = file(BASES_DIR . 'bots.txt',
            FILE_IGNORE_NEW_LINES | FILE_SKIP_EMPTY_LINES);
        // Filter out comments
        $patterns = array_filter($patterns, function($p) {
            return $p[0] !== '#';
        });
    }

    $uaLower = strtolower($ua);
    foreach ($patterns as $pattern) {
        if (stripos($uaLower, strtolower($pattern)) !== false) {
            return true;
        }
    }
    return false;
}
```

### 4.3 DeviceDetector Library (bases/device/)

```php
<?php
// DeviceDetector integration for device/OS/browser parsing
require_once BASES_DIR . 'device/DeviceDetector.php';

function detectDevice($ua) {
    $dd = new DeviceDetector($ua);
    $dd->setCache(new StaticCache());
    $dd->parse();

    return [
        'device_type'  => $dd->getDeviceName(),    // desktop, smartphone, tablet
        'device_brand' => $dd->getBrandName(),      // Apple, Samsung, etc.
        'device_model' => $dd->getModel(),          // iPhone 14, Galaxy S23
        'os_name'      => $dd->getOs('name'),       // iOS, Android, Windows
        'os_version'   => $dd->getOs('version'),    // 17.1, 14, 11
        'browser_name' => $dd->getClient('name'),   // Chrome, Safari, Firefox
        'browser_ver'  => $dd->getClient('version'),// 120.0, 17.1
        'is_bot'       => $dd->isBot(),
        'bot_info'     => $dd->getBot(),
    ];
}
```

### 4.4 IP Geolocation (bases/ipcountry.php, bases/iputils.php)

```php
<?php
// bases/ipcountry.php pattern - IP to country lookup
function getCountryByIP($ip) {
    // Uses binary search on sorted IP ranges
    $long = ip2long($ip);
    if ($long === false) return 'XX'; // Invalid IP

    $ranges = loadIPRanges();
    $country = binarySearchIP($ranges, $long);
    return $country ?: 'XX';
}

// bases/iputils.php pattern - IP utility functions
function isIPInRange($ip, $cidr) {
    list($subnet, $mask) = explode('/', $cidr);
    $ipLong = ip2long($ip);
    $subnetLong = ip2long($subnet);
    $maskLong = -1 << (32 - $mask);

    return ($ipLong & $maskLong) === ($subnetLong & $maskLong);
}

function isDatacenterIP($ip) {
    $datacenterRanges = [
        '3.0.0.0/8',       // AWS
        '13.0.0.0/8',      // AWS
        '34.0.0.0/8',      // GCP
        '35.0.0.0/8',      // GCP
        '104.16.0.0/12',   // Cloudflare
        '172.64.0.0/13',   // Cloudflare
        '199.27.128.0/21', // Cloudflare
    ];

    foreach ($datacenterRanges as $range) {
        if (isIPInRange($ip, $range)) {
            return true;
        }
    }
    return false;
}
```

---

## 5. JavaScript Integration

### 5.1 js/detect.js - Client-Side Detection

```javascript
// js/detect.js - Browser fingerprinting and bot detection
(function() {
    var signals = {};

    // Canvas fingerprint
    signals.canvas = (function() {
        var canvas = document.createElement('canvas');
        var ctx = canvas.getContext('2d');
        ctx.textBaseline = 'top';
        ctx.font = '14px Arial';
        ctx.fillText('GhostRoute', 2, 2);
        return canvas.toDataURL().hashCode();
    })();

    // WebGL fingerprint
    signals.webgl = (function() {
        var canvas = document.createElement('canvas');
        var gl = canvas.getContext('webgl');
        if (!gl) return 'none';
        var debugInfo = gl.getExtension('WEBGL_debug_renderer_info');
        return debugInfo ? gl.getParameter(debugInfo.UNMASKED_RENDERER_WEBGL) : 'unknown';
    })();

    // Timezone
    signals.timezone = Intl.DateTimeFormat().resolvedOptions().timeZone;

    // Screen
    signals.screen = screen.width + 'x' + screen.height + 'x' + screen.colorDepth;

    // Languages
    signals.languages = navigator.languages ? navigator.languages.join(',') : navigator.language;

    // Platform
    signals.platform = navigator.platform;

    // Hardware concurrency
    signals.cores = navigator.hardwareConcurrency || 0;

    // Device memory
    signals.memory = navigator.deviceMemory || 0;

    // Touch support
    signals.touch = 'ontouchstart' in window ? 1 : 0;

    // Bot indicators
    signals.webdriver = navigator.webdriver ? 1 : 0;
    signals.phantom = window._phantom || window.callPhantom ? 1 : 0;
    signals.headless = /HeadlessChrome/.test(navigator.userAgent) ? 1 : 0;

    // Send to backend
    window.__ghostSignals = signals;
})();
```

### 5.2 js/connect.js - Backend Communication

```javascript
// js/connect.js - Send detection data to backend
(function() {
    function sendSignals(signals, endpoint) {
        var xhr = new XMLHttpRequest();
        xhr.open('POST', endpoint, true);
        xhr.setRequestHeader('Content-Type', 'application/json');
        xhr.send(JSON.stringify({
            signals: signals,
            url: window.location.href,
            referrer: document.referrer,
            timestamp: Date.now()
        }));

        xhr.onreadystatechange = function() {
            if (xhr.readyState === 4 && xhr.status === 200) {
                var response = JSON.parse(xhr.responseText);
                if (response.action === 'redirect') {
                    window.location.href = response.url;
                } else if (response.action === 'inject') {
                    document.write(response.html);
                }
            }
        };
    }

    // Wait for signals to be collected
    if (window.__ghostSignals) {
        sendSignals(window.__ghostSignals, '/api/phpconnect.php');
    }
})();
```

### 5.3 js/iframe.js - Iframe Injection

```javascript
// js/iframe.js - Load content in hidden/visible iframe
function loadInIframe(url, options) {
    options = options || {};
    var iframe = document.createElement('iframe');
    iframe.src = url;
    iframe.style.width = options.width || '100%';
    iframe.style.height = options.height || '100%';
    iframe.style.border = 'none';
    iframe.style.position = options.hidden ? 'absolute' : 'relative';
    iframe.style.left = options.hidden ? '-9999px' : '0';

    if (options.replace) {
        document.body.innerHTML = '';
    }

    document.body.appendChild(iframe);
}
```

### 5.4 js/replace.js - DOM Content Replacement

```javascript
// js/replace.js - Replace page content dynamically
function replacePage(html) {
    // Full page replacement
    document.open();
    document.write(html);
    document.close();
}

function replaceElement(selector, html) {
    var el = document.querySelector(selector);
    if (el) {
        el.innerHTML = html;
    }
}

function injectAfterLoad(url) {
    var xhr = new XMLHttpRequest();
    xhr.open('GET', url, true);
    xhr.onload = function() {
        if (xhr.status === 200) {
            replacePage(xhr.responseText);
        }
    };
    xhr.send();
}
```

### 5.5 js/obfuscator.php - Dynamic JS Obfuscation

```php
<?php
// js/obfuscator.php - Serve obfuscated JavaScript
header('Content-Type: application/javascript');

$script = $_GET['s'] ?? 'detect';
$allowedScripts = ['detect', 'connect', 'iframe', 'replace'];

if (!in_array($script, $allowedScripts)) {
    http_response_code(404);
    exit;
}

$js = file_get_contents(__DIR__ . "/$script.js");

// Basic obfuscation (in production, use more sophisticated methods)
function obfuscateJS($code) {
    // Variable name randomization
    $varMap = [];
    preg_match_all('/var\s+(\w+)/', $code, $matches);
    foreach (array_unique($matches[1]) as $var) {
        $varMap[$var] = '_' . bin2hex(random_bytes(4));
    }

    foreach ($varMap as $original => $obfuscated) {
        $code = preg_replace('/\b' . preg_quote($original) . '\b/', $obfuscated, $code);
    }

    // String encoding
    $code = preg_replace_callback("/'([^']+)'/", function($m) {
        $hex = '';
        for ($i = 0; $i < strlen($m[1]); $i++) {
            $hex .= '\x' . dechex(ord($m[1][$i]));
        }
        return "'$hex'";
    }, $code);

    return $code;
}

echo obfuscateJS($js);
```

---

## 6. API Endpoint Mapping

### 6.1 api/postback.php - Conversion Tracking

```php
<?php
// api/postback.php - Handle affiliate network postbacks
require_once '../core.php';

$clickId = $_GET['click_id'] ?? $_GET['clickid'] ?? $_GET['cid'] ?? '';
$payout = floatval($_GET['payout'] ?? $_GET['sum'] ?? 0);
$currency = $_GET['currency'] ?? 'USD';
$status = $_GET['status'] ?? 'approved';

if (empty($clickId)) {
    http_response_code(400);
    exit('Missing click_id');
}

// Find the original click
$db = getDB();
$stmt = $db->prepare('SELECT * FROM clicks WHERE id = :id OR sub_id = :sub');
$stmt->bindValue(':id', $clickId);
$stmt->bindValue(':sub', $clickId);
$result = $stmt->execute();
$click = $result->fetchArray(SQLITE3_ASSOC);

if (!$click) {
    http_response_code(404);
    exit('Click not found');
}

// Record conversion
$stmt = $db->prepare('
    INSERT INTO conversions (click_id, campaign_id, status, payout, currency, timestamp, postback_data)
    VALUES (:click_id, :campaign_id, :status, :payout, :currency, :ts, :data)
');
$stmt->bindValue(':click_id', $click['id']);
$stmt->bindValue(':campaign_id', $click['campaign_id']);
$stmt->bindValue(':status', $status);
$stmt->bindValue(':payout', $payout);
$stmt->bindValue(':currency', $currency);
$stmt->bindValue(':ts', time());
$stmt->bindValue(':data', json_encode($_GET));
$stmt->execute();

// Update click revenue
$db->exec("UPDATE clicks SET revenue = revenue + $payout WHERE id = {$click['id']}");

echo 'OK';
```

### 6.2 api/events.php - Event Tracking

```php
<?php
// api/events.php - Track custom events (page views, button clicks, etc.)
require_once '../core.php';
header('Content-Type: application/json');

$input = json_decode(file_get_contents('php://input'), true);
if (!$input || !isset($input['event'])) {
    http_response_code(400);
    echo json_encode(['error' => 'Invalid payload']);
    exit;
}

$event = [
    'click_id'    => $input['click_id'] ?? '',
    'event_name'  => $input['event'],
    'event_data'  => json_encode($input['data'] ?? []),
    'timestamp'   => time(),
    'ip'          => getRealIP($_SERVER),
    'ua'          => $_SERVER['HTTP_USER_AGENT'] ?? '',
];

$db = getDB();
$stmt = $db->prepare('
    INSERT INTO events (click_id, event_name, event_data, timestamp, ip, ua)
    VALUES (:click_id, :event, :data, :ts, :ip, :ua)
');
foreach ($event as $key => $value) {
    $stmt->bindValue(':' . ($key === 'event_name' ? 'event' : ($key === 'event_data' ? 'data' : ($key === 'timestamp' ? 'ts' : $key))), $value);
}
$stmt->execute();

echo json_encode(['status' => 'ok']);
```

### 6.3 api/phpconnect.php - PHP Bridge

```php
<?php
// api/phpconnect.php - Receive client-side signals and decide action
require_once '../core.php';
header('Content-Type: application/json');

$input = json_decode(file_get_contents('php://input'), true);
if (!$input || !isset($input['signals'])) {
    echo json_encode(['action' => 'none']);
    exit;
}

$signals = $input['signals'];
$request = parseRequest($_SERVER, $_GET);

// Enhanced bot detection using JS signals
$isBotJS = false;
if ($signals['webdriver'] || $signals['phantom'] || $signals['headless']) {
    $isBotJS = true;
}

// Determine action based on combined server+client detection
$click = createClick(getCurrentCampaign(), $_SERVER, $_GET);
$click['js_signals'] = $signals;
$click['is_bot'] = $click['is_bot'] || $isBotJS;

if ($click['is_bot']) {
    echo json_encode(['action' => 'none']);
} else {
    $flow = determineFlow(getCurrentCampaign(), $click);
    echo json_encode([
        'action' => $flow['action']['type'] === 'redirect' ? 'redirect' : 'none',
        'url' => $flow['action']['url'] ?? ''
    ]);
}
```

### 6.4 api/updateparams.php - Dynamic Parameter Updates

```php
<?php
// api/updateparams.php - Update campaign parameters at runtime
require_once '../core.php';
checkAdminAccess();
header('Content-Type: application/json');

$input = json_decode(file_get_contents('php://input'), true);
$campaignId = $input['campaign_id'] ?? null;
$params = $input['params'] ?? [];

if (!$campaignId || empty($params)) {
    http_response_code(400);
    echo json_encode(['error' => 'Missing campaign_id or params']);
    exit;
}

$db = getDB();
$campaign = getCampaignByID($campaignId);
if (!$campaign) {
    http_response_code(404);
    echo json_encode(['error' => 'Campaign not found']);
    exit;
}

// Merge new parameters
$settings = json_decode($campaign['settings_json'], true) ?: [];
$settings = array_merge($settings, $params);

$stmt = $db->prepare('UPDATE campaigns SET settings_json = :settings WHERE id = :id');
$stmt->bindValue(':settings', json_encode($settings));
$stmt->bindValue(':id', $campaignId);
$stmt->execute();

echo json_encode(['status' => 'updated', 'settings' => $settings]);
```

---

## 7. Reverse Proxy Analysis

### 7.1 reverse/.htaccess

```apache
# reverse/.htaccess - Apache rewrite for reverse proxy mode
RewriteEngine On
RewriteBase /

# Pass all requests to index.php
RewriteCond %{REQUEST_FILENAME} !-f
RewriteCond %{REQUEST_FILENAME} !-d
RewriteRule ^(.*)$ index.php?__path=$1 [QSA,L]

# Block direct access to PHP files
RewriteRule ^(?!index\.php) - [F,L]

# Security headers
Header set X-Content-Type-Options "nosniff"
Header set X-Frame-Options "SAMEORIGIN"
```

### 7.2 reverse/index.php - Reverse Proxy Entry

```php
<?php
// reverse/index.php - Transparent reverse proxy for cloaking
require_once '../core.php';

$targetUrl = getSetting('reverse_proxy_target');
$requestPath = $_GET['__path'] ?? '';
$fullUrl = rtrim($targetUrl, '/') . '/' . ltrim($requestPath, '/');

// Forward the request
$ch = curl_init($fullUrl);
curl_setopt_array($ch, [
    CURLOPT_RETURNTRANSFER => true,
    CURLOPT_FOLLOWLOCATION => true,
    CURLOPT_HEADER => true,
    CURLOPT_HTTPHEADER => [
        'X-Forwarded-For: ' . getRealIP($_SERVER),
        'X-Real-IP: ' . getRealIP($_SERVER),
        'Host: ' . parse_url($targetUrl, PHP_URL_HOST),
    ],
]);

if ($_SERVER['REQUEST_METHOD'] === 'POST') {
    curl_setopt($ch, CURLOPT_POST, true);
    curl_setopt($ch, CURLOPT_POSTFIELDS, file_get_contents('php://input'));
}

$response = curl_exec($ch);
$headerSize = curl_getinfo($ch, CURLINFO_HEADER_SIZE);
$headers = substr($response, 0, $headerSize);
$body = substr($response, $headerSize);
$httpCode = curl_getinfo($ch, CURLINFO_HTTP_CODE);
curl_close($ch);

// Forward response headers
foreach (explode("\r\n", $headers) as $header) {
    if (stripos($header, 'content-type:') === 0 ||
        stripos($header, 'content-disposition:') === 0) {
        header($header);
    }
}

http_response_code($httpCode);

// Rewrite URLs in HTML responses
if (strpos($body, '<html') !== false) {
    $body = str_replace($targetUrl, getSetting('base_url'), $body);
}

echo $body;
```

### 7.3 reverse/no.php - Block Handler

```php
<?php
// reverse/no.php - Display block page or redirect blocked visitors
require_once '../core.php';

$action = getSetting('block_action', 'safe_page');

switch ($action) {
    case 'safe_page':
        $safePage = getSetting('safe_page_url', 'https://google.com');
        header("Location: $safePage", true, 302);
        break;

    case '403':
        http_response_code(403);
        echo '<h1>403 Forbidden</h1>';
        break;

    case '404':
        http_response_code(404);
        echo '<h1>404 Not Found</h1>';
        break;

    case 'blank':
        echo '';
        break;
}
exit;
```

---

## 8. Migration Priority Matrix

| Priority | Component | Effort | Strategy | GhostRoute Package |
|----------|-----------|--------|----------|-------------------|
| P0 | TDS Router (tds.php, main.php) | High | Rewrite in Go | `internal/tds/` |
| P0 | Campaign Config (campaign.php) | Medium | Rewrite with JSONB | `internal/store/campaign.go` |
| P0 | Bot Detection (bases/) | High | Rewrite + enhance | `internal/detect/bot.go` |
| P1 | Click Logging (logging.php) | Medium | ClickHouse + Redis | `internal/analytics/` |
| P1 | Request Parsing (requestfunc.php) | Low | Standard Go net/http | `internal/middleware/` |
| P1 | Action Handlers (actions.php) | Medium | Rewrite | `internal/action/` |
| P1 | Cookie Tracking (cookies.php) | Low | Rewrite | `internal/track/cookie.go` |
| P2 | Redirect Methods (redirect.php) | Low | Rewrite | `internal/action/redirect.go` |
| P2 | HTML Processing (htmlprocessing.php) | Medium | Rewrite | `internal/render/` |
| P2 | Macro System (macros.php) | Low | Rewrite | `internal/macro/` |
| P2 | A/B Testing (abtest.php) | Medium | Rewrite with Redis | `internal/tds/abtest.go` |
| P3 | Admin Panel (admin/) | High | New SPA frontend | `web/admin/` |
| P3 | JS Detection (js/) | Medium | Rewrite + CreepJS | `web/static/js/` |
| P3 | API Endpoints (api/) | Medium | New REST API | `internal/api/` |
| P3 | Reverse Proxy (reverse/) | Low | Nginx handles this | Nginx config |
| Discard | Currency (currency.php) | - | Not needed initially | - |
| Discard | PHP Client (phpclient.php) | - | Go native HTTP | - |
| Discard | Direct Load (directload.php) | - | Covered by action handlers | - |

---

## 9. Dependency Graph

```
                    index.php
                       |
                    core.php
                   /   |   \
            settings  db/db  paths
                 |      |
              main.php  |
             /   |   \  |
      actions  tds.php  campaign.php
       / | \      |
redirect  |  htmlinject
  |     send     |
macros    |   htmlprocessing
          |
       logging ---- cookies
          |
        debug

    Bot Detection Pipeline:
    requestfunc.php --> bases/bots.txt (UA matching)
                   --> bases/device/   (DeviceDetector)
                   --> bases/ipcountry.php (Geo lookup)
                   --> bases/iputils.php   (IP range check)

    JS Pipeline:
    js/detect.js --> js/connect.js --> api/phpconnect.php
    js/iframe.js (standalone)
    js/replace.js (standalone)
    js/obfuscator.php (serves obfuscated JS)

    API Layer:
    api/postback.php   --> db/db.php (conversion logging)
    api/events.php     --> db/db.php (event logging)
    api/phpconnect.php --> main.php  (decision engine)
    api/updateparams.php --> campaign.php (runtime config)

    Admin:
    admin/login.php      --> settings (auth)
    admin/index.php      --> db/db.php (stats queries)
    admin/campsettings   --> campaign.php (CRUD)
    admin/clicks.php     --> db/db.php (click log)
    admin/statistics.php --> db/db.php (aggregation)
```

---

## 10. Key Architectural Differences: YellowTDS vs GhostRoute

| Aspect | YellowTDS (PHP) | GhostRoute (Go) |
|--------|-----------------|------------------|
| Runtime | PHP-FPM per-request | Long-running binary with goroutines |
| Database | SQLite single-file | PostgreSQL + ClickHouse + Redis |
| Bot Detection | Static UA list file | ML pipeline + fingerprinting + real-time |
| Session State | PHP sessions + cookies | Redis-backed sessions |
| Config | JSON files on disk | PostgreSQL + hot-reload via Redis pub/sub |
| JS Detection | Basic fingerprinting | CreepJS-based advanced fingerprinting |
| Admin Panel | Server-rendered PHP | Separate SPA (React/Vue) + REST API |
| Deployment | Upload PHP files | Docker containers + orchestration |
| Scaling | Vertical (single server) | Horizontal (stateless workers) |
| Logging | SQLite inserts | Async pipeline to ClickHouse |
| SSL Fingerprint | Not supported | JA3/JA4 via custom Nginx module |
| IP Intelligence | Basic country lookup | ASN + datacenter + VPN + proxy detection |

---

## 11. Extraction Checklist

Use this checklist when auditing each file:

- [ ] Identify all global state and side effects
- [ ] Map function signatures to Go equivalents
- [ ] Note all database queries (for migration to PostgreSQL/ClickHouse)
- [ ] Identify hardcoded values that should be configurable
- [ ] Find security issues (SQL injection, path traversal, XSS)
- [ ] Document the request/response contract
- [ ] Identify caching opportunities
- [ ] Note error handling patterns (or lack thereof)
- [ ] Map external dependencies
- [ ] Identify concurrent access patterns (relevant for Go)

---

*End of SKILL_yellowtds_audit.md*
