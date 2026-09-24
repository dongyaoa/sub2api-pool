package service

import (
	"math"

	"github.com/tidwall/gjson"
)

// parseUpstreamMonitorUsage normalizes provider token semantics: OpenAI and
// Gemini input counts include cached tokens; Anthropic input counts exclude
// cache creation and cache reads. Missing counters remain unknown, never free.
func parseUpstreamMonitorUsage(provider, raw string) *UsageTokens {
	var inPath, outPath, cachePath string
	switch provider {
	case MonitorProviderAnthropic:
		inPath = "usage.input_tokens"
		outPath = "usage.output_tokens"
		cachePath = "usage.cache_read_input_tokens"
	case MonitorProviderGemini:
		inPath = "usageMetadata.promptTokenCount"
		outPath = "usageMetadata.candidatesTokenCount"
		cachePath = "usageMetadata.cachedContentTokenCount"
	default:
		inPath = "usage.prompt_tokens"
		outPath = "usage.completion_tokens"
		cachePath = "usage.prompt_tokens_details.cached_tokens"
		if gjson.Get(raw, "usage.input_tokens").Exists() {
			inPath = "usage.input_tokens"
			outPath = "usage.output_tokens"
			cachePath = "usage.input_tokens_details.cached_tokens"
		}
	}
	input, inOK := upstreamTokenCounter(raw, inPath, true)
	output, outOK := upstreamTokenCounter(raw, outPath, true)
	cached, cacheOK := upstreamTokenCounter(raw, cachePath, false)
	if !inOK || !outOK || !cacheOK {
		return nil
	}
	tokens := &UsageTokens{InputTokens: input, OutputTokens: output, CacheReadTokens: cached}
	if provider == MonitorProviderAnthropic {
		creation, ok := upstreamTokenCounter(raw, "usage.cache_creation_input_tokens", false)
		if !ok {
			return nil
		}
		tokens.CacheCreationTokens = creation
		short, ok := upstreamTokenCounter(raw, "usage.cache_creation.ephemeral_5m_input_tokens", false)
		if !ok {
			return nil
		}
		long, ok := upstreamTokenCounter(raw, "usage.cache_creation.ephemeral_1h_input_tokens", false)
		if !ok {
			return nil
		}
		if short+long > creation {
			return nil
		}
		tokens.CacheCreation5mTokens = short
		tokens.CacheCreation1hTokens = long
	} else {
		if cached > input {
			return nil
		}
		tokens.InputTokens -= cached
		if provider == MonitorProviderGemini {
			thinking, ok := upstreamTokenCounter(raw, "usageMetadata.thoughtsTokenCount", false)
			if !ok {
				return nil
			}
			tokens.OutputTokens += thinking
		}
	}
	return tokens
}

func upstreamTokenCounter(raw, path string, required bool) (int, bool) {
	value := gjson.Get(raw, path)
	if !value.Exists() {
		return 0, !required
	}
	if value.Type != gjson.Number || value.Num < 0 || value.Num > 1e10 || math.Trunc(value.Num) != value.Num {
		return 0, false
	}
	return int(value.Num), true
}
