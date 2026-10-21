package recorder

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/bluenviron/gortsplib/v5/pkg/description"
	rtspformat "github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/stretchr/testify/require"

	"github.com/bluenviron/mediamtx/internal/conf"
	"github.com/bluenviron/mediamtx/internal/stream"
	"github.com/bluenviron/mediamtx/internal/test"
	"github.com/bluenviron/mediamtx/internal/unit"
)

func TestRecorderFMP4TimestampSEISegmentNTP(t *testing.T) {
	for _, ca := range []string{"on", "off"} {
		t.Run(ca, func(t *testing.T) {
			forma := &rtspformat.H264{
				PayloadTyp:        96,
				PacketizationMode: 1,
				SPS: []byte{
					0x67, 0x64, 0x00, 0x28, 0xac, 0xd9, 0x40, 0x78,
					0x02, 0x27, 0xe5, 0x84, 0x00, 0x00, 0x03, 0x00,
					0x04, 0x00, 0x00, 0x03, 0x00, 0xf0, 0x3c, 0x60,
					0xc6, 0x58,
				},
				PPS: []byte{0x68, 0xeb, 0xe3, 0xcb, 0x22, 0xc0},
			}

			desc := &description.Session{Medias: []*description.Media{{
				Type:    description.MediaTypeVideo,
				Formats: []rtspformat.Format{forma},
			}}}

			strm := &stream.Stream{
				OrigDesc:          desc,
				WriteQueueSize:    512,
				RTPMaxPayloadSize: 1450,
				OwlTimestampSEI:   ca == "on",
				Parent:            test.NilLogger,
			}
			err := strm.Initialize()
			require.NoError(t, err)
			defer strm.Close()

			subStream := &stream.SubStream{Stream: strm}
			err = subStream.Initialize()
			require.NoError(t, err)

			dir := t.TempDir()
			var segments []string

			w := &Recorder{
				PathFormat:      filepath.Join(dir, "%path/%Y-%m-%d_%H-%M-%S-%f"),
				Format:          conf.RecordFormatFMP4,
				PartDuration:    100 * time.Millisecond,
				MaxPartSize:     50 * 1024 * 1024,
				SegmentDuration: 100 * time.Millisecond,
				PathName:        "mypath",
				Stream:          strm,
				Parent:          test.NilLogger,
				OnSegmentCreate: func(segPath string) {
					segments = append(segments, filepath.Base(segPath))
				},
			}
			w.Initialize()

			ntp := time.Date(2008, 5, 20, 22, 15, 25, 0, time.UTC)

			// B-frames: the second IDR has DTS 18067 and PTS 24000
			for _, s := range []struct {
				nalu []byte
				pts  int64
			}{
				{[]byte{0x65, 0x88, 0x84, 0x00, 0x33, 0xff}, 0},
				{[]byte{0x41, 0x9a, 0x21, 0x6c, 0x45, 0xff}, 3000},
				{[]byte{0x41, 0x9a, 0x42, 0x3c, 0x21, 0x93}, 6000},
				{[]byte{0x41, 0x9a, 0x63, 0x49, 0xe1, 0x0f}, 9000},
				{[]byte{0x41, 0x9a, 0x86, 0x49, 0xe1, 0x0f}, 18000},
				{[]byte{0x41, 0x9e, 0xa5, 0x42, 0x7f, 0xf9}, 15000},
				{[]byte{0x01, 0x9e, 0xc4, 0x69, 0x13, 0xff}, 12000},
				{[]byte{0x41, 0x9a, 0xc8, 0x4b, 0xa8, 0x42}, 24000},
				{[]byte{0x65, 0x88, 0x84, 0x00, 0x33, 0xff}, 24000},
				{[]byte{0x41, 0x9a, 0x21, 0x6c, 0x45, 0xff}, 27000},
				{[]byte{0x41, 0x9a, 0x42, 0x3c, 0x21, 0x93}, 30000},
			} {
				subStream.WriteUnit(desc.Medias[0], forma, &unit.Unit{
					PTS:     56890 + s.pts,
					NTP:     ntp.Add(time.Duration(s.pts) * time.Second / 90000),
					Payload: unit.PayloadH264{s.nalu},
				})
			}

			time.Sleep(100 * time.Millisecond)
			w.Close()

			require.GreaterOrEqual(t, len(segments), 2)
			require.Equal(t, "2008-05-20_22-15-25-000000.mp4", segments[0])

			if ca == "on" {
				require.Equal(t, "2008-05-20_22-15-25-200744.mp4", segments[1]) // DTS
			} else {
				require.Equal(t, "2008-05-20_22-15-25-266666.mp4", segments[1]) // PTS
			}
		})
	}
}
