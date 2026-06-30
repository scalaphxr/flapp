package midi

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"

	"github.com/flapp/core/internal/domain"
)

// ParseSMF парсит Standard MIDI File (формат 0 или 1) и возвращает ноты для пианоролла.
func ParseSMF(data []byte) (*domain.MidiNotesResult, error) {
	r := bytes.NewReader(data)

	// ── MThd ──────────────────────────────────────────────────────────────────
	var hdrID [4]byte
	if _, err := io.ReadFull(r, hdrID[:]); err != nil {
		return nil, errors.New("parse midi: truncated header")
	}
	if string(hdrID[:]) != "MThd" {
		return nil, errors.New("parse midi: not a MIDI file")
	}
	var hdrLen uint32
	if err := binary.Read(r, binary.BigEndian, &hdrLen); err != nil {
		return nil, err
	}
	hdrData := make([]byte, hdrLen)
	if _, err := io.ReadFull(r, hdrData); err != nil {
		return nil, err
	}
	if hdrLen < 6 {
		return nil, errors.New("parse midi: header too short")
	}
	division := binary.BigEndian.Uint16(hdrData[4:6])
	ticksPerBeat := int(division)
	if division&0x8000 != 0 {
		// SMPTE format — редкий случай, используем дефолт
		ticksPerBeat = 96
	}

	// ── Треки ─────────────────────────────────────────────────────────────────
	bpm := 120.0
	var allNotes []domain.MidiNote
	maxEndTick := 0

	for {
		var chunkID [4]byte
		if _, err := io.ReadFull(r, chunkID[:]); err != nil {
			break
		}
		var chunkLen uint32
		if err := binary.Read(r, binary.BigEndian, &chunkLen); err != nil {
			break
		}
		chunkData := make([]byte, chunkLen)
		if _, err := io.ReadFull(r, chunkData); err != nil {
			break
		}
		if string(chunkID[:]) != "MTrk" {
			continue
		}
		tBPM, notes := parseTrack(chunkData)
		if tBPM > 0 {
			bpm = tBPM
		}
		for _, n := range notes {
			end := n.Tick + n.DurationTicks
			if end > maxEndTick {
				maxEndTick = end
			}
			allNotes = append(allNotes, n)
		}
	}

	if allNotes == nil {
		allNotes = []domain.MidiNote{}
	}

	return &domain.MidiNotesResult{
		BPM:           bpm,
		TicksPerBeat:  ticksPerBeat,
		DurationTicks: maxEndTick,
		Notes:         allNotes,
	}, nil
}

// parseTrack парсит тело MTrk и возвращает BPM (из tempo meta) и список нот.
func parseTrack(data []byte) (bpm float64, notes []domain.MidiNote) {
	pos := 0
	absTime := 0

	type pending struct{ tick, vel int }
	inFlight := make(map[int]pending)
	var runStat byte

	for pos < len(data) {
		delta, n := smfReadVarLen(data[pos:])
		if n == 0 {
			break
		}
		pos += n
		absTime += delta

		if pos >= len(data) {
			break
		}

		b := data[pos]
		var status byte
		if b&0x80 != 0 {
			status = b
			runStat = b
			pos++
		} else {
			status = runStat
			// b — первый байт данных, pos не сдвигаем
		}

		msgType := status & 0xF0

		switch {
		case status == 0xFF: // Meta-событие
			if pos >= len(data) {
				return
			}
			metaType := data[pos]
			pos++
			ml, n := smfReadVarLen(data[pos:])
			pos += n
			if pos+ml > len(data) {
				return
			}
			md := data[pos : pos+ml]
			pos += ml
			runStat = 0
			if metaType == 0x51 && ml == 3 {
				us := int(md[0])<<16 | int(md[1])<<8 | int(md[2])
				if us > 0 {
					bpm = 60_000_000.0 / float64(us)
				}
			}

		case status == 0xF0 || status == 0xF7: // SysEx
			sl, n := smfReadVarLen(data[pos:])
			pos += n + sl
			runStat = 0

		case msgType == 0x90: // Note On
			if pos+1 >= len(data) {
				return
			}
			pitch := int(data[pos])
			vel := int(data[pos+1])
			pos += 2
			if vel == 0 {
				// Note Off закодированный как Note On vel=0
				if p, ok := inFlight[pitch]; ok {
					dur := absTime - p.tick
					if dur < 1 {
						dur = 1
					}
					notes = append(notes, domain.MidiNote{Tick: p.tick, DurationTicks: dur, Pitch: pitch, Velocity: p.vel})
					delete(inFlight, pitch)
				}
			} else {
				inFlight[pitch] = pending{tick: absTime, vel: vel}
			}

		case msgType == 0x80: // Note Off
			if pos+1 >= len(data) {
				return
			}
			pitch := int(data[pos])
			pos += 2
			if p, ok := inFlight[pitch]; ok {
				dur := absTime - p.tick
				if dur < 1 {
					dur = 1
				}
				notes = append(notes, domain.MidiNote{Tick: p.tick, DurationTicks: dur, Pitch: pitch, Velocity: p.vel})
				delete(inFlight, pitch)
			}

		case msgType == 0xA0: // Key Aftertouch
			pos += 2
		case msgType == 0xB0: // Control Change
			pos += 2
		case msgType == 0xC0: // Program Change (1 байт данных)
			pos++
		case msgType == 0xD0: // Channel Pressure (1 байт данных)
			pos++
		case msgType == 0xE0: // Pitch Bend (2 байта данных)
			pos += 2
		default:
			return
		}
	}

	// Закрываем незакрытые ноты
	for pitch, p := range inFlight {
		dur := absTime - p.tick
		if dur < 1 {
			dur = 1
		}
		notes = append(notes, domain.MidiNote{Tick: p.tick, DurationTicks: dur, Pitch: pitch, Velocity: p.vel})
	}

	return bpm, notes
}

// smfReadVarLen декодирует MIDI variable-length quantity из среза байт.
// Возвращает значение и количество прочитанных байт (0 при ошибке).
func smfReadVarLen(data []byte) (int, int) {
	var result int
	for i, b := range data {
		result = (result << 7) | int(b&0x7F)
		if b&0x80 == 0 {
			return result, i + 1
		}
		if i >= 4 {
			break // variable-length quantity максимум 4 байта
		}
	}
	return 0, 0
}
