package qdrant

import (
	"math"
	"regexp"
	"strings"

	"github.com/spaolacci/murmur3"
)

const VectorDim = 128

// FingerprintFeatures contains the raw fingerprint signals used for embedding.
type FingerprintFeatures struct {
	CanvasHash          string   `json:"canvas_hash"`
	WebGLRenderer       string   `json:"webgl_renderer"`
	AudioFingerprint    float64  `json:"audio_fingerprint"`
	ScreenWidth         int      `json:"screen_width"`
	ScreenHeight        int      `json:"screen_height"`
	ScreenDepth         int      `json:"screen_depth"`
	TimezoneOffset      int      `json:"timezone_offset"`
	Languages           []string `json:"languages"`
	InstalledFontsCount int      `json:"installed_fonts_count"`
	Plugins             []string `json:"plugins"`
	HardwareConcurrency int      `json:"hardware_concurrency"`
	DeviceMemory        float64  `json:"device_memory"`
	Platform            string   `json:"platform"`
}

// GenerateEmbedding converts fingerprint features into a 128-dimensional L2-normalized vector.
// Dimension allocation:
//
//	Dims 0-7:     canvas_hash (categorical, 8 dims)
//	Dims 8-23:    webgl_renderer (tokenized string, 16 dims)
//	Dims 24-31:   audio_fingerprint (numerical, 8 dims)
//	Dims 32-39:   screen dimensions (width, height, depth, 8 dims)
//	Dims 40-47:   timezone_offset (numerical, 8 dims)
//	Dims 48-63:   languages (list, 16 dims)
//	Dims 64-71:   installed_fonts_count (numerical, 8 dims)
//	Dims 72-87:   plugins (list, 16 dims)
//	Dims 88-95:   hardware_concurrency (numerical, 8 dims)
//	Dims 96-103:  device_memory (numerical, 8 dims)
//	Dims 104-119: platform (categorical, 16 dims)
//	Dims 120-127: reserved
func GenerateEmbedding(fp *FingerprintFeatures) []float32 {
	vector := make([]float32, VectorDim)

	// Canvas hash -> dims 0-7
	hashCategorical(fp.CanvasHash, 0, 8, vector)

	// WebGL renderer -> dims 8-23 (tokenized)
	hashTokenizedString(fp.WebGLRenderer, 8, 16, vector)

	// Audio fingerprint -> dims 24-31 (range: 0-100)
	hashNumerical(fp.AudioFingerprint, 0.0, 100.0, 24, 8, vector)

	// Screen dimensions -> dims 32-39
	hashNumerical(float64(fp.ScreenWidth), 320, 3840, 32, 3, vector)
	hashNumerical(float64(fp.ScreenHeight), 240, 2160, 35, 3, vector)
	hashNumerical(float64(fp.ScreenDepth), 8, 48, 38, 2, vector)

	// Timezone offset -> dims 40-47 (range: -720 to +840 minutes)
	hashNumerical(float64(fp.TimezoneOffset), -720, 840, 40, 8, vector)

	// Languages -> dims 48-63
	hashList(fp.Languages, 48, 16, vector)

	// Installed fonts count -> dims 64-71 (range: 0-500)
	hashNumerical(float64(fp.InstalledFontsCount), 0, 500, 64, 8, vector)

	// Plugins -> dims 72-87
	hashList(fp.Plugins, 72, 16, vector)

	// Hardware concurrency -> dims 88-95 (range: 1-128)
	hashNumerical(float64(fp.HardwareConcurrency), 1, 128, 88, 8, vector)

	// Device memory -> dims 96-103 (range: 0.5-64 GB)
	hashNumerical(fp.DeviceMemory, 0.5, 64.0, 96, 8, vector)

	// Platform -> dims 104-119
	hashCategorical(fp.Platform, 104, 16, vector)

	// L2 normalize
	l2Normalize(vector)

	return vector
}

// hashCategorical hashes a categorical string value into nDims dimensions starting at startDim.
func hashCategorical(value string, startDim, nDims int, vector []float32) {
	if value == "" {
		return
	}
	for i := 0; i < nDims; i++ {
		key := value + "_" + string(rune('0'+i))
		h := murmur3.Sum32WithSeed([]byte(key), uint32(42+i))
		// Use low bit for sign, rest for magnitude
		sign := float32(1.0)
		if h&1 == 1 {
			sign = -1.0
		}
		magnitude := float32((h>>1)%1000) / 1000.0
		vector[startDim+i] += sign * magnitude
	}
}

// hashNumerical encodes a numerical value using thermometer encoding across nDims.
func hashNumerical(value, minVal, maxVal float64, startDim, nDims int, vector []float32) {
	var normalized float64
	if maxVal == minVal {
		normalized = 0.5
	} else {
		normalized = (value - minVal) / (maxVal - minVal)
		if normalized < 0 {
			normalized = 0
		}
		if normalized > 1 {
			normalized = 1
		}
	}

	filled := int(normalized * float64(nDims))
	for i := 0; i < filled && i < nDims; i++ {
		vector[startDim+i] = 1.0
	}
	if filled < nDims {
		remainder := normalized*float64(nDims) - float64(filled)
		vector[startDim+filled] = float32(remainder)
	}
}

var tokenSplitter = regexp.MustCompile(`[^a-zA-Z0-9]+`)

// hashTokenizedString tokenizes a string and hashes each token into nDims dimensions.
func hashTokenizedString(value string, startDim, nDims int, vector []float32) {
	if value == "" {
		return
	}
	tokens := tokenSplitter.Split(strings.ToLower(value), -1)
	var filtered []string
	for _, t := range tokens {
		if t != "" {
			filtered = append(filtered, t)
		}
	}
	hashList(filtered, startDim, nDims, vector)
}

// hashList hashes a list of strings into nDims dimensions using additive hashing.
func hashList(items []string, startDim, nDims int, vector []float32) {
	if len(items) == 0 {
		return
	}
	for _, item := range items {
		bucket := int(murmur3.Sum32WithSeed([]byte(item), 42) % uint32(nDims))
		signHash := murmur3.Sum32WithSeed([]byte(item), 142)
		sign := float32(1.0)
		if signHash&1 == 1 {
			sign = -1.0
		}
		vector[startDim+bucket] += sign
	}

	// Normalize the sub-vector
	var norm float64
	for i := 0; i < nDims; i++ {
		norm += float64(vector[startDim+i]) * float64(vector[startDim+i])
	}
	norm = math.Sqrt(norm)
	if norm > 0 {
		for i := 0; i < nDims; i++ {
			vector[startDim+i] = float32(float64(vector[startDim+i]) / norm)
		}
	}
}

// l2Normalize normalizes the full vector to unit length.
func l2Normalize(vector []float32) {
	var norm float64
	for _, v := range vector {
		norm += float64(v) * float64(v)
	}
	norm = math.Sqrt(norm)
	if norm > 0 {
		for i := range vector {
			vector[i] = float32(float64(vector[i]) / norm)
		}
	}
}
