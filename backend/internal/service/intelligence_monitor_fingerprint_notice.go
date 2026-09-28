package service

import _ "embed"

// Keep the upstream notice in the standalone executable, including binaries
// delivered through application updates without the container filesystem.
//
//go:embed data/intelligence_fingerprint/LICENSE
var intelligenceFingerprintLicense string

//go:embed data/intelligence_fingerprint/README.md
var intelligenceFingerprintProvenance string

func IntelligenceFingerprintThirdPartyNotices() string {
	return intelligenceFingerprintProvenance + "\n" + intelligenceFingerprintLicense
}
