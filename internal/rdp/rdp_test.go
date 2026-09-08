package rdp

import (
	"encoding/binary"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/Shermanxwz/RDP-web/internal/store"
)

func TestURI(t *testing.T) {
	d := store.Device{Host:"example.com",Port:3390,Username:"alice",Domain:"ACME",Gateway:"gw.example.com",AudioMode:2}
	got := URI(d)
	for _, want := range []string{"rdp://full%20address=s:example.com:3390","username=s:alice","domain=s:ACME","gatewayhostname=s:gw.example.com","gatewayusagemethod=i:1","audiomode=i:2"} {
		if !strings.Contains(got, want) { t.Fatalf("URI %q missing %q", got, want) }
	}
	if strings.Contains(strings.ToLower(got), "password") { t.Fatal("URI must never contain a password") }
}

func TestFileIsUTF16AndSafeDefaults(t *testing.T) {
	d := store.Device{Host:"2001:db8::1",Port:3389,Username:"管理员",RedirectClipboard:true,UseMultimon:true}
	data := File(d)
	if len(data)<2 || data[0]!=0xff || data[1]!=0xfe { t.Fatal("missing UTF-16LE BOM") }
	units:=make([]uint16,(len(data)-2)/2);for i:=range units{units[i]=binary.LittleEndian.Uint16(data[2+i*2:])};text:=string(utf16.Decode(units))
	for _, want := range []string{"full address:s:[2001:db8::1]:3389","username:s:管理员","redirectclipboard:i:1","redirectprinters:i:0","drivestoredirect:s:","use multimon:i:1"} { if !strings.Contains(text,want){t.Fatalf("file missing %q: %q",want,text)} }
	if strings.Contains(strings.ToLower(text),"password") { t.Fatal("file must never contain a password") }
}
