# SKILL: Custom Nginx Build with SSL/TLS Fingerprint Module

## Purpose

Build a custom Nginx with SSL/TLS fingerprint extraction capability
to identify visitors by their TLS ClientHello characteristics (JA3/JA4),
enabling bot detection that cannot be spoofed via headers or cookies.

## Version Pins

- Nginx: 1.27.3
- OpenSSL: 3.4.0
- LuaJIT: 2.1-20241113
- lua-nginx-module: 0.10.27
- lua-resty-core: 0.1.29
- ngx_devel_kit: 0.3.3

---

## 1. Multi-Stage Dockerfile

```dockerfile
# Stage 1: Build Nginx with custom modules
FROM debian:bookworm-slim AS builder

ARG NGINX_VERSION=1.27.3
ARG OPENSSL_VERSION=3.4.0
ARG LUAJIT_VERSION=2.1-20241113
ARG LUA_NGINX_MODULE_VERSION=0.10.27
ARG LUA_RESTY_CORE_VERSION=0.1.29
ARG NGX_DEVEL_KIT_VERSION=0.3.3

RUN apt-get update && apt-get install -y \
    build-essential \
    ca-certificates \
    curl \
    libpcre3-dev \
    libgd-dev \
    libgeoip-dev \
    zlib1g-dev \
    perl \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /build

# Download and extract OpenSSL
RUN curl -fSL "https://github.com/openssl/openssl/releases/download/openssl-${OPENSSL_VERSION}/openssl-${OPENSSL_VERSION}.tar.gz" \
    -o openssl.tar.gz \
    && tar xzf openssl.tar.gz \
    && mv openssl-${OPENSSL_VERSION} openssl

# Download and build LuaJIT
RUN curl -fSL "https://github.com/openresty/luajit2/archive/refs/tags/v${LUAJIT_VERSION}.tar.gz" \
    -o luajit.tar.gz \
    && tar xzf luajit.tar.gz \
    && cd luajit2-${LUAJIT_VERSION} \
    && make -j$(nproc) PREFIX=/usr/local/luajit \
    && make install PREFIX=/usr/local/luajit

ENV LUAJIT_LIB=/usr/local/luajit/lib
ENV LUAJIT_INC=/usr/local/luajit/include/luajit-2.1

# Download ngx_devel_kit
RUN curl -fSL "https://github.com/vision5/ngx_devel_kit/archive/refs/tags/v${NGX_DEVEL_KIT_VERSION}.tar.gz" \
    -o ndk.tar.gz \
    && tar xzf ndk.tar.gz

# Download lua-nginx-module
RUN curl -fSL "https://github.com/openresty/lua-nginx-module/archive/refs/tags/v${LUA_NGINX_MODULE_VERSION}.tar.gz" \
    -o lua-nginx.tar.gz \
    && tar xzf lua-nginx.tar.gz

# Download lua-resty-core
RUN curl -fSL "https://github.com/openresty/lua-resty-core/archive/refs/tags/v${LUA_RESTY_CORE_VERSION}.tar.gz" \
    -o lua-resty-core.tar.gz \
    && tar xzf lua-resty-core.tar.gz \
    && cd lua-resty-core-${LUA_RESTY_CORE_VERSION} \
    && make install PREFIX=/usr/local/lua-resty

# Download and build Nginx
RUN curl -fSL "https://nginx.org/download/nginx-${NGINX_VERSION}.tar.gz" \
    -o nginx.tar.gz \
    && tar xzf nginx.tar.gz \
    && cd nginx-${NGINX_VERSION} \
    && ./configure \
        --prefix=/etc/nginx \
        --sbin-path=/usr/sbin/nginx \
        --modules-path=/usr/lib/nginx/modules \
        --conf-path=/etc/nginx/nginx.conf \
        --error-log-path=/var/log/nginx/error.log \
        --http-log-path=/var/log/nginx/access.log \
        --pid-path=/var/run/nginx.pid \
        --lock-path=/var/run/nginx.lock \
        --with-openssl=/build/openssl \
        --with-openssl-opt="enable-tls1_3 enable-ssl-trace" \
        --with-http_ssl_module \
        --with-http_v2_module \
        --with-http_realip_module \
        --with-http_gzip_static_module \
        --with-http_stub_status_module \
        --with-stream \
        --with-stream_ssl_module \
        --with-stream_ssl_preread_module \
        --with-stream_realip_module \
        --add-module=/build/ngx_devel_kit-${NGX_DEVEL_KIT_VERSION} \
        --add-module=/build/lua-nginx-module-${LUA_NGINX_MODULE_VERSION} \
        --with-ld-opt="-Wl,-rpath,/usr/local/luajit/lib" \
    && make -j$(nproc) \
    && make install

# Stage 2: Runtime image
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y \
    libpcre3 \
    zlib1g \
    libgd3 \
    && rm -rf /var/lib/apt/lists/*

# Copy built artifacts
COPY --from=builder /usr/sbin/nginx /usr/sbin/nginx
COPY --from=builder /etc/nginx /etc/nginx
COPY --from=builder /usr/local/luajit /usr/local/luajit
COPY --from=builder /usr/local/lua-resty /usr/local/lua-resty
COPY --from=builder /usr/lib/nginx /usr/lib/nginx

# Create required directories
RUN mkdir -p /var/log/nginx /var/cache/nginx /etc/nginx/lua

# Copy Lua scripts
COPY lua/ /etc/nginx/lua/
COPY nginx.conf /etc/nginx/nginx.conf

EXPOSE 443 80

STOPSIGNAL SIGQUIT
CMD ["nginx", "-g", "daemon off;"]
```

---

## 2. JA3 Fingerprint Algorithm

### Overview

JA3 creates an MD5 hash of specific fields from the TLS ClientHello message:

```
JA3 = MD5(TLSVersion,Ciphers,Extensions,EllipticCurves,EllipticCurvePointFormats)
```

### Field Breakdown

| Field | Description | Example |
|-------|-------------|---------|
| TLSVersion | Client-offered TLS version (decimal) | 771 (TLS 1.2) |
| Ciphers | Comma-separated cipher suite codes | 4866-4867-4865-49196-49200 |
| Extensions | Comma-separated extension type codes | 0-23-65281-10-11-35-16-5 |
| EllipticCurves | Supported groups / named curves | 29-23-24-25 |
| EllipticCurvePointFormats | EC point format types | 0 |

### Computation Steps

1. Extract TLSVersion from ClientHello (e.g., 0x0303 = 771 for TLS 1.2)
2. List cipher suites as dash-separated decimal values (exclude GREASE values)
3. List extensions as dash-separated decimal values (exclude GREASE)
4. List elliptic curves from supported_groups extension (exclude GREASE)
5. List EC point formats from ec_point_formats extension
6. Concatenate all fields with commas: `771,4866-4867-4865,0-23-65281-10-11,29-23-24,0`
7. Compute MD5 hash of the resulting string

### GREASE Values to Exclude

GREASE (Generate Random Extensions And Sustain Extensibility) values follow the
pattern `0x?A?A` where ? is any hex digit:
```
0x0A0A, 0x1A1A, 0x2A2A, 0x3A3A, 0x4A4A, 0x5A5A,
0x6A6A, 0x7A7A, 0x8A8A, 0x9A9A, 0xAAAA, 0xBABA,
0xCACA, 0xDADA, 0xEAEA, 0xFAFA
```

---

## 3. JA4 Fingerprint Format

### Overview

JA4 improves upon JA3 with a structured format: `a_b_c`

```
JA4 = <protocol><version><SNI><cipher_count><ext_count><ALPN>_<sorted_ciphers_hash>_<sorted_extensions_hash>
```

### Component Breakdown

**Section a (fixed-length prefix):**
- Protocol: `t` (TCP) or `q` (QUIC)
- TLS Version: `12` (TLS 1.2), `13` (TLS 1.3)
- SNI: `d` (domain present) or `i` (IP/absent)
- Cipher count: 2-digit decimal (number of ciphers)
- Extension count: 2-digit decimal (number of extensions)
- ALPN: first and last character of ALPN value (e.g., `h2` -> `h2`)

**Section b (12 chars):**
- First 12 characters of SHA256 of sorted cipher suites (dash-separated, GREASE excluded)

**Section c (12 chars):**
- First 12 characters of SHA256 of sorted extensions (dash-separated, GREASE excluded,
  SNI and ALPN extensions removed)

### Example

```
JA4: t13d1516h2_8daaf6152771_e5627efa2ab1
      |  | |  | |  |              |
      |  | |  | |  |              +-- SHA256(sorted extensions)[:12]
      |  | |  | |  +-- SHA256(sorted ciphers)[:12]
      |  | |  | +-- ALPN (h2)
      |  | |  +-- 16 extensions
      |  | +-- 15 ciphers
      |  +-- domain in SNI
      +-- TLS 1.3, TCP
```

---

## 4. Lua JA3 Implementation for Nginx

Save as `/etc/nginx/lua/ja3.lua`:

```lua
-- ja3.lua - JA3 fingerprint computation from ClientHello data
-- Requires: lua-resty-core, resty.md5

local _M = {}

local resty_md5 = require "resty.md5"
local str_byte = string.byte
local str_char = string.char
local str_format = string.format
local table_concat = table.concat
local table_sort = table.sort

-- GREASE values to exclude (decimal representations)
local GREASE = {
    [2570]  = true, [6682]  = true, [10794] = true, [14906] = true,
    [19018] = true, [23130] = true, [27242] = true, [31354] = true,
    [35466] = true, [39578] = true, [43690] = true, [47802] = true,
    [51914] = true, [56026] = true, [60138] = true, [64250] = true,
}

-- Convert 2 bytes (big-endian) to uint16
local function read_uint16(data, offset)
    return str_byte(data, offset) * 256 + str_byte(data, offset + 1)
end

-- Parse ClientHello and extract JA3 components
function _M.parse_client_hello(data)
    if not data or #data < 44 then
        return nil, "data too short"
    end

    local offset = 1

    -- TLS Record Layer
    local content_type = str_byte(data, offset)
    if content_type ~= 22 then -- Handshake
        return nil, "not a handshake message"
    end
    offset = offset + 5 -- skip record header

    -- Handshake header
    local handshake_type = str_byte(data, offset)
    if handshake_type ~= 1 then -- ClientHello
        return nil, "not a ClientHello"
    end
    offset = offset + 4 -- skip handshake header

    -- Client Version (2 bytes)
    local tls_version = read_uint16(data, offset)
    offset = offset + 2

    -- Random (32 bytes)
    offset = offset + 32

    -- Session ID
    local session_id_len = str_byte(data, offset)
    offset = offset + 1 + session_id_len

    -- Cipher Suites
    local ciphers_len = read_uint16(data, offset)
    offset = offset + 2
    local ciphers = {}
    for i = 0, (ciphers_len / 2) - 1 do
        local cipher = read_uint16(data, offset + i * 2)
        if not GREASE[cipher] then
            ciphers[#ciphers + 1] = cipher
        end
    end
    offset = offset + ciphers_len

    -- Compression Methods
    local comp_len = str_byte(data, offset)
    offset = offset + 1 + comp_len

    -- Extensions
    local extensions = {}
    local curves = {}
    local point_formats = {}

    if offset < #data then
        local ext_total_len = read_uint16(data, offset)
        offset = offset + 2
        local ext_end = offset + ext_total_len

        while offset < ext_end do
            local ext_type = read_uint16(data, offset)
            local ext_len = read_uint16(data, offset + 2)
            offset = offset + 4

            if not GREASE[ext_type] then
                extensions[#extensions + 1] = ext_type

                -- Supported Groups (0x000A = 10)
                if ext_type == 10 then
                    local list_len = read_uint16(data, offset)
                    for i = 0, (list_len / 2) - 1 do
                        local curve = read_uint16(data, offset + 2 + i * 2)
                        if not GREASE[curve] then
                            curves[#curves + 1] = curve
                        end
                    end
                end

                -- EC Point Formats (0x000B = 11)
                if ext_type == 11 then
                    local fmt_len = str_byte(data, offset)
                    for i = 1, fmt_len do
                        point_formats[#point_formats + 1] = str_byte(data, offset + i)
                    end
                end
            end

            offset = offset + ext_len
        end
    end

    return {
        version = tls_version,
        ciphers = ciphers,
        extensions = extensions,
        curves = curves,
        point_formats = point_formats,
    }
end

-- Compute JA3 hash from parsed ClientHello
function _M.compute_ja3(parsed)
    if not parsed then
        return nil
    end

    local parts = {
        tostring(parsed.version),
        table_concat(parsed.ciphers, "-"),
        table_concat(parsed.extensions, "-"),
        table_concat(parsed.curves, "-"),
        table_concat(parsed.point_formats, "-"),
    }

    local ja3_str = table_concat(parts, ",")

    -- MD5 hash
    local md5 = resty_md5:new()
    md5:update(ja3_str)
    local digest = md5:final()

    local hex = {}
    for i = 1, #digest do
        hex[i] = str_format("%02x", str_byte(digest, i))
    end

    return table_concat(hex), ja3_str
end

return _M
```

---

## 5. Complete nginx.conf

```nginx
# /etc/nginx/nginx.conf - GhostRoute TLS fingerprinting proxy

worker_processes auto;
error_log /var/log/nginx/error.log warn;
pid /var/run/nginx.pid;

events {
    worker_connections 4096;
    use epoll;
    multi_accept on;
}

# Stream block: SSL preread to extract raw ClientHello before termination
stream {
    # Log format with JA3 placeholder
    log_format stream_log '$remote_addr [$time_local] '
                          '$protocol $status $bytes_sent $bytes_received '
                          '$session_time "$ssl_preread_server_name"';

    # Map to extract SNI for routing
    map $ssl_preread_server_name $backend {
        default https_backend;
    }

    # Frontend listener - captures raw TLS ClientHello
    server {
        listen 443;
        ssl_preread on;

        # Pass raw ClientHello data to the HTTP backend via proxy protocol
        proxy_pass $backend;
        proxy_protocol on;
    }

    # Backend that terminates TLS
    upstream https_backend {
        server 127.0.0.1:8443;
    }
}

http {
    # Lua package paths
    lua_package_path "/etc/nginx/lua/?.lua;/usr/local/lua-resty/lib/lua/?.lua;;";
    lua_package_cpath "/usr/local/luajit/lib/lua/5.1/?.so;;";

    # Shared dict for caching fingerprints
    lua_shared_dict fingerprint_cache 10m;
    lua_shared_dict ja3_stats 1m;

    # Initialize Lua modules
    init_by_lua_block {
        require "resty.core"
        ja3 = require "ja3"
    }

    # Log format including fingerprint headers
    log_format fingerprint '$remote_addr - $remote_user [$time_local] '
                           '"$request" $status $body_bytes_sent '
                           '"$http_referer" "$http_user_agent" '
                           'ja3="$http_x_ja3_hash" ja4="$http_x_ja4_hash"';

    access_log /var/log/nginx/access.log fingerprint;

    # Upstream backend (GhostRoute Go service)
    upstream ghostroute {
        server 127.0.0.1:8080;
        keepalive 64;
    }

    # TLS termination server
    server {
        listen 8443 ssl proxy_protocol;
        server_name _;

        # TLS configuration
        ssl_certificate /etc/nginx/ssl/cert.pem;
        ssl_certificate_key /etc/nginx/ssl/key.pem;
        ssl_protocols TLSv1.2 TLSv1.3;
        ssl_ciphers ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384;
        ssl_prefer_server_ciphers off;
        ssl_session_timeout 1d;
        ssl_session_cache shared:SSL:10m;

        # Real IP from proxy protocol
        set_real_ip_from 127.0.0.1;
        real_ip_header proxy_protocol;

        # Extract JA3 fingerprint via Lua
        set $ja3_hash '';
        set $ja3_str '';
        set $ja4_hash '';

        # Compute fingerprints on each request
        access_by_lua_block {
            local ssl = require "ngx.ssl"

            -- Get raw ClientHello (requires custom patch or ssl_client_hello callback)
            local client_hello_raw = ngx.var.ssl_client_raw_hello
            if client_hello_raw then
                local parsed = ja3.parse_client_hello(client_hello_raw)
                if parsed then
                    local hash, raw_str = ja3.compute_ja3(parsed)
                    ngx.var.ja3_hash = hash or ""
                    ngx.var.ja3_str = raw_str or ""
                end
            end

            -- Fallback: use ssl_ja3_hash variable if available from patched nginx
            if ngx.var.ja3_hash == "" and ngx.var.ssl_ja3_hash then
                ngx.var.ja3_hash = ngx.var.ssl_ja3_hash
            end
        }

        # Proxy to GhostRoute backend with fingerprint headers
        location / {
            proxy_pass http://ghostroute;
            proxy_http_version 1.1;
            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
            proxy_set_header X-Forwarded-Proto $scheme;
            proxy_set_header Connection "";

            # Fingerprint headers injected to backend
            proxy_set_header X-JA3-Hash $ja3_hash;
            proxy_set_header X-JA3-String $ja3_str;
            proxy_set_header X-JA4-Hash $ja4_hash;
            proxy_set_header X-TLS-Version $ssl_protocol;
            proxy_set_header X-TLS-Cipher $ssl_cipher;
            proxy_set_header X-SSL-Session-Reused $ssl_session_reused;
        }

        # Health check endpoint (no fingerprint processing)
        location /health {
            access_by_lua_block { }
            return 200 "ok\n";
        }
    }

    # HTTP to HTTPS redirect
    server {
        listen 80;
        server_name _;
        return 301 https://$host$request_uri;
    }
}
```

---

## 6. Go Backend - Fingerprint Header Processing

```go
package fingerprint

import (
	"crypto/md5"
	"encoding/hex"
	"net/http"
	"strings"
)

// TLSFingerprint represents extracted TLS fingerprint data from Nginx headers.
type TLSFingerprint struct {
	JA3Hash         string
	JA3String       string
	JA4Hash         string
	TLSVersion      string
	TLSCipher       string
	SessionReused   bool
	RawClientHello  []byte
}

// ExtractFromHeaders reads TLS fingerprint data injected by Nginx.
func ExtractFromHeaders(r *http.Request) TLSFingerprint {
	return TLSFingerprint{
		JA3Hash:       r.Header.Get("X-JA3-Hash"),
		JA3String:     r.Header.Get("X-JA3-String"),
		JA4Hash:       r.Header.Get("X-JA4-Hash"),
		TLSVersion:    r.Header.Get("X-TLS-Version"),
		TLSCipher:     r.Header.Get("X-TLS-Cipher"),
		SessionReused: r.Header.Get("X-SSL-Session-Reused") == "r",
	}
}

// IsKnownBot checks if the JA3 hash matches known bot fingerprints.
func (fp TLSFingerprint) IsKnownBot() bool {
	// Known bot JA3 hashes (headless Chrome, curl, Python requests, etc.)
	knownBots := map[string]string{
		"e4f26f64c47e1fc3f9f5e8c5e38a4f0c": "curl/7.x",
		"b32309a26951912be7dba376398abc3b": "Python-urllib",
		"3b5074b1b5d032e5620f69f9f700ff0e": "Python-requests",
		"a0e9f5d64349fb13191bc781f81f42e1": "HeadlessChrome",
		"cd08e31494f9531f560d64c695473da9": "PhantomJS",
		"19e29534fd49dd27d09234e639c4057e": "wget",
		"5d65ea3fb1d4aa7d826733f2c5e97b05": "Go-http-client",
		"1d095e36b45b5a9f56e6caf6b08e4002": "Googlebot",
		"2b3e96781f6c43e8e543652b3adb9240": "Bingbot",
	}

	if _, found := knownBots[fp.JA3Hash]; found {
		return true
	}
	return false
}

// BotName returns the name of the detected bot, or empty string.
func (fp TLSFingerprint) BotName() string {
	knownBots := map[string]string{
		"e4f26f64c47e1fc3f9f5e8c5e38a4f0c": "curl/7.x",
		"b32309a26951912be7dba376398abc3b": "Python-urllib",
		"3b5074b1b5d032e5620f69f9f700ff0e": "Python-requests",
		"a0e9f5d64349fb13191bc781f81f42e1": "HeadlessChrome",
		"cd08e31494f9531f560d64c695473da9": "PhantomJS",
		"19e29534fd49dd27d09234e639c4057e": "wget",
		"5d65ea3fb1d4aa7d826733f2c5e97b05": "Go-http-client",
	}
	return knownBots[fp.JA3Hash]
}

// ComputeJA3 computes JA3 hash from its raw string representation.
func ComputeJA3(ja3String string) string {
	hash := md5.Sum([]byte(ja3String))
	return hex.EncodeToString(hash[:])
}

// Middleware creates an HTTP middleware that extracts and validates fingerprints.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fp := ExtractFromHeaders(r)

		// Store in request context for downstream handlers
		ctx := WithFingerprint(r.Context(), fp)
		r = r.WithContext(ctx)

		// Quick bot check - reject known bad fingerprints
		if fp.IsKnownBot() {
			// Log and serve safe page instead of blocking
			r.Header.Set("X-GhostRoute-Bot", "true")
			r.Header.Set("X-GhostRoute-Bot-Name", fp.BotName())
		}

		next.ServeHTTP(w, r)
	})
}
```

---

## 7. OpenSSL Compilation Flags

Key flags for building OpenSSL with TLS introspection support:

```bash
cd /build/openssl

./config \
    --prefix=/usr/local/openssl \
    --openssldir=/usr/local/openssl \
    enable-tls1_3 \
    enable-ssl-trace \
    enable-ec_nistp_64_gcc_128 \
    no-weak-ssl-ciphers \
    no-ssl3 \
    no-idea \
    no-md2 \
    no-mdc2 \
    no-rc5 \
    no-comp \
    -DOPENSSL_TLS_SECURITY_LEVEL=2

make -j$(nproc)
make install_sw
```

Flags explained:
- `enable-tls1_3`: Required for JA3/JA4 on modern clients
- `enable-ssl-trace`: Enables SSL_trace() for debugging ClientHello parsing
- `enable-ec_nistp_64_gcc_128`: Optimized ECC for x86_64
- `no-weak-ssl-ciphers`: Security hardening
- `no-ssl3`: Disable SSLv3 (POODLE vulnerability)
- `no-comp`: Disable compression (CRIME vulnerability)

---

## 8. Testing Methodology

### Test with curl

```bash
# Basic TLS 1.2 connection
curl -v --tlsv1.2 --tls-max 1.2 https://your-domain.com/api/fingerprint

# TLS 1.3 connection
curl -v --tlsv1.3 https://your-domain.com/api/fingerprint

# Specific cipher suite
curl --ciphers ECDHE-RSA-AES128-GCM-SHA256 https://your-domain.com/api/fingerprint

# Check response headers for fingerprint data
curl -s -D- https://your-domain.com/ 2>/dev/null | grep -i "x-ja"
```

### Test with openssl s_client

```bash
# Full ClientHello dump
openssl s_client -connect your-domain.com:443 -msg 2>&1 | head -50

# TLS 1.2 with specific curves
openssl s_client -connect your-domain.com:443 \
    -tls1_2 \
    -cipher ECDHE-RSA-AES256-GCM-SHA384 \
    -curves P-256:P-384

# TLS 1.3
openssl s_client -connect your-domain.com:443 \
    -tls1_3 \
    -ciphersuites TLS_AES_256_GCM_SHA384

# Extract and display session info
echo | openssl s_client -connect your-domain.com:443 2>/dev/null | \
    openssl x509 -noout -text
```

### Verify JA3 hash manually

```bash
# Capture ClientHello with tshark
tshark -i eth0 -f "tcp port 443" -Y "tls.handshake.type == 1" \
    -T fields -e tls.handshake.version -e tls.handshake.ciphersuite \
    -e tls.handshake.extension.type -e tls.handshake.extensions_supported_group \
    -e tls.handshake.extensions_ec_point_format

# Compute JA3 from captured fields
echo -n "771,4866-4867-4865-49196-49200-49195-49199-52393-52392,0-23-65281-10-11-35-16-5-13-18-51-45-43-27-21,29-23-24,0" | md5sum
```

### Integration test script

```bash
#!/bin/bash
# test_fingerprint.sh - Verify fingerprint extraction is working

DOMAIN="${1:-localhost}"
PORT="${2:-443}"

echo "=== Testing TLS Fingerprint Extraction ==="
echo "Target: ${DOMAIN}:${PORT}"
echo

# Test 1: Basic connection
echo "[Test 1] Basic HTTPS connection..."
RESPONSE=$(curl -sk "https://${DOMAIN}:${PORT}/api/debug/fingerprint" 2>/dev/null)
JA3=$(echo "$RESPONSE" | jq -r '.ja3_hash // empty')
if [ -n "$JA3" ]; then
    echo "  PASS: JA3 hash received: ${JA3}"
else
    echo "  FAIL: No JA3 hash in response"
fi

# Test 2: Different TLS versions produce different hashes
echo "[Test 2] TLS version differentiation..."
JA3_12=$(curl -sk --tlsv1.2 --tls-max 1.2 "https://${DOMAIN}:${PORT}/api/debug/fingerprint" | jq -r '.ja3_hash')
JA3_13=$(curl -sk --tlsv1.3 "https://${DOMAIN}:${PORT}/api/debug/fingerprint" | jq -r '.ja3_hash')
if [ "$JA3_12" != "$JA3_13" ]; then
    echo "  PASS: TLS 1.2 ($JA3_12) != TLS 1.3 ($JA3_13)"
else
    echo "  WARN: Same hash for both versions (may be expected with curl)"
fi

# Test 3: Headers are injected
echo "[Test 3] Header injection..."
HEADERS=$(curl -skI "https://${DOMAIN}:${PORT}/" 2>/dev/null)
if echo "$HEADERS" | grep -qi "x-ja3"; then
    echo "  PASS: X-JA3 header present in response"
else
    echo "  INFO: X-JA3 not in response headers (internal only - check backend)"
fi

echo
echo "=== Tests Complete ==="
```

---

## 9. Stream ssl_preread Configuration Detail

The `ssl_preread` module allows inspecting the ClientHello without terminating TLS:

```nginx
stream {
    # Extract SNI without decrypting
    map $ssl_preread_server_name $upstream_pool {
        ~^api\.     api_backend;
        ~^admin\.   admin_backend;
        default     default_backend;
    }

    # Extract ALPN protocol
    map $ssl_preread_alpn_protocols $proxy_port {
        ~\bh2\b     8443;
        default     8443;
    }

    server {
        listen 443;
        ssl_preread on;

        # Access the raw ClientHello for fingerprinting
        # Note: ssl_preread provides $ssl_preread_server_name,
        # $ssl_preread_protocol, $ssl_preread_alpn_protocols
        # For full ClientHello access, use a custom module or
        # the lua-resty-openssl approach in the http block

        proxy_pass $upstream_pool;
    }

    upstream api_backend {
        server 127.0.0.1:8443;
    }

    upstream admin_backend {
        server 127.0.0.1:9443;
    }

    upstream default_backend {
        server 127.0.0.1:8443;
    }
}
```

---

## 10. Known JA3 Fingerprints Database

Common fingerprints for reference and testing:

| JA3 Hash | Client |
|----------|--------|
| `e4f26f64c47e1fc3f9f5e8c5e38a4f0c` | curl/7.68+ |
| `b32309a26951912be7dba376398abc3b` | Python urllib3 |
| `3b5074b1b5d032e5620f69f9f700ff0e` | Python requests 2.28+ |
| `a0e9f5d64349fb13191bc781f81f42e1` | Headless Chrome 100+ |
| `cd08e31494f9531f560d64c695473da9` | PhantomJS 2.1 |
| `19e29534fd49dd27d09234e639c4057e` | GNU Wget 1.21+ |
| `5d65ea3fb1d4aa7d826733f2c5e97b05` | Go net/http |
| `473cd7cb9faa642487833865d516e578` | Safari 17 (macOS) |
| `b4340fbc30f98b8e13a6d2e4c4af947c` | Chrome 120 (Windows) |
| `9dc949149415e26b5b3d300bc6e38cda` | Firefox 121 (Windows) |

---

*End of SKILL_nginx_ssl_fingerprint.md*
