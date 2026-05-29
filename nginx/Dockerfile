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
    curl \
    && rm -rf /var/lib/apt/lists/*

# Copy built artifacts
COPY --from=builder /usr/sbin/nginx /usr/sbin/nginx
COPY --from=builder /etc/nginx /etc/nginx
COPY --from=builder /usr/local/luajit /usr/local/luajit
COPY --from=builder /usr/local/lua-resty /usr/local/lua-resty
COPY --from=builder /usr/lib/nginx /usr/lib/nginx

# Create required directories
RUN mkdir -p /var/log/nginx /var/cache/nginx /etc/nginx/lua /etc/nginx/ssl

# Copy Lua scripts and config
COPY lua/ /etc/nginx/lua/
COPY nginx.conf /etc/nginx/nginx.conf

EXPOSE 443 80

STOPSIGNAL SIGQUIT
CMD ["nginx", "-g", "daemon off;"]
