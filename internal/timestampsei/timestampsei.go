// Package timestampsei contains a SEI NAL unit that carries the ingest time of an access unit.
//
// The SEI message is of type 5 (user data unregistered) and its payload is:
//   - UUID (16 bytes)
//   - version (1 byte, 1)
//   - NTP, as Unix nanoseconds (int64, big endian)
//   - PTS, in 90kHz units (int64, big endian)
package timestampsei

import (
	"encoding/binary"
	"time"

	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h265"
)

// UUID identifies the SEI message.
var UUID = [16]byte{
	0x8c, 0x52, 0x3f, 0xd1, 0x3e, 0x7f, 0x4b, 0x4e,
	0xbf, 0x20, 0xc5, 0xc7, 0x22, 0x4a, 0x1a, 0xcf,
}

// Version is the version of the payload.
const Version = 1

const (
	payloadTypeUserDataUnregistered = 5
	payloadSize                     = 16 + 1 + 8 + 8
	h265NALUTypePrefixSEI           = 39
)

// Codec is a video codec.
type Codec int

// codecs.
const (
	CodecH264 Codec = iota
	CodecH265
)

func emulationPreventionInsert(rbsp []byte) []byte {
	out := make([]byte, 0, len(rbsp)+len(rbsp)/2)
	zeros := 0

	for _, b := range rbsp {
		if zeros == 2 && b <= 3 {
			out = append(out, 3)
			zeros = 0
		}

		out = append(out, b)

		if b == 0 {
			zeros++
		} else {
			zeros = 0
		}
	}

	return out
}

// Build builds a SEI NAL unit.
// temporalID is the TemporalId of the access unit and is used by H265 only.
func Build(ntp time.Time, pts int64, codec Codec, temporalID uint8) []byte {
	rbsp := make([]byte, 0, 2+payloadSize+1)
	rbsp = append(rbsp, payloadTypeUserDataUnregistered, payloadSize)
	rbsp = append(rbsp, UUID[:]...)
	rbsp = append(rbsp, Version)
	rbsp = binary.BigEndian.AppendUint64(rbsp, uint64(ntp.UnixNano()))
	rbsp = binary.BigEndian.AppendUint64(rbsp, uint64(pts))
	rbsp = append(rbsp, 0x80)

	var header []byte
	if codec == CodecH265 {
		header = []byte{h265NALUTypePrefixSEI << 1, temporalID + 1}
	} else {
		header = []byte{byte(h264.NALUTypeSEI)}
	}

	return append(header, emulationPreventionInsert(rbsp)...)
}

func isVCLH264(nalu []byte) bool {
	typ := h264.NALUType(nalu[0] & 0x1F)
	return typ >= h264.NALUTypeNonIDR && typ <= h264.NALUTypeIDR
}

func isVCLH265(nalu []byte) bool {
	typ := h265.NALUType((nalu[0] >> 1) & 0b111111)
	return typ <= 31
}

func insert(au [][]byte, pos int, nalu []byte) [][]byte {
	out := make([][]byte, 0, len(au)+1)
	out = append(out, au[:pos]...)
	out = append(out, nalu)
	return append(out, au[pos:]...)
}

// InsertH264 returns a copy of an H264 access unit with the SEI before its first VCL NAL unit.
// The access unit is returned unchanged when it has no VCL NAL unit.
func InsertH264(au [][]byte, ntp time.Time, pts int64) [][]byte {
	for i, nalu := range au {
		if isVCLH264(nalu) {
			return insert(au, i, Build(ntp, pts, CodecH264, 0))
		}
	}
	return au
}

// InsertH265 returns a copy of an H265 access unit with the SEI before its first VCL NAL unit.
// The access unit is returned unchanged when it has no VCL NAL unit.
func InsertH265(au [][]byte, ntp time.Time, pts int64) [][]byte {
	for i, nalu := range au {
		if len(nalu) >= 2 && isVCLH265(nalu) {
			return insert(au, i, Build(ntp, pts, CodecH265, (nalu[1]&0b111)-1))
		}
	}
	return au
}
