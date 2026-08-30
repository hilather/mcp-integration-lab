// Package ntpquery implements a minimal NTPv4 SNTP client for smoke:
// one client-mode datagram, one reply, served time from the transmit stamp.
package ntpquery

import (
	"encoding/binary"
	"fmt"
	"net"
	"time"
)

const ntpEpochOffset = 2208988800 // seconds between 1900-01-01 and 1970-01-01

// Query sends one NTPv4 client packet to addr (host:port) and returns the
// server transmit timestamp. No new NTP library.
func Query(addr string, timeout time.Duration) (time.Time, error) {
	conn, err := net.DialTimeout("udp", addr, timeout)
	if err != nil {
		return time.Time{}, err
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return time.Time{}, err
	}

	pkt := make([]byte, 48)
	pkt[0] = 0x23 // LI=0 VN=4 Mode=3 (client)
	now := time.Now().UTC()
	putNTPTime(pkt[40:], now)
	if _, err := conn.Write(pkt); err != nil {
		return time.Time{}, err
	}

	reply := make([]byte, 48)
	n, err := conn.Read(reply)
	if err != nil {
		return time.Time{}, err
	}
	if n < 48 {
		return time.Time{}, fmt.Errorf("short NTP reply: %d bytes", n)
	}
	vn := (reply[0] >> 3) & 0x07
	mode := reply[0] & 0x07
	if vn != 3 && vn != 4 {
		return time.Time{}, fmt.Errorf("unexpected NTP version %d", vn)
	}
	if mode != 4 {
		return time.Time{}, fmt.Errorf("unexpected NTP mode %d (want server=4)", mode)
	}
	return ntpTime(reply[40:48]), nil
}

func putNTPTime(b []byte, t time.Time) {
	sec := uint32(t.Unix() + ntpEpochOffset)
	frac := uint32((uint64(t.Nanosecond()) << 32) / 1e9)
	binary.BigEndian.PutUint32(b[0:4], sec)
	binary.BigEndian.PutUint32(b[4:8], frac)
}

func ntpTime(b []byte) time.Time {
	sec := binary.BigEndian.Uint32(b[0:4])
	frac := binary.BigEndian.Uint32(b[4:8])
	unix := int64(sec) - ntpEpochOffset
	nsec := int64((uint64(frac) * 1e9) >> 32)
	return time.Unix(unix, nsec).UTC()
}
