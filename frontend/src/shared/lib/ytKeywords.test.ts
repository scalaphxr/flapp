import { buildHashtags, buildKeywords, mergeRoster, parseRoster } from "./ytKeywords.ts";

function eq(got: unknown, want: unknown, msg: string): void {
  const G = JSON.stringify(got), W = JSON.stringify(want);
  if (G !== W) throw new Error(`FAIL ${msg}\n  got:  ${G}\n  want: ${W}`);
  console.log(`ok - ${msg}`);
}

// parseRoster: split on comma/newline, trim, drop empty, case-insensitive dedup, keep order
eq(parseRoster("jeezy, Chief Keef\n jeezy ,,\nbuy rap beats"),
   ["jeezy", "Chief Keef", "buy rap beats"], "parseRoster splits & dedups");

// buildKeywords: front slice (per artist) → roster → evergreen tail, lowercased, deduped
const kw = buildKeywords(["Bankroll Fresh", "MexikoDro"], "jeezy, chief keef", 2026);
const parts = kw.split(", ");
eq(parts[0], "bankroll fresh type beat", "keywords front slice first");
eq(parts[1], "free bankroll fresh type beat", "keywords free variant");
eq(parts[2], "bankroll fresh type beat 2026", "keywords year variant");
eq(parts[3], "bankroll fresh", "keywords bare artist");
eq(parts.includes("jeezy"), true, "keywords include roster");
eq(parts.includes("chief keef"), true, "keywords include roster 2");
eq(parts.includes("type beat"), true, "keywords include evergreen tail");
eq(new Set(parts).size, parts.length, "keywords deduped");

// buildKeywords budget: never exceed the char budget
const big = Array.from({ length: 500 }, (_, i) => `artist${i}`).join(", ");
const capped = buildKeywords([], big, 2026, 300);
eq(capped.length <= 300, true, `keywords respect budget (len=${capped.length})`);

// buildKeywords: empty roster still works from artists + evergreen
const noRoster = buildKeywords(["Gunna"], "", 2026).split(", ");
eq(noRoster[0], "gunna type beat", "keywords work with empty roster");
eq(noRoster.includes("instrumental"), true, "keywords evergreen with empty roster");

// buildHashtags: CamelCase #ArtistTypeBeat, alnum only, dedup, cap 15
eq(buildHashtags(["bankroll fresh", "MexikoDro", "warhol.ss"]),
   "#BankrollFreshTypeBeat #MexikodroTypeBeat #WarholSsTypeBeat", "hashtags camelcase");
eq(buildHashtags(["jeezy", "Jeezy"]), "#JeezyTypeBeat", "hashtags dedup ci");
eq(buildHashtags(Array.from({ length: 30 }, (_, i) => `a${i}`)).split(" ").length, 15,
   "hashtags cap 15");

// mergeRoster: append bare + "{artist} type beat" for new artists only
eq(mergeRoster("jeezy", ["Bankroll Fresh", "jeezy"]),
   "jeezy, Bankroll Fresh, Bankroll Fresh type beat", "mergeRoster adds new both forms, skips existing");

console.log("all passed");
