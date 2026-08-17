package radius

import (
	"bytes"
	"crypto/hmac"
	"crypto/md5"
	"testing"
)

var testAuth = [16]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}

// decryptPassword reverses the RFC 2865 §5.2 obfuscation.
func decryptPassword(enc []byte, secret string, authenticator [16]byte) string {
	out := make([]byte, len(enc))
	prev := authenticator[:]
	for i := 0; i < len(enc); i += 16 {
		h := md5.New()
		h.Write([]byte(secret))
		h.Write(prev)
		digest := h.Sum(nil)
		for j := 0; j < 16; j++ {
			out[i+j] = enc[i+j] ^ digest[j]
		}
		prev = enc[i : i+16]
	}
	return string(bytes.TrimRight(out, "\x00"))
}

func attrsOf(t *testing.T, pkt []byte) map[byte][]byte {
	t.Helper()
	out := map[byte][]byte{}
	for i := 20; i < len(pkt); {
		typ, l := pkt[i], int(pkt[i+1])
		if l < 2 || i+l > len(pkt) {
			t.Fatalf("bad attribute at offset %d", i)
		}
		out[typ] = pkt[i+2 : i+l]
		i += l
	}
	return out
}

func TestBuildAccessRequest(t *testing.T) {
	pkt, err := BuildAccessRequest("lab-admin", "s3cretpw-longer-than-16-bytes", "shared-secret", 42, testAuth)
	if err != nil {
		t.Fatal(err)
	}
	if pkt[0] != CodeAccessRequest || pkt[1] != 42 {
		t.Fatalf("header = %v", pkt[:2])
	}
	if got := int(pkt[2])<<8 | int(pkt[3]); got != len(pkt) {
		t.Fatalf("length field %d != packet length %d", got, len(pkt))
	}

	attrs := attrsOf(t, pkt)
	if string(attrs[1]) != "lab-admin" {
		t.Fatalf("User-Name = %q", attrs[1])
	}
	if got := decryptPassword(attrs[2], "shared-secret", testAuth); got != "s3cretpw-longer-than-16-bytes" {
		t.Fatalf("password roundtrip = %q", got)
	}
	if len(attrs[2])%16 != 0 {
		t.Fatalf("User-Password length %d not a 16 multiple", len(attrs[2]))
	}

	// Message-Authenticator: HMAC-MD5 over the packet with the MA zeroed.
	zeroed := append([]byte(nil), pkt...)
	for i := 20; i < len(zeroed); {
		typ, l := zeroed[i], int(zeroed[i+1])
		if typ == 80 {
			copy(zeroed[i+2:i+l], make([]byte, 16))
		}
		i += l
	}
	mac := hmac.New(md5.New, []byte("shared-secret"))
	mac.Write(zeroed)
	if !hmac.Equal(mac.Sum(nil), attrs[80]) {
		t.Fatal("Message-Authenticator mismatch")
	}
}

func TestVerifyResponse(t *testing.T) {
	req, err := BuildAccessRequest("u", "p", "sec", 7, testAuth)
	if err != nil {
		t.Fatal(err)
	}

	// Forge a valid Access-Accept the way a server would.
	resp := []byte{CodeAccessAccept, 7, 0, 20}
	h := md5.New()
	h.Write(resp[:4])
	h.Write(req[4:20])
	h.Write([]byte("sec"))
	resp = append(resp, h.Sum(nil)...)

	code, err := VerifyResponse(resp, req, "sec")
	if err != nil || code != CodeAccessAccept {
		t.Fatalf("code=%d err=%v", code, err)
	}

	if _, err := VerifyResponse(resp, req, "wrong"); err == nil {
		t.Fatal("expected authenticator mismatch with wrong secret")
	}
	bad := append([]byte(nil), resp...)
	bad[1] = 9
	if _, err := VerifyResponse(bad, req, "sec"); err == nil {
		t.Fatal("expected id mismatch")
	}
}
