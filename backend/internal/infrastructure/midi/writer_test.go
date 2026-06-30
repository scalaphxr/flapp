package midi

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// parseMThd минимально разбирает заголовочный чанк MThd.
func parseMThd(data []byte) (format, ntracks, ppq int, ok bool) {
	if len(data) < 14 || !bytes.Equal(data[0:4], []byte("MThd")) {
		return
	}
	chunkLen := binary.BigEndian.Uint32(data[4:8])
	if chunkLen < 6 || len(data) < 14 {
		return
	}
	format = int(binary.BigEndian.Uint16(data[8:10]))
	ntracks = int(binary.BigEndian.Uint16(data[10:12]))
	ppq = int(binary.BigEndian.Uint16(data[12:14]))
	ok = true
	return
}

// parseMTrk возвращает длину тела трека и саму область.
func parseMTrk(data []byte, offset int) (body []byte, ok bool) {
	if offset+8 > len(data) {
		return
	}
	if !bytes.Equal(data[offset:offset+4], []byte("MTrk")) {
		return
	}
	chunkLen := int(binary.BigEndian.Uint32(data[offset+4 : offset+8]))
	end := offset + 8 + chunkLen
	if end > len(data) {
		return
	}
	return data[offset+8 : end], true
}

func TestWriteSMFBasic(t *testing.T) {
	notes := []NoteEvent{
		{PositionTicks: 0, LengthTicks: 96, Key: 60, Velocity: 100},
		{PositionTicks: 96, LengthTicks: 96, Key: 64, Velocity: 90},
		{PositionTicks: 192, LengthTicks: 192, Key: 67, Velocity: 80},
	}

	data, err := WriteSMF(notes, 96, 120.0)
	if err != nil {
		t.Fatalf("WriteSMF: %v", err)
	}

	// Проверяем MThd.
	format, ntracks, ppq, ok := parseMThd(data)
	if !ok {
		t.Fatal("invalid MThd header")
	}
	if format != 0 {
		t.Errorf("format = %d, want 0", format)
	}
	if ntracks != 1 {
		t.Errorf("ntracks = %d, want 1", ntracks)
	}
	if ppq != 96 {
		t.Errorf("ppq = %d, want 96", ppq)
	}

	// Проверяем MTrk.
	body, ok := parseMTrk(data, 14)
	if !ok {
		t.Fatal("invalid MTrk chunk")
	}
	if len(body) == 0 {
		t.Fatal("track body is empty")
	}

	// Трек должен заканчиваться на meta end-of-track: 00 FF 2F 00.
	if len(body) < 4 {
		t.Fatal("track body too short for end-of-track")
	}
	tail := body[len(body)-4:]
	if !bytes.Equal(tail, []byte{0x00, 0xFF, 0x2F, 0x00}) {
		t.Errorf("track does not end with end-of-track: % X", tail)
	}
}

func TestWriteSMFTempoInTrack(t *testing.T) {
	notes := []NoteEvent{
		{PositionTicks: 0, LengthTicks: 48, Key: 60, Velocity: 100},
	}

	data, err := WriteSMF(notes, 48, 140.0)
	if err != nil {
		t.Fatalf("WriteSMF: %v", err)
	}

	body, ok := parseMTrk(data, 14)
	if !ok {
		t.Fatal("invalid MTrk")
	}

	// Первые байты тела: varint 0, FF 51 03, + 3 байта темпа.
	if len(body) < 7 {
		t.Fatal("track body too short for tempo event")
	}
	// delta=0 → 0x00, meta type FF 51, len 03.
	if body[0] != 0x00 || body[1] != 0xFF || body[2] != 0x51 || body[3] != 0x03 {
		t.Errorf("expected tempo meta event at start, got: % X", body[:7])
	}

	// Проверяем значение темпа: 60_000_000 / 140 ≈ 428571 мкс/бит.
	usPerBeat := int(body[4])<<16 | int(body[5])<<8 | int(body[6])
	want := int(60_000_000 / 140)
	if abs(usPerBeat-want) > 1 {
		t.Errorf("tempo = %d µs/beat, want ≈%d", usPerBeat, want)
	}
}

func TestWriteSMFEmpty(t *testing.T) {
	data, err := WriteSMF(nil, 96, 120.0)
	if err != nil {
		t.Fatalf("WriteSMF(nil): %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty SMF for empty note list (still needs MThd + MTrk)")
	}
	_, _, _, ok := parseMThd(data)
	if !ok {
		t.Fatal("MThd invalid for empty notes")
	}
}

func TestVarLenRoundTrip(t *testing.T) {
	for _, n := range []int{0, 1, 127, 128, 255, 8192, 1_000_000} {
		var buf bytes.Buffer
		writeVarLen(&buf, n)
		// Раскодируем обратно: MIDI VLQ big-endian (MSB group first).
		got := 0
		for _, b := range buf.Bytes() {
			got = (got << 7) | int(b&0x7F)
			if b&0x80 == 0 {
				break
			}
		}
		if got != n {
			t.Errorf("varLen round-trip %d → %d", n, got)
		}
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
