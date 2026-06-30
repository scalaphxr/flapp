// Package midi — multi-track MIDI export from FL Studio piano-roll data.
//
// Two export modes:
//
//   strict  — preserves FL Studio tick positions exactly as parsed. Intended
//             for round-tripping back into a DAW that understands FL timings.
//
//   clean   — quantizes note positions to the nearest 1/16 and enforces a
//             minimum note duration of 1/32. Removes near-zero-velocity ghosts
//             (vel < 5). Suited for DAWs and MPC/hardware that choke on
//             sub-tick precision.
//
// Each FL Studio pattern becomes a separate MIDI track (SMF format 1).
// A tempo-map meta event is written in track 0; pattern tracks follow.
package midi

import (
	"bytes"
	"fmt"
	"sort"
)

// ExportMode selects the timing mode for MIDI export.
type ExportMode string

const (
	ModeStrict ExportMode = "strict" // exact FL ticks, no quantisation
	ModeClean  ExportMode = "clean"  // quantised, minimum duration enforced
)

// defaultPPQ is the PPQ used when the project's PPQ is not available or zero.
const defaultPPQ = 960

// PatternTrack holds the notes for one FL Studio pattern.
type PatternTrack struct {
	PatternName string
	Channel     int
	Notes       []NoteEvent
}

// TempoPoint is one tempo-map entry (tick position + BPM).
type TempoPoint struct {
	Tick uint32
	BPM  float64
}

// MultiTrackParams configures a multi-track export.
type MultiTrackParams struct {
	PPQ        int          // ticks per quarter note from FLhd (0 → defaultPPQ)
	TempoMap   []TempoPoint // sorted by tick; first entry is initial tempo
	TimeSignNumerator   uint8 // default 4/4 if zero
	TimeSignDenominator uint8 // denominator as power of 2 (e.g. 2 = quarter = /4)
	Tracks     []PatternTrack
	Mode       ExportMode
}

// WriteSMFMultiTrack produces a Format-1 SMF with one tempo/signature track
// (track 0) plus one track per PatternTrack. PPQ is taken from params.PPQ.
func WriteSMFMultiTrack(params MultiTrackParams) ([]byte, error) {
	ppq := params.PPQ
	if ppq <= 0 {
		ppq = defaultPPQ
	}
	if len(params.Tracks) == 0 {
		return nil, fmt.Errorf("midi: no tracks to export")
	}

	numTracks := uint16(1 + len(params.Tracks)) // track 0 = tempo/meta

	var out bytes.Buffer
	// MThd: format=1, nTracks, division=ppq
	out.WriteString("MThd")
	writeU32BE(&out, 6)
	writeU16BE(&out, 1) // format 1
	writeU16BE(&out, numTracks)
	writeU16BE(&out, uint16(ppq))

	// Track 0: tempo map + time signature.
	tempoTrack := buildTempoTrack(params)
	writeMTrkBytes(&out, tempoTrack)

	// Pattern tracks.
	for _, pt := range params.Tracks {
		notes := applyMode(pt.Notes, ppq, params.Mode)
		track := buildPatternTrack(pt.PatternName, notes)
		writeMTrkBytes(&out, track)
	}
	return out.Bytes(), nil
}

// buildTempoTrack produces the tempo-map + time-signature track (track 0).
func buildTempoTrack(params MultiTrackParams) []byte {
	var trk bytes.Buffer

	// Time signature at tick 0.
	num := params.TimeSignNumerator
	if num == 0 {
		num = 4
	}
	den := params.TimeSignDenominator
	if den == 0 {
		den = 2 // 2^2 = 4 → /4
	}
	writeVarLen(&trk, 0)
	trk.Write([]byte{0xFF, 0x58, 0x04, num, den, 0x18, 0x08})

	// Tempo changes in chronological order.
	tm := params.TempoMap
	if len(tm) == 0 {
		tm = []TempoPoint{{Tick: 0, BPM: 120}}
	}
	sort.Slice(tm, func(i, j int) bool { return tm[i].Tick < tm[j].Tick })

	prevTick := uint32(0)
	for _, tp := range tm {
		bpm := tp.BPM
		if bpm <= 0 {
			bpm = 120
		}
		delta := tp.Tick - prevTick
		prevTick = tp.Tick
		usPerBeat := uint32(60_000_000.0 / bpm)
		writeVarLen(&trk, int(delta))
		trk.Write([]byte{
			0xFF, 0x51, 0x03,
			byte(usPerBeat >> 16),
			byte(usPerBeat >> 8),
			byte(usPerBeat),
		})
	}

	// Track name.
	writeVarLen(&trk, 0)
	writeMetaText(&trk, 0x03, "Tempo Map")

	// End of track.
	writeVarLen(&trk, 0)
	trk.Write([]byte{0xFF, 0x2F, 0x00})
	return trk.Bytes()
}

// buildPatternTrack writes one pattern's notes into a MIDI track chunk body.
func buildPatternTrack(name string, notes []NoteEvent) []byte {
	var trk bytes.Buffer

	// Track name meta event.
	writeVarLen(&trk, 0)
	writeMetaText(&trk, 0x03, name)

	type ev struct {
		tick   uint32
		status byte
		note   uint8
		vel    uint8
		ch     uint8
	}

	evs := make([]ev, 0, len(notes)*2)
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
		if end <= n.PositionTicks {
			end = n.PositionTicks + 1
		}
		evs = append(evs,
			ev{tick: n.PositionTicks, status: 0x90, note: key, vel: vel},
			ev{tick: end, status: 0x80, note: key, vel: 0},
		)
	}
	sort.SliceStable(evs, func(i, j int) bool {
		if evs[i].tick != evs[j].tick {
			return evs[i].tick < evs[j].tick
		}
		return evs[i].status < evs[j].status // note-off before note-on at same tick
	})

	prevTick := uint32(0)
	for _, e := range evs {
		delta := e.tick - prevTick
		prevTick = e.tick
		writeVarLen(&trk, int(delta))
		trk.Write([]byte{e.status, e.note, e.vel})
	}

	writeVarLen(&trk, 0)
	trk.Write([]byte{0xFF, 0x2F, 0x00})
	return trk.Bytes()
}

// applyMode applies strict or clean timing to the note list.
func applyMode(notes []NoteEvent, ppq int, mode ExportMode) []NoteEvent {
	if mode != ModeClean || ppq <= 0 {
		return notes
	}

	// Quantisation grid: 1/16 note.
	sixteenth := uint32(ppq / 4)
	if sixteenth < 1 {
		sixteenth = 1
	}
	// Minimum note duration: 1/32.
	minLen := sixteenth / 2
	if minLen < 1 {
		minLen = 1
	}

	out := make([]NoteEvent, 0, len(notes))
	for _, n := range notes {
		// Snap position to nearest 1/16.
		snapped := quantise(n.PositionTicks, sixteenth)

		// Enforce minimum duration.
		length := n.LengthTicks
		if length < minLen {
			length = minLen
		}
		// Snap length to nearest 1/16 as well.
		length = quantise(length, sixteenth)
		if length < minLen {
			length = minLen
		}

		// Drop ghost notes (very low velocity).
		if n.Velocity < 5 {
			continue
		}

		out = append(out, NoteEvent{
			PositionTicks: snapped,
			LengthTicks:   length,
			Key:           n.Key,
			Velocity:      n.Velocity,
		})
	}
	return out
}

// quantise rounds v to the nearest multiple of grid (rounds up at mid-point).
func quantise(v, grid uint32) uint32 {
	if grid == 0 {
		return v
	}
	return ((v + grid/2) / grid) * grid
}

// writeMTrkBytes wraps body in an MTrk header and appends to w.
func writeMTrkBytes(w *bytes.Buffer, body []byte) {
	w.WriteString("MTrk")
	writeU32BE(w, uint32(len(body)))
	w.Write(body)
}

// writeMetaText writes a MIDI meta event with the given type byte and text.
func writeMetaText(w *bytes.Buffer, metaType byte, text string) {
	b := []byte(text)
	w.Write([]byte{0xFF, metaType})
	writeVarLen(w, len(b))
	w.Write(b)
}

// writeU16BE and writeU32BE are defined in writer.go (same package).
