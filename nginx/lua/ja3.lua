-- ja3.lua - JA3 fingerprint computation from ClientHello data
-- Requires: lua-resty-core, resty.md5

local _M = {}

local resty_md5 = require "resty.md5"
local str_byte = string.byte
local str_format = string.format
local table_concat = table.concat

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
