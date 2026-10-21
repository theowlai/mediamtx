package timestampsei

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"

	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
	"github.com/stretchr/testify/require"
)

type parsed struct {
	version uint8
	ntp     time.Time
	pts     int64
}

func parse(t *testing.T, nalu []byte, headerLen int) parsed {
	rbsp := h264.EmulationPreventionRemove(nalu[headerLen:])

	require.Equal(t, byte(5), rbsp[0])
	require.Equal(t, byte(33), rbsp[1])
	require.Equal(t, UUID[:], rbsp[2:18])
	require.Equal(t, []byte{0x80}, rbsp[2+33:])

	return parsed{
		version: rbsp[18],
		ntp:     time.Unix(0, int64(binary.BigEndian.Uint64(rbsp[19:27]))),
		pts:     int64(binary.BigEndian.Uint64(rbsp[27:35])),
	}
}

func hasStartCodeEmulation(b []byte) bool {
	for i := 0; i+2 < len(b); i++ {
		if b[i] == 0 && b[i+1] == 0 && b[i+2] <= 2 {
			return true
		}
	}
	return false
}

func TestBuildRoundTrip(t *testing.T) {
	for _, ca := range []struct {
		name string
		ntp  time.Time
		pts  int64
	}{
		{"regular", time.Date(2026, 10, 21, 12, 30, 15, 123456789, time.UTC), 8589934592 + 12345},
		{"zero pts", time.Date(2026, 10, 21, 12, 30, 15, 0, time.UTC), 0},
		{"zero heavy", time.Unix(0, 1<<32), 1 << 16},
		{"zero ntp", time.Unix(0, 0), 3},
		{"negative pts", time.Date(2026, 10, 21, 12, 30, 15, 0, time.UTC), -90000},
	} {
		t.Run(ca.name, func(t *testing.T) {
			nalu := Build(ca.ntp, ca.pts, CodecH264, 0)
			require.Equal(t, byte(0x06), nalu[0])
			require.False(t, hasStartCodeEmulation(nalu))

			p := parse(t, nalu, 1)
			require.Equal(t, uint8(Version), p.version)
			require.Equal(t, ca.ntp.UnixNano(), p.ntp.UnixNano())
			require.Equal(t, ca.pts, p.pts)
		})
	}
}

func TestBuildEmulationPrevention(t *testing.T) {
	nalu := Build(time.Unix(0, 0), 0, CodecH264, 0)
	require.False(t, hasStartCodeEmulation(nalu))
	require.True(t, bytes.Contains(nalu, []byte{0, 0, 3, 0, 0, 3}))
	require.Greater(t, len(nalu), 1+2+33+1)

	p := parse(t, nalu, 1)
	require.Equal(t, int64(0), p.ntp.UnixNano())
	require.Equal(t, int64(0), p.pts)
}

func TestEmulationPreventionInsert(t *testing.T) {
	for _, ca := range []struct {
		in  []byte
		out []byte
	}{
		{[]byte{0, 0, 0}, []byte{0, 0, 3, 0}},
		{[]byte{0, 0, 1}, []byte{0, 0, 3, 1}},
		{[]byte{0, 0, 3}, []byte{0, 0, 3, 3}},
		{[]byte{0, 0, 4}, []byte{0, 0, 4}},
		{[]byte{0, 0, 0, 0, 0}, []byte{0, 0, 3, 0, 0, 3, 0}},
		{[]byte{1, 0, 0, 2, 0}, []byte{1, 0, 0, 3, 2, 0}},
	} {
		require.Equal(t, ca.out, emulationPreventionInsert(ca.in))
		require.Equal(t, ca.in, h264.EmulationPreventionRemove(ca.out))
	}
}

func TestBuildH265(t *testing.T) {
	ntp := time.Date(2026, 10, 21, 12, 30, 15, 0, time.UTC)

	nalu := Build(ntp, 90000, CodecH265, 0)
	require.Equal(t, []byte{0x4E, 0x01}, nalu[:2])
	p := parse(t, nalu, 2)
	require.Equal(t, ntp.UnixNano(), p.ntp.UnixNano())
	require.Equal(t, int64(90000), p.pts)

	nalu = Build(ntp, 90000, CodecH265, 2)
	require.Equal(t, []byte{0x4E, 0x03}, nalu[:2])
}

func TestInsertH264(t *testing.T) {
	ntp := time.Date(2026, 10, 21, 12, 30, 15, 0, time.UTC)
	aud := []byte{9, 0xf0}
	sps := []byte{7, 1, 2}
	pps := []byte{8, 3}
	sei := []byte{6, 5, 1, 0x80}
	idr := []byte{5, 4}
	nonIDR := []byte{1, 5}

	au := [][]byte{aud, sps, pps, sei, idr, nonIDR}
	out := InsertH264(au, ntp, 1234)
	require.Len(t, out, 7)
	require.Equal(t, [][]byte{aud, sps, pps, sei}, out[:4])
	require.Equal(t, Build(ntp, 1234, CodecH264, 0), out[4])
	require.Equal(t, [][]byte{idr, nonIDR}, out[5:])
	require.Equal(t, [][]byte{aud, sps, pps, sei, idr, nonIDR}, au)

	out = InsertH264([][]byte{nonIDR}, ntp, 1234)
	require.Equal(t, [][]byte{Build(ntp, 1234, CodecH264, 0), nonIDR}, out)

	out = InsertH264([][]byte{sps, pps}, ntp, 1234)
	require.Equal(t, [][]byte{sps, pps}, out)
}

func TestInsertH265(t *testing.T) {
	ntp := time.Date(2026, 10, 21, 12, 30, 15, 0, time.UTC)
	aud := []byte{35 << 1, 1, 0x50}
	vps := []byte{32 << 1, 1, 1}
	sps := []byte{33 << 1, 1, 2}
	pps := []byte{34 << 1, 1, 3}
	sei := []byte{39 << 1, 1, 5, 1, 0x80}
	trail := []byte{1 << 1, 3, 4}
	idr := []byte{19 << 1, 1, 4}

	au := [][]byte{aud, vps, sps, pps, sei, idr}
	out := InsertH265(au, ntp, 1234)
	require.Equal(t, [][]byte{aud, vps, sps, pps, sei, Build(ntp, 1234, CodecH265, 0), idr}, out)

	out = InsertH265([][]byte{trail}, ntp, 1234)
	require.Equal(t, [][]byte{Build(ntp, 1234, CodecH265, 2), trail}, out)
	require.Equal(t, byte(3), out[0][1])

	out = InsertH265([][]byte{vps, sps, pps}, ntp, 1234)
	require.Equal(t, [][]byte{vps, sps, pps}, out)
}
