package domain

// Category is the canonical sound taxonomy used throughout the app.
type Category string

const (
	Cat808      Category = "808"
	CatKick     Category = "Kick"
	CatSnare    Category = "Snare"
	CatClap     Category = "Clap"
	CatHiHat    Category = "Hi-Hat"
	CatOpenHat  Category = "Open Hat"
	CatPerc     Category = "Perc"
	CatVox      Category = "Vox"
	CatFX       Category = "FX"
	CatLoop     Category = "Loop"
	CatDrumLoop Category = "Drum Loop"
)

// ColorGroup maps a Category onto one of the design-system pill tones.
type ColorGroup string

const (
	GroupSub      ColorGroup = "808"      // --cat-808
	GroupKick     ColorGroup = "kick"     // --cat-kick
	GroupSnare    ColorGroup = "snare"    // --cat-snare
	GroupClap     ColorGroup = "clap"     // --cat-clap
	GroupHiHat    ColorGroup = "hihat"    // --cat-hihat
	GroupOpenHat  ColorGroup = "openhat"  // --cat-openhat
	GroupPerc     ColorGroup = "perc"     // --cat-perc
	GroupVox      ColorGroup = "vox"      // --cat-vox
	GroupFX       ColorGroup = "fx"       // --cat-fx
	GroupLoop     ColorGroup = "bass"     // --cat-bass (repurposed for loop)
	GroupDrumLoop ColorGroup = "drumloop" // --cat-drumloop
	GroupOther    ColorGroup = "unsorted" // --cat-unsorted
)

// AllCategories is the ordered list shown in filters and analytics.
var AllCategories = []Category{
	Cat808, CatKick, CatSnare, CatClap, CatHiHat, CatOpenHat,
	CatPerc, CatVox, CatFX, CatLoop, CatDrumLoop,
}

var categoryColorGroup = map[Category]ColorGroup{
	Cat808:      GroupSub,
	CatKick:     GroupKick,
	CatSnare:    GroupSnare,
	CatClap:     GroupClap,
	CatHiHat:    GroupHiHat,
	CatOpenHat:  GroupOpenHat,
	CatPerc:     GroupPerc,
	CatVox:      GroupVox,
	CatFX:       GroupFX,
	CatLoop:     GroupLoop,
	CatDrumLoop: GroupDrumLoop,
}

// Group returns the design-system color group for a category.
func (c Category) Group() ColorGroup {
	if g, ok := categoryColorGroup[c]; ok {
		return g
	}
	return GroupOther
}

// IsLoop reports whether the category represents a multi-bar loop.
func (c Category) IsLoop() bool {
	return c == CatDrumLoop || c == CatLoop
}

// IsDrum reports whether the category is a percussive one-shot.
func (c Category) IsDrum() bool {
	switch c {
	case Cat808, CatKick, CatSnare, CatClap, CatHiHat, CatOpenHat, CatPerc:
		return true
	}
	return false
}

// RemapLegacy converts old fine-grained category names (from before the
// simplified taxonomy) to their new equivalents. Returns the input unchanged
// if it already belongs to the current set.
func RemapLegacy(raw string) Category {
	switch raw {
	case "808":
		return Cat808
	case "Kick":
		return CatKick
	case "Snare":
		return CatSnare
	case "Clap", "Rim":
		return CatClap
	case "Hi-Hat":
		return CatHiHat
	case "Open Hat", "Crash", "Ride", "Cymbal":
		return CatOpenHat
	case "Perc", "Tom", "Foley", "One Shot":
		return CatPerc
	case "Vox", "Chant", "Shout", "Vocal":
		return CatVox
	case "FX", "Sweep", "Impact", "Riser", "Downlifter",
		"Texture", "Ambience", "MIDI", "Synth", "Other":
		return CatFX
	case "Loop", "Melody Loop", "Piano", "Guitar", "Bell",
		"Pluck", "Pad", "Bass", "Melody":
		return CatLoop
	case "Drum Loop", "Fill":
		return CatDrumLoop
	}
	// Already in the new set or unknown → return as-is.
	return Category(raw)
}
