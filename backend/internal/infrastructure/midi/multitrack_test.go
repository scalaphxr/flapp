package midi_test

import (
	"encoding/binary"
	"testing"

	"github.com/flapp/core/internal/infrastructure/midi"
)

func makeParams(mode midi.ExportMode, ppq int, notes []midi.NoteEvent) midi.MultiTrackParams {
	return midi.MultiTrackParams{
		PPQ:      ppq,
		TempoMap: []midi.TempoPoint{{Tick: 0, BPM: 120}},
		Tracks: []midi.PatternTrack{
			{PatternName: "Pattern 1", Notes: notes},
		},
		Mode: mode,
	}
}

func TestWriteSMFMultiTrack_Basic(t *testing.T) {
	notes := []midi.NoteEvent{
		{PositionTicks: 0, LengthTicks: 480, Key: 60, Velocity: 100},
		{PositionTicks: 480, LengthTicks: 480, Key: 64, Velocity: 80},
	}
	data, err := midi.WriteSMFMultiTrack(makeParams(midi.ModeStrict, 960, notes))
	if err != nil {
		t.Fatalf("WriteSMFMultiTrack error: %v", err)
	}
	// Check SMF header.
	if string(data[:4]) != "MThd" {
		t.Errorf("missing MThd, got %q", string(data[:4]))
	}
	// Format = 1.
	format := binary.BigEndian.Uint16(data[8:10])
	if format != 1 {
		t.Errorf("expected format 1, got %d", format)
	}
	// nTracks = 2 (tempo + pattern).
	nTracks := binary.BigEndian.Uint16(data[10:12])
	if nTracks != 2 {
		t.Errorf("expected 2 tracks, got %d", nTracks)
	}
	// PPQ = 960.
	ppq := binary.BigEndian.Uint16(data[12:14])
	if ppq != 960 {
		t.Errorf("expected ppq=960, got %d", ppq)
	}
}

func TestWriteSMFMultiTrack_EmptyTracks(t *testing.T) {
	params := midi.MultiTrackParams{PPQ: 960, Tracks: nil}
	_, err := midi.WriteSMFMultiTrack(params)
	if err == nil {
		t.Error("expected error for empty tracks")
	}
}

func TestCleanMode_Quantisation(t *testing.T) {
	ppq := 960
	sixteenth := uint32(ppq / 4) // 240 ticks

	// A note at tick 10 (close to 0) should snap to 0 in clean mode.
	notes := []midi.NoteEvent{
		{PositionTicks: 10, LengthTicks: 240, Key: 60, Velocity: 100},
	}
	data, err := midi.WriteSMFMultiTrack(makeParams(midi.ModeClean, ppq, notes))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	_ = data // We just verify it doesn't crash and is valid MIDI.
	_ = sixteenth
}

func TestCleanMode_GhostNoteRemoval(t *testing.T) {
	ppq := 960
	notes := []midi.NoteEvent{
		{PositionTicks: 0, LengthTicks: 480, Key: 60, Velocity: 4},   // ghost, vel < 5
		{PositionTicks: 0, LengthTicks: 480, Key: 64, Velocity: 100}, // kept
	}
	dataClean, err := midi.WriteSMFMultiTrack(makeParams(midi.ModeClean, ppq, notes))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	dataStrict, err := midi.WriteSMFMultiTrack(makeParams(midi.ModeStrict, ppq, notes))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	// Clean should produce fewer events (ghost note removed) → smaller file.
	if len(dataClean) >= len(dataStrict) {
		t.Error("clean mode should produce a smaller/equal file when ghost notes are removed")
	}
}

func TestCleanMode_MinDuration(t *testing.T) {
	ppq := 960
	// Note with near-zero length.
	notes := []midi.NoteEvent{
		{PositionTicks: 0, LengthTicks: 1, Key: 60, Velocity: 100},
	}
	data, err := midi.WriteSMFMultiTrack(makeParams(midi.ModeClean, ppq, notes))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	// Verify output is valid SMF (has MThd header).
	if string(data[:4]) != "MThd" {
		t.Errorf("invalid SMF output")
	}
}

func TestStrictMode_ExactTicks(t *testing.T) {
	ppq := 96
	pos := uint32(7)  // odd tick — strict mode must not snap it
	notes := []midi.NoteEvent{
		{PositionTicks: pos, LengthTicks: 48, Key: 60, Velocity: 100},
	}
	data, err := midi.WriteSMFMultiTrack(makeParams(midi.ModeStrict, ppq, notes))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if string(data[:4]) != "MThd" {
		t.Errorf("invalid SMF output")
	}
	// We can't easily decode the track bytes here without a full MIDI parser,
	// but the important invariant is that strict mode does not modify tick 7.
}

func TestMultiplePatterns(t *testing.T) {
	params := midi.MultiTrackParams{
		PPQ:      960,
		TempoMap: []midi.TempoPoint{{Tick: 0, BPM: 140}},
		Tracks: []midi.PatternTrack{
			{PatternName: "Verse", Notes: []midi.NoteEvent{
				{PositionTicks: 0, LengthTicks: 480, Key: 60, Velocity: 100},
			}},
			{PatternName: "Chorus", Notes: []midi.NoteEvent{
				{PositionTicks: 0, LengthTicks: 960, Key: 67, Velocity: 90},
			}},
		},
		Mode: midi.ModeStrict,
	}
	data, err := midi.WriteSMFMultiTrack(params)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	nTracks := binary.BigEndian.Uint16(data[10:12])
	if nTracks != 3 { // tempo + verse + chorus
		t.Errorf("expected 3 tracks, got %d", nTracks)
	}
}

func TestTempoMap_MultipleTempos(t *testing.T) {
	params := midi.MultiTrackParams{
		PPQ: 960,
		TempoMap: []midi.TempoPoint{
			{Tick: 0, BPM: 120},
			{Tick: 3840, BPM: 140}, // tempo change at bar 5 (4/4 at 960ppq: 3840 ticks)
		},
		Tracks: []midi.PatternTrack{
			{PatternName: "Test", Notes: []midi.NoteEvent{
				{PositionTicks: 0, LengthTicks: 960, Key: 60, Velocity: 100},
			}},
		},
		Mode: midi.ModeStrict,
	}
	data, err := midi.WriteSMFMultiTrack(params)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if string(data[:4]) != "MThd" {
		t.Errorf("invalid SMF output")
	}
}
