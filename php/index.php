<?php
declare(strict_types=1);

// GhostRoute PHP Frontend - Inline Rendering via Go Core
// No 302 redirects - always inline render content

// Configuration from environment
$goApiUrl = getenv('GHOSTROUTE_API_URL') ?: 'http://ghostroute-api:8080';
$defaultCampaignId = getenv('DEFAULT_CAMPAIGN_ID') ?: '';
$whitePagePath = getenv('WHITE_PAGE_PATH') ?: __DIR__ . '/pages/white.html';
$blackPagePath = getenv('BLACK_PAGE_PATH') ?: __DIR__ . '/pages/black.html';
$timeout = 200; // 200ms timeout for Go API call

// Extract visitor data
$ip = getVisitorIP();
$ua = $_SERVER['HTTP_USER_AGENT'] ?? '';
$headers = getallheaders() ?: [];
$campaignId = getCampaignId();

// Call Go evaluate endpoint
$decision = evaluateVisitor($goApiUrl, $ip, $ua, $headers, $campaignId, $timeout);

// Serve content based on decision (inline render, no redirects)
if ($decision === 'allow') {
    serveBlackPage($blackPagePath);
} else {
    serveWhitePage($whitePagePath);
}

// --- Functions ---

function getVisitorIP(): string {
    $headers = ['HTTP_CF_CONNECTING_IP', 'HTTP_X_REAL_IP', 'HTTP_X_FORWARDED_FOR', 'REMOTE_ADDR'];
    foreach ($headers as $header) {
        if (!empty($_SERVER[$header])) {
            $ip = $_SERVER[$header];
            if (str_contains($ip, ',')) {
                $ip = trim(explode(',', $ip)[0]);
            }
            if (filter_var($ip, FILTER_VALIDATE_IP)) {
                return $ip;
            }
        }
    }
    return '0.0.0.0';
}

function getCampaignId(): string {
    // Try query param first, then path-based routing
    if (!empty($_GET['campaign'])) {
        return $_GET['campaign'];
    }
    $path = trim($_SERVER['REQUEST_URI'] ?? '', '/');
    $parts = explode('/', $path);
    return $parts[0] ?: (getenv('DEFAULT_CAMPAIGN_ID') ?: '');
}

function evaluateVisitor(string $apiUrl, string $ip, string $ua, array $headers, string $campaignId, int $timeoutMs): string {
    $payload = json_encode([
        'ip' => $ip,
        'user_agent' => $ua,
        'headers' => $headers,
        'ja3' => '',
        'campaign_id' => $campaignId,
    ]);

    $ch = curl_init($apiUrl . '/evaluate');
    curl_setopt_array($ch, [
        CURLOPT_POST => true,
        CURLOPT_POSTFIELDS => $payload,
        CURLOPT_HTTPHEADER => ['Content-Type: application/json', 'Accept: application/json'],
        CURLOPT_RETURNTRANSFER => true,
        CURLOPT_TIMEOUT_MS => $timeoutMs,
        CURLOPT_CONNECTTIMEOUT_MS => 100,
    ]);

    $response = curl_exec($ch);
    $httpCode = curl_getinfo($ch, CURLINFO_HTTP_CODE);
    $error = curl_error($ch);
    curl_close($ch);

    // On any error or timeout, fall back to white (safe) page
    if ($response === false || $httpCode !== 200 || !empty($error)) {
        return 'block'; // Safe fallback
    }

    $data = json_decode($response, true);
    if (!is_array($data) || !isset($data['decision'])) {
        return 'block'; // Safe fallback
    }

    return $data['decision'];
}

function serveWhitePage(string $path): void {
    ob_start();
    if (file_exists($path)) {
        include $path;
    } else {
        echo '<!DOCTYPE html><html><head><title>Welcome</title></head><body><h1>Welcome</h1><p>Content coming soon.</p></body></html>';
    }
    $html = ob_get_clean();

    header('HTTP/1.1 200 OK');
    header('Content-Type: text/html; charset=UTF-8');
    header('Cache-Control: public, max-age=3600');
    header('X-Robots-Tag: index, follow');
    header('Content-Length: ' . strlen($html));
    echo $html;
    exit;
}

function serveBlackPage(string $path): void {
    ob_start();
    if (file_exists($path)) {
        include $path;
    } else {
        echo '<!DOCTYPE html><html><head><title>Offer</title></head><body><h1>Special Offer</h1></body></html>';
    }
    $html = ob_get_clean();

    header('HTTP/1.1 200 OK');
    header('Content-Type: text/html; charset=UTF-8');
    header('Cache-Control: no-store, no-cache, must-revalidate');
    header('X-Robots-Tag: noindex, nofollow');
    header('Pragma: no-cache');
    header('Content-Length: ' . strlen($html));
    echo $html;
    exit;
}
