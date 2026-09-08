package rdp

import (
	"encoding/binary"
	"net/url"
	"strings"
	"unicode/utf16"

	"github.com/Shermanxwz/RDP-web/internal/store"
)

// URI generates the documented legacy rdp:// URI used by Microsoft's
// Android, iOS and macOS Remote Desktop clients. Passwords are intentionally
// unsupported and never appear in the URI.
func URI(d store.Device) string {
	pairs := [][2]string{{"full address", "s:" + store.Address(d)}}
	if d.Username != "" { pairs = append(pairs, [2]string{"username", "s:" + d.Username}) }
	if d.Domain != "" { pairs = append(pairs, [2]string{"domain", "s:" + d.Domain}) }
	if d.Gateway != "" {
		pairs = append(pairs, [2]string{"gatewayhostname", "s:" + d.Gateway}, [2]string{"gatewayusagemethod", "i:1"})
	}
	pairs = append(pairs, [2]string{"audiomode", "i:" + string(rune('0'+d.AudioMode))})
	parts := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		parts = append(parts, escape(pair[0])+"="+escape(pair[1]))
	}
	return "rdp://" + strings.Join(parts, "&")
}

func escape(value string) string {
	encoded := url.QueryEscape(value)
	encoded = strings.ReplaceAll(encoded, "+", "%20")
	encoded = strings.ReplaceAll(encoded, "%3A", ":")
	return encoded
}

// File returns a conservative .rdp profile. It avoids drive/printer/device
// redirection by default and does not contain a password.
func File(d store.Device) []byte {
	boolInt := func(v bool) int { if v { return 1 }; return 0 }
	lines := []string{
		"full address:s:" + store.Address(d),
		"screen mode id:i:2",
		"session bpp:i:32",
		"compression:i:1",
		"networkautodetect:i:1",
		"bandwidthautodetect:i:1",
		"authentication level:i:2",
		"enablecredsspsupport:i:1",
		"prompt for credentials:i:1",
		"redirectclipboard:i:" + itoa(boolInt(d.RedirectClipboard)),
		"redirectprinters:i:0",
		"redirectcomports:i:0",
		"redirectsmartcards:i:0",
		"drivestoredirect:s:",
		"audiomode:i:" + itoa(d.AudioMode),
		"use multimon:i:" + itoa(boolInt(d.UseMultimon)),
	}
	if d.Username != "" { lines = append(lines, "username:s:"+d.Username) }
	if d.Domain != "" { lines = append(lines, "domain:s:"+d.Domain) }
	if d.Gateway != "" {
		lines = append(lines, "gatewayhostname:s:"+d.Gateway, "gatewayusagemethod:i:1", "promptcredentialonce:i:1")
	}
	return utf16LE(strings.Join(lines, "\r\n") + "\r\n")
}

func utf16LE(s string) []byte {
	runes := utf16.Encode([]rune(s))
	out := make([]byte, 2+len(runes)*2)
	out[0], out[1] = 0xFF, 0xFE
	for i, r := range runes { binary.LittleEndian.PutUint16(out[2+i*2:], r) }
	return out
}

func itoa(v int) string {
	if v == 0 { return "0" }
	if v == 1 { return "1" }
	if v == 2 { return "2" }
	return "0"
}
