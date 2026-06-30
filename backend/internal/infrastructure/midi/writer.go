// Package midi реализует запись Standard MIDI File (SMF) формата 0 и
// категоризацию MIDI-клипов по источнику-каналу FL Studio.
package midi

import (
	"bytes"
	"encoding/binary"
	"sort"
)

// NoteEvent — одна нота для записи в MIDI-файл.
type NoteEvent struct {
	PositionTicks uint32
	LengthTicks   uint32
	Key           uint8 // MIDI note number 0-127
	Velocity      uint8
}

// WriteSMF записывает список нот в Standard MIDI File (формат 0, один трек).
//
// ppq — тиков на четверть ноты из FLhd.
// bpm — темп проекта.
// Позиции и длины нот переносятся из FL Studio 1:1 в тиках.
func WriteSMF(notes []NoteEvent, ppq int, bpm float64) ([]byte, error) {
	if ppq <= 0 {
		ppq = 96
	}
	if bpm <= 0 {
		bpm = 120
	}

	// Сортируем ноты по позиции для детерминированного порядка событий.
	sorted := make([]NoteEvent, len(notes))
	copy(sorted, notes)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].PositionTicks < sorted[j].PositionTicks
	})

	track := buildTrack(sorted, bpm)

	var out bytes.Buffer
	writeMThd(&out, ppq)
	writeMTrk(&out, track)
	return out.Bytes(), nil
}

// writeMThd записывает заголовочный чанк MThd (6 байт данных).
func writeMThd(w *bytes.Buffer, ppq int) {
	w.WriteString("MThd")
	writeU32BE(w, 6)           // длина чанка всегда 6
	writeU16BE(w, 0)           // формат 0 — один трек
	writeU16BE(w, 1)           // количество треков
	writeU16BE(w, uint16(ppq)) // division: тиков на четверть (bit15=0)
}

// writeMTrk оборачивает тело трека в заголовок MTrk.
func writeMTrk(w *bytes.Buffer, track []byte) {
	w.WriteString("MTrk")
	writeU32BE(w, uint32(len(track)))
	w.Write(track)
}

// buildTrack строит тело MTrk: meta-событие темпа, note on/off пары, end of track.
func buildTrack(notes []NoteEvent, bpm float64) []byte {
	var trk bytes.Buffer

	// Meta-событие: установка темпа (microseconds per beat).
	usPerBeat := uint32(60_000_000.0 / bpm)
	writeVarLen(&trk, 0) // delta = 0
	trk.Write([]byte{
		0xFF, 0x51, 0x03,
		byte(usPerBeat >> 16),
		byte(usPerBeat >> 8),
		byte(usPerBeat),
	})

	// Собираем все события note-on и note-off.
	type midiEvent struct {
		tick   uint32
		status byte  // 0x90 = note on, 0x80 = note off
		note   uint8
		vel    uint8
	}

	events := make([]midiEvent, 0, len(notes)*2)
	for _, n := range notes {
		key := n.Key
		if key > 127 {
			key = 127
		}
		vel := n.Velocity
		if vel == 0 {
			vel = 100
		}
		end := n.PositionTicks + n.LengthTicks
		if end < n.PositionTicks {
			end = n.PositionTicks + 1 // overflow guard
		}
		events = append(events,
			midiEvent{tick: n.PositionTicks, status: 0x90, note: key, vel: vel},
			midiEvent{tick: end, status: 0x80, note: key, vel: 0},
		)
	}

	// Сортируем: по тику, при равных тиках note-off (0x80) раньше note-on (0x90).
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].tick != events[j].tick {
			return events[i].tick < events[j].tick
		}
		return events[i].status < events[j].status
	})

	// Пишем события с delta-временем на канале 0.
	var prevTick uint32
	for _, e := range events {
		delta := e.tick - prevTick
		prevTick = e.tick
		writeVarLen(&trk, int(delta))
		trk.Write([]byte{e.status, e.note, e.vel})
	}

	// Meta-событие: конец трека.
	writeVarLen(&trk, 0)
	trk.Write([]byte{0xFF, 0x2F, 0x00})

	return trk.Bytes()
}

// writeVarLen кодирует неотрицательное целое как MIDI variable-length quantity
// (big-endian 7-битные группы, старший бит установлен у всех кроме последнего).
func writeVarLen(w *bytes.Buffer, n int) {
	if n < 0 {
		n = 0
	}
	var buf [5]byte
	i := 4
	buf[i] = byte(n & 0x7F)
	n >>= 7
	for n > 0 {
		i--
		buf[i] = byte(n&0x7F) | 0x80
		n >>= 7
	}
	w.Write(buf[i:])
}

func writeU16BE(w *bytes.Buffer, v uint16) {
	_ = binary.Write(w, binary.BigEndian, v)
}

func writeU32BE(w *bytes.Buffer, v uint32) {
	_ = binary.Write(w, binary.BigEndian, v)
}
