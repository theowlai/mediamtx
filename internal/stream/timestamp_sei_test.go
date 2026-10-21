package stream

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
	"github.com/bluenviron/mediamtx/internal/timestampsei"
	"github.com/bluenviron/mediamtx/internal/unit"
	"github.com/pion/rtp"
	"github.com/stretchr/testify/require"
)

var (
	testSPS = []byte{
		0x67, 0x42, 0xc0, 0x28, 0xd9, 0x00, 0x78, 0x02,
		0x27, 0xe5, 0x84, 0x00, 0x00, 0x03, 0x00, 0x04,
		0x00, 0x00, 0x03, 0x00, 0xf0, 0x3c, 0x60, 0xc9, 0x20,
	}
	testPPS = []byte{0x08, 0x06, 0x07, 0x08}
)

func readUnit(t *testing.T, strm *Stream, medi *description.Media, forma format.Format) chan *unit.Unit {
	r := &Reader{}
	ch := make(chan *unit.Unit, 10)

	r.OnData(medi, forma, func(u *unit.Unit) error {
		ch <- u
		return nil
	})

	strm.AddReader(r)
	t.Cleanup(func() { strm.RemoveReader(r) })

	return ch
}

func TestTimestampSEIH264(t *testing.T) {
	desc := &description.Session{Medias: []*description.Media{{
		Type: description.MediaTypeVideo,
		Formats: []format.Format{&format.H264{
			PayloadTyp:        96,
			PacketizationMode: 1,
			SPS:               testSPS,
			PPS:               testPPS,
		}},
	}}}

	strm := &Stream{
		OrigDesc:          desc,
		WriteQueueSize:    512,
		RTPMaxPayloadSize: 1450,
		ReplaceNTP:        true,
		OwlTimestampSEI:   true,
		Parent:            nilLogger{},
	}
	err := strm.Initialize()
	require.NoError(t, err)
	defer strm.Close()

	subStream := &SubStream{Stream: strm}
	err = subStream.Initialize()
	require.NoError(t, err)

	ch := readUnit(t, strm, desc.Medias[0], desc.Medias[0].Formats[0])

	userSEI := []byte{6, 5, 1, 0x80}

	subStream.WriteUnit(desc.Medias[0], desc.Medias[0].Formats[0], &unit.Unit{
		PTS: 90000 * 3,
		Payload: unit.PayloadH264{
			{byte(h264.NALUTypeAccessUnitDelimiter), 0xf0},
			userSEI,
			{5, 2},
		},
	})

	u := <-ch
	au := u.Payload.(unit.PayloadH264)
	require.Len(t, au, 5)
	require.Equal(t, testSPS, au[0])
	require.Equal(t, testPPS, au[1])
	require.Equal(t, userSEI, au[2])
	require.Equal(t, timestampsei.Build(u.NTP, 90000*3, timestampsei.CodecH264, 0), au[3])
	require.Equal(t, []byte{5, 2}, au[4])

	rbsp := h264.EmulationPreventionRemove(au[3][1:])
	require.Equal(t, timestampsei.UUID[:], rbsp[2:18])
	require.Equal(t, u.NTP.UnixNano(), int64(binary.BigEndian.Uint64(rbsp[19:27])))

	// the stamp travels in RTP packets too
	var sz int
	for _, pkt := range u.RTPPackets {
		sz += len(pkt.Payload)
	}
	require.Greater(t, sz, len(au[3]))

	subStream.WriteUnit(desc.Medias[0], desc.Medias[0].Formats[0], &unit.Unit{
		PTS:     90000 * 4,
		Payload: unit.PayloadH264{testSPS, testPPS},
	})

	subStream.WriteUnit(desc.Medias[0], desc.Medias[0].Formats[0], &unit.Unit{
		PTS:     90000 * 5,
		Payload: unit.PayloadH264{{1, 2}},
	})

	u = <-ch
	require.True(t, u.NilPayload())

	u = <-ch
	au = u.Payload.(unit.PayloadH264)
	require.Equal(t, unit.PayloadH264{
		timestampsei.Build(u.NTP, 90000*5, timestampsei.CodecH264, 0),
		{1, 2},
	}, au)
}

func TestTimestampSEIOff(t *testing.T) {
	desc := &description.Session{Medias: []*description.Media{{
		Type:    description.MediaTypeVideo,
		Formats: []format.Format{&format.H264{PayloadTyp: 96, PacketizationMode: 1}},
	}}}

	strm := &Stream{
		OrigDesc:          desc,
		WriteQueueSize:    512,
		RTPMaxPayloadSize: 1450,
		ReplaceNTP:        true,
		Parent:            nilLogger{},
	}
	err := strm.Initialize()
	require.NoError(t, err)
	defer strm.Close()

	subStream := &SubStream{Stream: strm}
	err = subStream.Initialize()
	require.NoError(t, err)

	ch := readUnit(t, strm, desc.Medias[0], desc.Medias[0].Formats[0])

	subStream.WriteUnit(desc.Medias[0], desc.Medias[0].Formats[0], &unit.Unit{
		PTS:     90000,
		Payload: unit.PayloadH264{{1, 2}},
	})

	u := <-ch
	require.Equal(t, unit.PayloadH264{{1, 2}}, u.Payload)
	require.False(t, u.NTP.IsZero())
}

func TestTimestampSEIRTPPackets(t *testing.T) {
	desc := &description.Session{Medias: []*description.Media{{
		Type:    description.MediaTypeVideo,
		Formats: []format.Format{&format.H264{PayloadTyp: 96, PacketizationMode: 1}},
	}}}

	strm := &Stream{
		OrigDesc:          desc,
		WriteQueueSize:    512,
		RTPMaxPayloadSize: 1450,
		ReplaceNTP:        true,
		OwlTimestampSEI:   true,
		Parent:            nilLogger{},
	}
	err := strm.Initialize()
	require.NoError(t, err)
	defer strm.Close()

	subStream := &SubStream{Stream: strm, UseRTPPackets: true}
	err = subStream.Initialize()
	require.NoError(t, err)

	ch := readUnit(t, strm, desc.Medias[0], desc.Medias[0].Formats[0])

	pkt := &rtp.Packet{
		Header: rtp.Header{
			Version:        2,
			Marker:         true,
			PayloadType:    96,
			SequenceNumber: 1,
			Timestamp:      90000,
			SSRC:           1234,
		},
		Payload: []byte{1, 2},
	}

	subStream.WriteUnit(desc.Medias[0], desc.Medias[0].Formats[0], &unit.Unit{
		PTS:        90000,
		RTPPackets: []*rtp.Packet{pkt},
	})

	u := <-ch
	require.Equal(t, unit.PayloadH264{{1, 2}}, u.Payload)
	require.Equal(t, []*rtp.Packet{pkt}, u.RTPPackets)
}

func TestTimestampSEISharedNTP(t *testing.T) {
	for _, ca := range []string{"on", "off"} {
		t.Run(ca, func(t *testing.T) {
			desc := &description.Session{Medias: []*description.Media{
				{
					Type:    description.MediaTypeAudio,
					Formats: []format.Format{&format.Opus{PayloadTyp: 97, ChannelCount: 2}},
				},
				{
					Type:    description.MediaTypeVideo,
					Formats: []format.Format{&format.H264{PayloadTyp: 96, PacketizationMode: 1}},
				},
			}}

			strm := &Stream{
				OrigDesc:          desc,
				WriteQueueSize:    512,
				RTPMaxPayloadSize: 1450,
				ReplaceNTP:        true,
				OwlTimestampSEI:   ca == "on",
				Parent:            nilLogger{},
			}
			err := strm.Initialize()
			require.NoError(t, err)
			defer strm.Close()

			subStream := &SubStream{Stream: strm}
			err = subStream.Initialize()
			require.NoError(t, err)

			audioCh := readUnit(t, strm, desc.Medias[0], desc.Medias[0].Formats[0])
			videoCh := readUnit(t, strm, desc.Medias[1], desc.Medias[1].Formats[0])

			subStream.WriteUnit(desc.Medias[0], desc.Medias[0].Formats[0], &unit.Unit{
				PTS:     48000,
				Payload: unit.PayloadOpus{{1, 2}},
			})
			audio := <-audioCh

			time.Sleep(20 * time.Millisecond)

			subStream.WriteUnit(desc.Medias[1], desc.Medias[1].Formats[0], &unit.Unit{
				PTS:     90000,
				Payload: unit.PayloadH264{{5, 2}},
			})
			video := <-videoCh

			if ca == "on" {
				require.Equal(t, audio.NTP, video.NTP)
				require.Len(t, video.Payload, 2)
			} else {
				require.True(t, video.NTP.After(audio.NTP))
				require.Equal(t, unit.PayloadH264{{5, 2}}, video.Payload)
			}
		})
	}
}

func newSharedNTPStream(t *testing.T) (*SubStream, chan *unit.Unit, chan *unit.Unit, *description.Session) {
	desc := &description.Session{Medias: []*description.Media{
		{
			Type:    description.MediaTypeAudio,
			Formats: []format.Format{&format.Opus{PayloadTyp: 97, ChannelCount: 2}},
		},
		{
			Type:    description.MediaTypeVideo,
			Formats: []format.Format{&format.H264{PayloadTyp: 96, PacketizationMode: 1}},
		},
	}}

	strm := &Stream{
		OrigDesc:          desc,
		WriteQueueSize:    512,
		RTPMaxPayloadSize: 1450,
		ReplaceNTP:        true,
		OwlTimestampSEI:   true,
		Parent:            nilLogger{},
	}
	err := strm.Initialize()
	require.NoError(t, err)
	t.Cleanup(strm.Close)

	subStream := &SubStream{Stream: strm}
	err = subStream.Initialize()
	require.NoError(t, err)

	audioCh := readUnit(t, strm, desc.Medias[0], desc.Medias[0].Formats[0])
	videoCh := readUnit(t, strm, desc.Medias[1], desc.Medias[1].Formats[0])

	return subStream, audioCh, videoCh, desc
}

// The estimator re-anchors when a PTS is ahead of the wall clock,
// so a jump in the audio PTS forces a re-anchor without faking time.
func TestTimestampSEISharedNTPReanchor(t *testing.T) {
	subStream, audioCh, videoCh, desc := newSharedNTPStream(t)

	writeAudio := func(pts int64) *unit.Unit {
		subStream.WriteUnit(desc.Medias[0], desc.Medias[0].Formats[0], &unit.Unit{
			PTS:     pts,
			Payload: unit.PayloadOpus{{1, 2}},
		})
		return <-audioCh
	}
	writeVideo := func(pts int64) *unit.Unit {
		subStream.WriteUnit(desc.Medias[1], desc.Medias[1].Formats[0], &unit.Unit{
			PTS:     pts,
			Payload: unit.PayloadH264{{5, 2}},
		})
		return <-videoCh
	}

	audio1 := writeAudio(48000)
	video1 := writeVideo(90000 - 3600)
	require.Equal(t, audio1.NTP.Add(-40*time.Millisecond), video1.NTP)

	time.Sleep(20 * time.Millisecond)

	audio2 := writeAudio(48000 * 100)
	require.True(t, audio2.NTP.After(audio1.NTP))
	require.Less(t, audio2.NTP.Sub(audio1.NTP), 99*time.Second)

	video2 := writeVideo(90000*100 - 3600)
	require.Equal(t, audio2.NTP.Add(-40*time.Millisecond), video2.NTP)
	require.Equal(t, audio2.NTP.Sub(audio1.NTP), video2.NTP.Sub(video1.NTP))
}

func TestTimestampSEISharedNTPBFrames(t *testing.T) {
	subStream, audioCh, videoCh, desc := newSharedNTPStream(t)

	subStream.WriteUnit(desc.Medias[0], desc.Medias[0].Formats[0], &unit.Unit{
		PTS:     48000 * 10,
		Payload: unit.PayloadOpus{{1, 2}},
	})
	anchor := (<-audioCh).NTP

	// PTS in ms before the anchor, video in decode order.
	for _, w := range []struct {
		video bool
		ms    int64
	}{
		{true, -300},
		{false, -280},
		{true, -200},
		{true, -267},
		{false, -260},
		{true, -233},
		{true, -100},
		{false, -240},
		{true, -167},
		{true, -133},
	} {
		if w.video {
			subStream.WriteUnit(desc.Medias[1], desc.Medias[1].Formats[0], &unit.Unit{
				PTS:     90000*10 + w.ms*90,
				Payload: unit.PayloadH264{{1, 2}},
			})
			u := <-videoCh
			require.Equal(t, anchor.Add(time.Duration(w.ms)*time.Millisecond), u.NTP)
			require.Equal(t, timestampsei.Build(u.NTP, 90000*10+w.ms*90, timestampsei.CodecH264, 0),
				u.Payload.(unit.PayloadH264)[0])
		} else {
			subStream.WriteUnit(desc.Medias[0], desc.Medias[0].Formats[0], &unit.Unit{
				PTS:     48000*10 + w.ms*48,
				Payload: unit.PayloadOpus{{1, 2}},
			})
			require.Equal(t, anchor.Add(time.Duration(w.ms)*time.Millisecond), (<-audioCh).NTP)
		}
	}
}
