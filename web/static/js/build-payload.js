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

if (!fs.existsSync(OUTPUT_DIR)) {
  fs.mkdirSync(OUTPUT_DIR, { recursive: true });
}

const sourceCode = fs.readFileSync(SOURCE_FILE, 'utf8');

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

const obfuscationResult = JavaScriptObfuscator.obfuscate(sourceCode, {
  ...config,
  sourceMap: true,
  sourceMapMode: 'separate',
  inputFileName: 'fingerprint.js',
  sourceMapFileName: sourceMapFilename
});

const obfuscatedCode = obfuscationResult.getObfuscatedCode();
fs.writeFileSync(path.join(OUTPUT_DIR, outputFilename), obfuscatedCode);

const sourceMap = obfuscationResult.getSourceMap();
fs.writeFileSync(path.join(OUTPUT_DIR, sourceMapFilename), sourceMap);

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
