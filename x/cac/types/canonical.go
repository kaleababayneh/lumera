//go:build cosmos

package types

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/gowebpki/jcs"
	"lukechampine.com/blake3"
)

var canonicalBufPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

const canonicalBufMaxRetain = 64 * 1024

// ComputeCanonicalRequestHash mirrors the router's JCS request-hash contract
// for {args, request_id, tool_id} and returns "blake3:<hex>".
func ComputeCanonicalRequestHash(toolID, requestID string, args map[string]any) string {
	argsCanonical, err := CanonicalizeRequestArgs(args)
	if err != nil {
		payload := map[string]any{
			"tool_id":    toolID,
			"request_id": requestID,
			"args":       args,
		}
		encoded, _ := json.Marshal(payload)
		sum := blake3.Sum256(encoded)
		return fmt.Sprintf("blake3:%x", sum[:])
	}

	buf := canonicalBufPool.Get().(*bytes.Buffer)
	buf.Reset()
	buf.WriteString(`{"args":`)
	buf.Write(argsCanonical)
	buf.WriteString(`,"request_id":`)
	appendCanonicalJSONString(buf, requestID)
	buf.WriteString(`,"tool_id":`)
	appendCanonicalJSONString(buf, toolID)
	buf.WriteByte('}')

	sum := blake3.Sum256(buf.Bytes())
	if buf.Cap() <= canonicalBufMaxRetain {
		canonicalBufPool.Put(buf)
	}
	return fmt.Sprintf("blake3:%x", sum[:])
}

// CanonicalizeRequestArgs returns JCS canonical JSON for the args payload.
func CanonicalizeRequestArgs(args map[string]any) ([]byte, error) {
	if args == nil {
		return []byte("null"), nil
	}
	if len(args) == 0 {
		return []byte("{}"), nil
	}
	return canonicalizeJSON(args)
}

func canonicalizeJSON(value any) ([]byte, error) {
	buf := canonicalBufPool.Get().(*bytes.Buffer)
	buf.Reset()
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		canonicalBufPool.Put(buf)
		return nil, err
	}
	raw := buf.Bytes()
	if n := len(raw); n > 0 && raw[n-1] == '\n' {
		raw = raw[:n-1]
	}
	out, err := jcs.Transform(raw)
	if buf.Cap() <= canonicalBufMaxRetain {
		canonicalBufPool.Put(buf)
	}
	return out, err
}

func appendCanonicalJSONString(buf *bytes.Buffer, s string) {
	if isSafeJSONASCII(s) {
		buf.WriteByte('"')
		buf.WriteString(s)
		buf.WriteByte('"')
		return
	}

	var tmp bytes.Buffer
	enc := json.NewEncoder(&tmp)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		buf.WriteByte('"')
		buf.WriteByte('"')
		return
	}
	raw := tmp.Bytes()
	if n := len(raw); n > 0 && raw[n-1] == '\n' {
		raw = raw[:n-1]
	}
	canonical, err := jcs.Transform(raw)
	if err != nil {
		buf.Write(raw)
		return
	}
	buf.Write(canonical)
}

func isSafeJSONASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c > 0x7e || c == '"' || c == '\\' {
			return false
		}
	}
	return true
}

// ComputeCanonicalCacheKey mirrors the router's do-cache hash contract for
// {args, policy_version, session_id, tool_id, tool_version}.
func ComputeCanonicalCacheKey(sessionID, policyVersion, toolID, toolVersion string, args map[string]any) string {
	trimmedToolID := strings.TrimSpace(toolID)
	trimmedToolVersion := strings.TrimSpace(toolVersion)

	argsCanonical, err := CanonicalizeRequestArgs(args)
	if err != nil {
		payload := map[string]any{
			"session_id":     sessionID,
			"policy_version": policyVersion,
			"tool_id":        trimmedToolID,
			"tool_version":   trimmedToolVersion,
			"args":           args,
		}
		encoded, _ := json.Marshal(payload)
		sum := blake3.Sum256(encoded)
		trimmedSession := strings.TrimSpace(sessionID)
		if trimmedSession == "" {
			return fmt.Sprintf("do_cache::blake3:%x", sum[:])
		}
		return fmt.Sprintf("do_cache::%s::blake3:%x", trimmedSession, sum[:])
	}

	buf := canonicalBufPool.Get().(*bytes.Buffer)
	buf.Reset()
	buf.WriteString(`{"args":`)
	buf.Write(argsCanonical)
	buf.WriteString(`,"policy_version":`)
	appendCanonicalJSONString(buf, policyVersion)
	buf.WriteString(`,"session_id":`)
	appendCanonicalJSONString(buf, sessionID)
	buf.WriteString(`,"tool_id":`)
	appendCanonicalJSONString(buf, trimmedToolID)
	buf.WriteString(`,"tool_version":`)
	appendCanonicalJSONString(buf, trimmedToolVersion)
	buf.WriteByte('}')

	sum := blake3.Sum256(buf.Bytes())
	if buf.Cap() <= canonicalBufMaxRetain {
		canonicalBufPool.Put(buf)
	}

	trimmedSession := strings.TrimSpace(sessionID)
	if trimmedSession == "" {
		return fmt.Sprintf("do_cache::blake3:%x", sum[:])
	}
	return fmt.Sprintf("do_cache::%s::blake3:%x", trimmedSession, sum[:])
}
