// Package radius implements just enough of RFC 2865/3579 to smoke-test the
// lab's RADIUS listener: build a PAP Access-Request (with the
// Message-Authenticator the server requires) and classify the reply.
package radius

import (
	"crypto/hmac"
	"crypto/md5"
	"fmt"
)

// Packet codes (RFC 2865 §3).
const (
	CodeAccessRequest = 1
	CodeAccessAccept  = 2
	CodeAccessReject  = 3
)

// Attribute types.
const (
	attrUserName             = 1
	attrUserPassword         = 2
	attrNASIdentifier        = 32
	attrMessageAuthenticator = 80
)

// BuildAccessRequest encodes a PAP Access-Request. id and authenticator are
// caller-supplied so encoding is deterministic and testable; callers should
// randomize both per request.
func BuildAccessRequest(user, password, secret string, id byte, authenticator [16]byte) ([]byte, error) {
	if len(user) == 0 || len(user) > 253 {
		return nil, fmt.Errorf("user name length %d out of range", len(user))
	}
	if len(password) == 0 || len(password) > 128 {
		return nil, fmt.Errorf("password length %d out of range", len(password))
	}

	attrs := attr(attrUserName, []byte(user))
	attrs = append(attrs, attr(attrUserPassword, encryptPassword(password, secret, authenticator))...)
	attrs = append(attrs, attr(attrNASIdentifier, []byte("mcplab-smoke"))...)
	// Message-Authenticator is computed over the packet with its own value
	// zeroed (RFC 3579 §3.2), so append the placeholder last.
	maOffset := 20 + len(attrs) + 2
	attrs = append(attrs, attr(attrMessageAuthenticator, make([]byte, 16))...)

	length := 20 + len(attrs)
	pkt := make([]byte, 0, length)
	pkt = append(pkt, CodeAccessRequest, id, byte(length>>8), byte(length))
	pkt = append(pkt, authenticator[:]...)
	pkt = append(pkt, attrs...)

	mac := hmac.New(md5.New, []byte(secret))
	mac.Write(pkt)
	copy(pkt[maOffset:maOffset+16], mac.Sum(nil))
	return pkt, nil
}

// VerifyResponse checks the Response Authenticator (RFC 2865 §3) of a reply
// to the request packet and returns the response code.
func VerifyResponse(resp, req []byte, secret string) (byte, error) {
	if len(resp) < 20 {
		return 0, fmt.Errorf("response too short (%d bytes)", len(resp))
	}
	length := int(resp[2])<<8 | int(resp[3])
	if length < 20 || length > len(resp) {
		return 0, fmt.Errorf("bad response length %d", length)
	}
	resp = resp[:length]
	if resp[1] != req[1] {
		return 0, fmt.Errorf("response id %d does not match request id %d", resp[1], req[1])
	}
	// ResponseAuth = MD5(Code+ID+Length+RequestAuth+Attributes+Secret)
	h := md5.New()
	h.Write(resp[:4])
	h.Write(req[4:20])
	h.Write(resp[20:])
	h.Write([]byte(secret))
	if !hmac.Equal(h.Sum(nil), resp[4:20]) {
		return 0, fmt.Errorf("response authenticator mismatch")
	}
	return resp[0], nil
}

func attr(typ byte, value []byte) []byte {
	out := make([]byte, 0, 2+len(value))
	return append(append(out, typ, byte(2+len(value))), value...)
}

// encryptPassword implements the RFC 2865 §5.2 User-Password obfuscation:
// the password is padded to a 16-octet multiple and XORed chunkwise with
// MD5(secret + previous-chunk-or-authenticator).
func encryptPassword(password, secret string, authenticator [16]byte) []byte {
	padded := make([]byte, (len(password)+15)/16*16)
	copy(padded, password)

	out := make([]byte, len(padded))
	prev := authenticator[:]
	for i := 0; i < len(padded); i += 16 {
		h := md5.New()
		h.Write([]byte(secret))
		h.Write(prev)
		digest := h.Sum(nil)
		for j := 0; j < 16; j++ {
			out[i+j] = padded[i+j] ^ digest[j]
		}
		prev = out[i : i+16]
	}
	return out
}
